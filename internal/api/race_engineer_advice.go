package api

import (
	"errors"
	"io"
	"net/http"
	"sort"
	"time"

	"telemetry-one-backend/internal/ai"
	"telemetry-one-backend/internal/events"
	"telemetry-one-backend/internal/platform/httperror"
	"telemetry-one-backend/internal/sessions"
)

const (
	raceEngineerAdviceDefaultMaxEvents = 5
	raceEngineerAdviceMinEvents        = 1
	raceEngineerAdviceMaxEvents        = 10
	raceEngineerAdviceStatusNoEvents   = "no_events"
	raceEngineerNoEventsMessage        = "No race engineer events are available for the selected window yet. Keep driving and request advice again after new events are detected."
)

type raceEngineerAdviceRequest struct {
	SinceUnixMs *int64 `json:"sinceUnixMs,omitempty"`
	MaxEvents   *int   `json:"maxEvents,omitempty"`
}

type raceEngineerAdviceResponse struct {
	SessionID         string                   `json:"sessionId"`
	Status            string                   `json:"status"`
	Message           string                   `json:"message"`
	ReferencedEvents  []string                 `json:"referencedEvents"`
	Window            raceEngineerAdviceWindow `json:"window"`
	ProviderInfo      ai.ProviderResultInfo    `json:"providerInfo"`
	GeneratedAtUnixMs int64                    `json:"generatedAtUnixMs"`
}

type raceEngineerAdviceWindow struct {
	SinceUnixMs        *int64 `json:"sinceUnixMs"`
	MaxEvents          int    `json:"maxEvents"`
	SelectedEventCount int    `json:"selectedEventCount"`
	FromUnixMs         int64  `json:"fromUnixMs"`
	ToUnixMs           int64  `json:"toUnixMs"`
	ProviderCalled     bool   `json:"providerCalled"`
}

func raceEngineerAdviceHandler(aiSvc ai.AIService, eventStore events.Repository, sessionRepo sessions.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("sessionId")
		if _, err := validateSession(r.Context(), sessionRepo, sessionID, false); err != nil {
			writeSessionError(w, err)
			return
		}

		request, err := decodeRaceEngineerAdviceRequest(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest(err.Error())))
			return
		}

		maxEvents := normalizeAdviceMaxEvents(request.MaxEvents)
		if request.SinceUnixMs != nil && *request.SinceUnixMs <= 0 {
			writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest("sinceUnixMs must be greater than zero")))
			return
		}

		storedEvents, err := eventStore.List(r.Context(), events.Query{SessionID: sessionID})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, httperror.Envelope(httperror.Internal("failed to list race engineer events")))
			return
		}

		selectedEvents := selectRaceEngineerAdviceEvents(storedEvents, request.SinceUnixMs, maxEvents)
		window := buildRaceEngineerAdviceWindow(request.SinceUnixMs, maxEvents, selectedEvents, false)
		generatedAt := time.Now().UTC().UnixMilli()

		if len(selectedEvents) == 0 {
			writeJSON(w, http.StatusOK, raceEngineerAdviceResponse{
				SessionID:         sessionID,
				Status:            raceEngineerAdviceStatusNoEvents,
				Message:           raceEngineerNoEventsMessage,
				ReferencedEvents:  []string{},
				Window:            window,
				ProviderInfo:      ai.ProviderResultInfo{},
				GeneratedAtUnixMs: generatedAt,
			})
			return
		}

		gatewayReq := buildRaceEngineerAdviceGatewayRequest(sessionID, selectedEvents)
		gatewayResp, err := aiSvc.Analyze(r.Context(), gatewayReq)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest(err.Error())))
			return
		}

		window.ProviderCalled = true
		status := gatewayResp.Status
		if status == "" {
			status = ai.StatusSuccess
		}

		writeJSON(w, http.StatusOK, raceEngineerAdviceResponse{
			SessionID:         sessionID,
			Status:            status,
			Message:           gatewayResp.Summary,
			ReferencedEvents:  referencedEventIDs(selectedEvents),
			Window:            window,
			ProviderInfo:      gatewayResp.ProviderInfo,
			GeneratedAtUnixMs: generatedAt,
		})
	}
}

