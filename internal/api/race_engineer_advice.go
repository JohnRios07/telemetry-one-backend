package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sort"
	"time"

	"telemetry-one-backend/internal/ai"
	"telemetry-one-backend/internal/events"
	"telemetry-one-backend/internal/laps"
	"telemetry-one-backend/internal/platform/httperror"
	"telemetry-one-backend/internal/sessions"
	"telemetry-one-backend/internal/telemetry"
	"telemetry-one-backend/internal/tracks"
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
	Signals           []ai.Signal              `json:"signals,omitempty"`
	Window            raceEngineerAdviceWindow `json:"window"`
	ProviderInfo      ai.ProviderResultInfo    `json:"providerInfo"`
	GeneratedAtUnixMs int64                    `json:"generatedAtUnixMs"`
}

type raceEngineerAdviceWindow struct {
	SinceUnixMs        *int64 `json:"sinceUnixMs"`
	MaxEvents          int    `json:"maxEvents"`
	SelectedEventCount int    `json:"selectedEventCount"`
	DerivedSignalCount int    `json:"derivedSignalCount"`
	FromUnixMs         int64  `json:"fromUnixMs"`
	ToUnixMs           int64  `json:"toUnixMs"`
	ProviderCalled     bool   `json:"providerCalled"`
}

func raceEngineerAdviceHandler(aiSvc ai.AIService, eventStore events.Repository, lapRepo laps.Repository, sessionRepo sessions.Repository, frameStore telemetry.Store, catalog tracks.Catalog) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("sessionId")
		session, err := validateSession(r.Context(), sessionRepo, sessionID, false)
		if err != nil {
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

		completedLaps := listRaceEngineerAdviceCompletedLaps(r.Context(), lapRepo, sessionID)
		derivedSignals := buildRaceEngineerAdviceSignals(storedEvents, completedLaps, request.SinceUnixMs)
		selectedEvents := selectRaceEngineerAdviceEvents(storedEvents, request.SinceUnixMs, maxEvents)
		window := buildRaceEngineerAdviceWindow(request.SinceUnixMs, maxEvents, selectedEvents, len(derivedSignals), false)
		generatedAt := time.Now().UTC().UnixMilli()

		if len(selectedEvents) == 0 && len(derivedSignals) == 0 {
			writeVersionedJSON(w, http.StatusOK, raceEngineerAdviceResponse{
				SessionID:         sessionID,
				Status:            raceEngineerAdviceStatusNoEvents,
				Message:           raceEngineerNoEventsMessage,
				ReferencedEvents:  []string{},
				Signals:           []ai.Signal{},
				Window:            window,
				ProviderInfo:      ai.ProviderResultInfo{},
				GeneratedAtUnixMs: generatedAt,
			})
			return
		}

		gatewayReq := buildRaceEngineerAdviceGatewayRequest(r.Context(), session, selectedEvents, frameStore, catalog, derivedSignals...)
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

		writeVersionedJSON(w, http.StatusOK, raceEngineerAdviceResponse{
			SessionID:         sessionID,
			Status:            status,
			Message:           gatewayResp.Summary,
			ReferencedEvents:  referencedEventIDs(selectedEvents),
			Signals:           derivedSignals,
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

func buildRaceEngineerAdviceWindow(sinceUnixMs *int64, maxEvents int, selectedEvents []events.EngineerEvent, derivedSignalCount int, providerCalled bool) raceEngineerAdviceWindow {
	window := raceEngineerAdviceWindow{
		SinceUnixMs:        sinceUnixMs,
		MaxEvents:          maxEvents,
		SelectedEventCount: len(selectedEvents),
		DerivedSignalCount: derivedSignalCount,
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

func buildRaceEngineerAdviceGatewayRequest(ctx context.Context, session sessions.Session, selectedEvents []events.EngineerEvent, frameStore telemetry.Store, catalog tracks.Catalog, derivedSignals ...ai.Signal) ai.GatewayRequest {
	inputEvents := make([]ai.EventEnvelope, len(selectedEvents))
	for i, event := range selectedEvents {
		inputEvents[i] = ai.EventEnvelope{Event: event}
	}

	track, layout := selectAdviceEventCatalogRefs(selectedEvents)

	if track == nil || layout == nil {
		track, layout = resolvePersistedSessionCatalogRefs(session, catalog)
	}

	if track == nil || layout == nil {
		detectedTrack, detectedLayout := detectTrackLayoutFromFrames(ctx, session.ID, frameStore, catalog)
		if detectedTrack != nil && detectedLayout != nil {
			track, layout = detectedTrack, detectedLayout
		}
	}

	if track == nil {
		track = resolveManualSessionTrackRef(session, catalog)
	}

	return ai.GatewayRequest{
		Mode: ai.GatewayModeEngineer,
		Input: ai.ConsumerInput{
			ContractVersion: ai.ContractVersionV1,
			Session: ai.SessionContext{
				SessionID: session.ID,
				Track:     track,
				Layout:    layout,
			},
			Events:  inputEvents,
			Signals: derivedSignals,
			Safety: ai.SafetyMetadata{
				RedactionPolicy: "structured engineer events, derived metrics, and derived signals only; raw telemetry frames, provider keys, prompts, and model configuration are not accepted from clients",
				AllowedInputKinds: []string{
					ai.AllowedInputEngineerEvents,
					ai.AllowedInputDerivedMetrics,
					ai.AllowedInputDerivedSignals,
					ai.AllowedInputCatalogRefs,
					ai.AllowedInputSessionContext,
					ai.UnknownStateExplicit,
				},
			},
			Constraints: []string{
				"Base advice only on the selected stored engineer events plus backend-derived signals and their supporting evidence.",
				"Do not infer raw telemetry values or use client-supplied prompts, provider names, model names, or API keys.",
				"Keep advice concise and actionable for a live race engineer text response.",
			},
		},
	}
}

func selectAdviceEventCatalogRefs(selectedEvents []events.EngineerEvent) (*events.CatalogRef, *events.CatalogRef) {
	for _, event := range selectedEvents {
		if event.Track != nil && event.Layout != nil {
			return event.Track, event.Layout
		}
	}

	return nil, nil
}

func resolvePersistedSessionCatalogRefs(session sessions.Session, catalog tracks.Catalog) (*events.CatalogRef, *events.CatalogRef) {
	if session.DetectedTrackID == "" || session.DetectedLayoutID == "" {
		return nil, nil
	}

	for _, track := range catalog.Tracks {
		if track.ID != session.DetectedTrackID {
			continue
		}
		for _, layout := range track.Layouts {
			if layout.ID != session.DetectedLayoutID {
				continue
			}

			trackID := track.ID
			trackName := track.Name
			layoutID := layout.ID
			layoutName := layout.Name
			return &events.CatalogRef{ID: &trackID, Name: &trackName, DisplayStrategy: events.DisplayStrategyCatalogName}, &events.CatalogRef{ID: &layoutID, Name: &layoutName, DisplayStrategy: events.DisplayStrategyCatalogName}
		}
	}

	return nil, nil
}

func resolveManualSessionTrackRef(session sessions.Session, catalog tracks.Catalog) *events.CatalogRef {
	if session.TrackID == "" {
		return nil
	}

	for _, track := range catalog.Tracks {
		if track.ID != session.TrackID {
			continue
		}

		trackID := track.ID
		trackName := track.Name
		return &events.CatalogRef{ID: &trackID, Name: &trackName, DisplayStrategy: events.DisplayStrategyCatalogName}
	}

	return nil
}

func referencedEventIDs(selectedEvents []events.EngineerEvent) []string {
	ids := make([]string, len(selectedEvents))
	for i, event := range selectedEvents {
		ids[i] = event.EventID
	}
	return ids
}

func hasLapOneBegan(frames []telemetry.Frame) bool {
	for _, f := range frames {
		if f.LapNumber >= 1 {
			return true
		}
	}
	return false
}

func detectTrackLayoutFromFrames(ctx context.Context, sessionID string, frameStore telemetry.Store, catalog tracks.Catalog) (*events.CatalogRef, *events.CatalogRef) {
	frames, err := frameStore.Frames(ctx, sessionID)
	if err != nil || len(frames) == 0 {
		return nil, nil
	}

	if !hasLapOneBegan(frames) {
		return nil, nil
	}

	result := tracks.DetectTrack(frames, catalog, tracks.DetectionOptions{})
	if result.Status != tracks.DetectionStatusDetected {
		return nil, nil
	}

	var trackRef, layoutRef *events.CatalogRef
	if result.TrackID != nil && result.TrackName != nil {
		trackRef = &events.CatalogRef{
			ID:              result.TrackID,
			Name:            result.TrackName,
			DisplayStrategy: events.DisplayStrategyCatalogName,
		}
	}
	if result.LayoutID != nil && result.LayoutName != nil {
		layoutRef = &events.CatalogRef{
			ID:              result.LayoutID,
			Name:            result.LayoutName,
			DisplayStrategy: events.DisplayStrategyCatalogName,
		}
	}

	return trackRef, layoutRef
}
