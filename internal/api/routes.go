package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"telemetry-one-backend/internal/ai"
	"telemetry-one-backend/internal/config"
	"telemetry-one-backend/internal/events"
	"telemetry-one-backend/internal/platform/httperror"
	"telemetry-one-backend/internal/sessions"
	"telemetry-one-backend/internal/telemetry"
	"telemetry-one-backend/internal/tracks"
)

type healthResponse struct {
	Status string `json:"status"`
	Env    string `json:"env"`
	Time   string `json:"time"`
}

func routes(cfg config.Config, logger *slog.Logger) http.Handler {
	frameStore := telemetry.NewFrameStore(cfg.RetainedFramesPerSession)
	catalog := tracks.OfficialGT7SeedCatalog()
	eventStore := events.NewStore(events.DefaultStoredEventsLimit, events.DedupOptions{})
	pipeCfg := ai.PipelineConfigFromConfig(cfg)
	aiSvc := ai.ComposePipeline(pipeCfg, logger)
	return routesWithAI(cfg, logger, frameStore, catalog, eventStore, aiSvc)
}

func routesWithFrameStore(cfg config.Config, logger *slog.Logger, frameStore *telemetry.FrameStore) http.Handler {
	return routesWithDependencies(cfg, logger, frameStore, tracks.OfficialGT7SeedCatalog())
}

func routesWithDependencies(cfg config.Config, logger *slog.Logger, frameStore *telemetry.FrameStore, catalog tracks.Catalog) http.Handler {
	return routesWithEventStore(cfg, logger, frameStore, catalog, events.NewStore(events.DefaultStoredEventsLimit, events.DedupOptions{}))
}

func routesWithEventStore(cfg config.Config, logger *slog.Logger, frameStore *telemetry.FrameStore, catalog tracks.Catalog, eventStore events.Repository) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler(cfg))
	mux.HandleFunc("GET /api/v1/health", healthHandler(cfg))
	mux.HandleFunc("POST /api/v1/sessions", createSessionHandler)
	mux.HandleFunc("POST /api/v1/sessions/{sessionId}/frames", ingestFramesHandler(frameStore))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}/track", detectTrackHandler(frameStore, catalog))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}/events", listEventsHandler(eventStore))

	return loggingMiddleware(logger, mux)
}

func healthHandler(cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{
			Status: "ok",
			Env:    cfg.Env,
			Time:   time.Now().UTC().Format(time.RFC3339),
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, httperror.Envelope(httperror.Internal("failed to encode response")).Error.Message, http.StatusInternalServerError)
	}
}

func createSessionHandler(w http.ResponseWriter, r *http.Request) {
	var request sessions.CreateRequest
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest("invalid JSON body")))
		return
	}

	if err := request.Validate(); err != nil {
		writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest(err.Error())))
		return
	}

	writeJSON(w, http.StatusNotImplemented, httperror.Envelope(httperror.NotImplemented("session persistence is planned for phase 1.4")))
}

func ingestFramesHandler(frameStore *telemetry.FrameStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request telemetry.IngestBatchRequest
		if err := decodeJSON(r, &request); err != nil {
			writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest("invalid JSON body")))
			return
		}

		if request.SessionID == "" {
			request.SessionID = r.PathValue("sessionId")
		}
		if request.SessionID != r.PathValue("sessionId") {
			writeTelemetryRejection(w, telemetry.SessionIDMismatchError())
			return
		}

		frames, err := request.Normalize()
		if err != nil {
			writeTelemetryRejection(w, err)
			return
		}

		frameStore.Append(request.SessionID, frames)

		fromUnixMs, toUnixMs := acceptedTimeRange(frames)
		writeJSON(w, http.StatusAccepted, telemetry.IngestBatchResponse{
			SessionID:          request.SessionID,
			ReceivedFrames:     len(request.Frames),
			AcceptedFrames:     len(frames),
			RejectedFrames:     0,
			AcceptedFromUnixMs: fromUnixMs,
			AcceptedToUnixMs:   toUnixMs,
			Status:             "accepted",
		})
	}
}

func writeTelemetryRejection(w http.ResponseWriter, err error) {
	var rejection *telemetry.RejectionError
	if errors.As(err, &rejection) {
		writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequestWithDetails(rejection.Error(), rejection.Details())))
		return
	}

	writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest(err.Error())))
}

func acceptedTimeRange(frames []telemetry.Frame) (int64, int64) {
	fromUnixMs := frames[0].TimestampUnixMs
	toUnixMs := frames[0].TimestampUnixMs
	for _, frame := range frames[1:] {
		if frame.TimestampUnixMs < fromUnixMs {
			fromUnixMs = frame.TimestampUnixMs
		}
		if frame.TimestampUnixMs > toUnixMs {
			toUnixMs = frame.TimestampUnixMs
		}
	}

	return fromUnixMs, toUnixMs
}

func detectTrackHandler(frameStore *telemetry.FrameStore, catalog tracks.Catalog) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("sessionId")
		if sessionID == "" {
			writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest(telemetry.ErrMissingSessionID.Error())))
			return
		}

		result := tracks.DetectTrack(frameStore.Frames(sessionID), catalog, tracks.DetectionOptions{})
		writeJSON(w, http.StatusOK, result)
	}
}

func listEventsHandler(store events.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query, err := eventQuery(r)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest(err.Error())))
			return
		}

		storedEvents, err := store.List(r.Context(), query)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest(err.Error())))
			return
		}

		writeJSON(w, http.StatusOK, events.ListResponse{SessionID: query.SessionID, Events: storedEvents})
	}
}

func analyzeHandler(aiSvc ai.AIService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ai.GatewayRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest("invalid JSON body")))
			return
		}

		resp, err := aiSvc.Analyze(r.Context(), req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest(err.Error())))
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func routesWithAI(cfg config.Config, logger *slog.Logger, frameStore *telemetry.FrameStore, catalog tracks.Catalog, eventStore events.Repository, aiSvc ai.AIService) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler(cfg))
	mux.HandleFunc("GET /api/v1/health", healthHandler(cfg))
	mux.HandleFunc("POST /api/v1/sessions", createSessionHandler)
	mux.HandleFunc("POST /api/v1/sessions/{sessionId}/frames", ingestFramesHandler(frameStore))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}/track", detectTrackHandler(frameStore, catalog))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}/events", listEventsHandler(eventStore))
	mux.HandleFunc("POST /api/v1/sessions/{sessionId}/analyze", analyzeHandler(aiSvc))
	return loggingMiddleware(logger, mux)
}

func eventQuery(r *http.Request) (events.Query, error) {
	query := events.Query{SessionID: r.PathValue("sessionId")}
	values := r.URL.Query()

	if lapNumber := values.Get("lapNumber"); lapNumber != "" {
		parsed, err := strconv.Atoi(lapNumber)
		if err != nil {
			return events.Query{}, events.ErrInvalidLapNumber
		}
		query.LapNumber = &parsed
	}
	query.CornerID = values.Get("cornerId")
	if eventType := values.Get("type"); eventType != "" {
		query.Type = events.EventType(eventType)
	}

	if err := query.Validate(); err != nil {
		return events.Query{}, err
	}

	return query, nil
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("body must contain a single JSON value")
	}

	return nil
}

func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("http request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds())
	})
}