func decodeRaceEngineerAdviceRequest(r *http.Request) (raceEngineerAdviceRequest, error) {
	if r.Body == nil || r.Body == http.NoBody || r.ContentLength == 0 {
		return raceEngineerAdviceRequest{}, nil
	}

	var request raceEngineerAdviceRequest
	if err := decodeJSON(r, &request); err != nil {
		if errors.Is(err, io.EOF) {
			return raceEngineerAdviceRequest{}, nil
		}
		return raceEngineerAdviceRequest{}, errors.New("invalid JSON body")
	}
	return request, nil
}

func normalizeAdviceMaxEvents(maxEvents *int) int {
	if maxEvents == nil {
		return raceEngineerAdviceDefaultMaxEvents
	}
	if *maxEvents < raceEngineerAdviceMinEvents {
		return raceEngineerAdviceMinEvents
	}
	if *maxEvents > raceEngineerAdviceMaxEvents {
		return raceEngineerAdviceMaxEvents
	}
	return *maxEvents
}

func selectRaceEngineerAdviceEvents(storedEvents []events.EngineerEvent, sinceUnixMs *int64, maxEvents int) []events.EngineerEvent {
	matching := make([]events.EngineerEvent, 0, len(storedEvents))
	for _, event := range storedEvents {
		if sinceUnixMs != nil && event.TimestampUnixMs < *sinceUnixMs {
			continue
		}
		matching = append(matching, event)
	}

	sort.SliceStable(matching, func(i, j int) bool {
		return matching[i].TimestampUnixMs < matching[j].TimestampUnixMs
	})
	if len(matching) > maxEvents {
		matching = matching[len(matching)-maxEvents:]
	}

	return matching
}

func buildRaceEngineerAdviceWindow(sinceUnixMs *int64, maxEvents int, selectedEvents []events.EngineerEvent, providerCalled bool) raceEngineerAdviceWindow {
	window := raceEngineerAdviceWindow{
		SinceUnixMs:        sinceUnixMs,
		MaxEvents:          maxEvents,
		SelectedEventCount: len(selectedEvents),
		ProviderCalled:     providerCalled,
	}
	if len(selectedEvents) == 0 {
		return window
	}
	window.FromUnixMs = selectedEvents[0].TimestampUnixMs
	window.ToUnixMs = selectedEvents[0].TimestampUnixMs
	for _, event := range selectedEvents[1:] {
		if event.TimestampUnixMs < window.FromUnixMs {
			window.FromUnixMs = event.TimestampUnixMs
		}
		if event.TimestampUnixMs > window.ToUnixMs {
			window.ToUnixMs = event.TimestampUnixMs
		}
	}
	return window
}

func buildRaceEngineerAdviceGatewayRequest(sessionID string, selectedEvents []events.EngineerEvent) ai.GatewayRequest {
	inputEvents := make([]ai.EventEnvelope, len(selectedEvents))
	for i, event := range selectedEvents {
		inputEvents[i] = ai.EventEnvelope{Event: event}
	}

	var track *events.CatalogRef
	var layout *events.CatalogRef
	for _, event := range selectedEvents {
		if track == nil {
			track = event.Track
		}
		if layout == nil {
			layout = event.Layout
		}
		if track != nil && layout != nil {
			break
		}
	}

	return ai.GatewayRequest{
		Mode: ai.GatewayModeEngineer,
		Input: ai.ConsumerInput{
			ContractVersion: ai.ContractVersionV1,
			Session: ai.SessionContext{
				SessionID: sessionID,
				Track:     track,
				Layout:    layout,
			},
			Events: inputEvents,
			Safety: ai.SafetyMetadata{
				RedactionPolicy: "structured engineer events and derived metrics only; raw telemetry frames, provider keys, prompts, and model configuration are not accepted from clients",
				AllowedInputKinds: []string{
					ai.AllowedInputEngineerEvents,
					ai.AllowedInputDerivedMetrics,
					ai.AllowedInputCatalogRefs,
					ai.AllowedInputSessionContext,
					ai.UnknownStateExplicit,
				},
			},
			Constraints: []string{
				"Base advice only on the selected stored engineer events and their derived metric evidence.",
				"Do not infer raw telemetry values or use client-supplied prompts, provider names, model names, or API keys.",
				"Keep advice concise and actionable for a live race engineer text response.",
			},
		},
	}
}

func referencedEventIDs(selectedEvents []events.EngineerEvent) []string {
	ids := make([]string, len(selectedEvents))
	for i, event := range selectedEvents {
		ids[i] = event.EventID
	}
	return ids
}
