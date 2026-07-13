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
	sessionRepo := sessions.NewMemoryRepository()
	pipeCfg := ai.PipelineConfigFromConfig(cfg)
	aiSvc := ai.ComposePipeline(pipeCfg, logger)
	return routesWithAIAndSessions(cfg, logger, frameStore, catalog, eventStore, sessionRepo, aiSvc)
}

func routesWithFrameStore(cfg config.Config, logger *slog.Logger, frameStore *telemetry.FrameStore) http.Handler {
	return routesWithDependencies(cfg, logger, frameStore, tracks.OfficialGT7SeedCatalog())
}

func routesWithDependencies(cfg config.Config, logger *slog.Logger, frameStore *telemetry.FrameStore, catalog tracks.Catalog) http.Handler {
	return routesWithEventStore(cfg, logger, frameStore, catalog, events.NewStore(events.DefaultStoredEventsLimit, events.DedupOptions{}))
}

func routesWithEventStore(cfg config.Config, logger *slog.Logger, frameStore *telemetry.FrameStore, catalog tracks.Catalog, eventStore events.Repository) http.Handler {
	return routesWithSessionRepository(cfg, logger, frameStore, catalog, eventStore, sessions.NewMemoryRepository())
}

func routesWithSessionRepository(cfg config.Config, logger *slog.Logger, frameStore *telemetry.FrameStore, catalog tracks.Catalog, eventStore events.Repository, sessionRepo sessions.Repository) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler(cfg))
	mux.HandleFunc("GET /api/v1/health", healthHandler(cfg))
	mux.HandleFunc("POST /api/v1/sessions", createSessionHandler(sessionRepo, catalog))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}", getSessionHandler(sessionRepo, frameStore, eventStore))
	mux.HandleFunc("POST /api/v1/sessions/{sessionId}/finish", finishSessionHandler(sessionRepo, frameStore, eventStore))
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

func createSessionHandler(repo sessions.Repository, catalog tracks.Catalog) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request sessions.CreateRequest
		if err := decodeJSON(r, &request); err != nil {
			writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest("invalid JSON body")))
			return
		}

		if err := request.Validate(); err != nil {
			writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest(err.Error())))
			return
		}
		if request.TrackID != "" && !catalogHasTrack(catalog, request.TrackID) {
			writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest("trackId must exist in catalog")))
			return
		}

		id, err := sessions.NewID()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, httperror.Envelope(httperror.Internal("failed to generate session id")))
			return
		}

		session, err := repo.Create(r.Context(), sessions.Session{
			ID:          id,
			Source:      request.Source,
			Game:        request.Game,
			Platform:    request.Platform,
			DriverAlias: request.DriverAlias,
			StartedAt:   time.UnixMilli(request.StartedUnixMs).UTC(),
			TrackID:     request.TrackID,
		})
		if err != nil {
			writeSessionError(w, err)
			return
		}

		writeJSON(w, http.StatusCreated, sessions.Response{Session: sessions.NewDTO(session, 0, 0)})
	}
}

func getSessionHandler(repo sessions.Repository, frameStore *telemetry.FrameStore, eventStore events.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, err := repo.FindByID(r.Context(), r.PathValue("sessionId"))
		if err != nil {
			writeSessionError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, sessions.Response{Session: sessionDTO(r, session, frameStore, eventStore)})
	}
}

var nowUTC = func() time.Time { return time.Now().UTC() }

func finishSessionHandler(repo sessions.Repository, frameStore *telemetry.FrameStore, eventStore events.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request sessions.FinishRequest
		if r.Body != nil && r.ContentLength != 0 {
			if err := decodeJSON(r, &request); err != nil {
				if errors.Is(err, io.EOF) {
					request = sessions.FinishRequest{}
				} else {
					writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest("invalid JSON body")))
					return
				}
			}
		}

		session, err := repo.End(r.Context(), r.PathValue("sessionId"), request.EndedAt(nowUTC))
		if err != nil {
			writeSessionError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, sessions.Response{Session: sessionDTO(r, session, frameStore, eventStore)})
	}
}

func sessionDTO(r *http.Request, session sessions.Session, frameStore *telemetry.FrameStore, eventStore events.Repository) sessions.DTO {
	frameCount := len(frameStore.Frames(session.ID))
	eventCount := 0
	if eventStore != nil {
		if storedEvents, err := eventStore.List(r.Context(), events.Query{SessionID: session.ID}); err == nil {
			eventCount = len(storedEvents)
		}
	}

	return sessions.NewDTO(session, frameCount, eventCount)
}

func writeSessionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sessions.ErrNotFound):
		writeJSON(w, http.StatusNotFound, httperror.Envelope(httperror.NotFound(err.Error())))
	case errors.Is(err, sessions.ErrAlreadyFinished):
		writeJSON(w, http.StatusConflict, httperror.Envelope(httperror.Conflict(err.Error())))
	case errors.Is(err, sessions.ErrInvalidEndedAt), errors.Is(err, sessions.ErrMissingID):
		writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest(err.Error())))
	default:
		writeJSON(w, http.StatusInternalServerError, httperror.Envelope(httperror.Internal("session repository error")))
	}
}

func catalogHasTrack(catalog tracks.Catalog, trackID string) bool {
	for _, track := range catalog.Tracks {
		if track.ID == trackID {
			return true
		}
	}

	return false
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
	return routesWithAIAndSessions(cfg, logger, frameStore, catalog, eventStore, sessions.NewMemoryRepository(), aiSvc)
}

func routesWithAIAndSessions(cfg config.Config, logger *slog.Logger, frameStore *telemetry.FrameStore, catalog tracks.Catalog, eventStore events.Repository, sessionRepo sessions.Repository, aiSvc ai.AIService) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler(cfg))
	mux.HandleFunc("GET /api/v1/health", healthHandler(cfg))
	mux.HandleFunc("POST /api/v1/sessions", createSessionHandler(sessionRepo, catalog))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}", getSessionHandler(sessionRepo, frameStore, eventStore))
	mux.HandleFunc("POST /api/v1/sessions/{sessionId}/finish", finishSessionHandler(sessionRepo, frameStore, eventStore))
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
