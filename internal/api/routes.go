package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"telemetry-one-backend/internal/admin"
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
	statsRepo := admin.NewMemoryStatsRepo(sessionRepo, frameStore)
	summaryRepo := sessions.NewMemorySummaryRepository(sessionRepo, frameStore, eventStore)
	return routesWithAIAndSessions(cfg, logger, frameStore, catalog, eventStore, sessionRepo, aiSvc, statsRepo, summaryRepo)
}

func routesWithFrameStore(cfg config.Config, logger *slog.Logger, frameStore telemetry.Store) http.Handler {
	return routesWithDependencies(cfg, logger, frameStore, tracks.OfficialGT7SeedCatalog())
}

func routesWithDependencies(cfg config.Config, logger *slog.Logger, frameStore telemetry.Store, catalog tracks.Catalog) http.Handler {
	return routesWithEventStore(cfg, logger, frameStore, catalog, events.NewStore(events.DefaultStoredEventsLimit, events.DedupOptions{}))
}

func routesWithEventStore(cfg config.Config, logger *slog.Logger, frameStore telemetry.Store, catalog tracks.Catalog, eventStore events.Repository) http.Handler {
	return routesWithSessionRepository(cfg, logger, frameStore, catalog, eventStore, sessions.NewMemoryRepository())
}

func routesWithSessionRepository(cfg config.Config, logger *slog.Logger, frameStore telemetry.Store, catalog tracks.Catalog, eventStore events.Repository, sessionRepo sessions.Repository) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler(cfg))
	mux.HandleFunc("GET /api/v1/health", healthHandler(cfg))
	mux.HandleFunc("POST /api/v1/sessions", createSessionHandler(sessionRepo, catalog))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}", getSessionHandler(sessionRepo, frameStore, eventStore))
	mux.HandleFunc("POST /api/v1/sessions/{sessionId}/finish", finishSessionHandler(sessionRepo, frameStore, eventStore))
	mux.HandleFunc("POST /api/v1/sessions/{sessionId}/frames", ingestFramesHandler(frameStore, eventStore, sessionRepo, logger))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}/track", detectTrackHandler(frameStore, catalog, sessionRepo))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}/events", listEventsHandler(eventStore, sessionRepo))

	summaryRepo := sessions.NewMemorySummaryRepository(sessionRepo, frameStore, eventStore)
	mux.HandleFunc("GET /api/v1/sessions", listSessionsHandler(summaryRepo))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}/summary", sessionSummaryHandler(summaryRepo))

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

func getSessionHandler(repo sessions.Repository, frameStore telemetry.Store, eventStore events.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, err := repo.FindByID(r.Context(), r.PathValue("sessionId"))
		if err != nil {
			writeSessionError(w, err)
			return
		}

		dto, err := sessionDTO(r, session, frameStore, eventStore)
		if err != nil {
			writeSessionDTOError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, sessions.Response{Session: dto})
	}
}

var nowUTC = func() time.Time { return time.Now().UTC() }

func finishSessionHandler(repo sessions.Repository, frameStore telemetry.Store, eventStore events.Repository) http.HandlerFunc {
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

		dto, err := sessionDTO(r, session, frameStore, eventStore)
		if err != nil {
			writeSessionDTOError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, sessions.Response{Session: dto})
	}
}

func sessionDTO(r *http.Request, session sessions.Session, frameStore telemetry.Store, eventStore events.Repository) (sessions.DTO, error) {
	frames, err := frameStore.Frames(r.Context(), session.ID)
	if err != nil {
		return sessions.DTO{}, fmt.Errorf("%w: %v", errFrameRepository, err)
	}
	frameCount := len(frames)
	eventCount := 0
	if eventStore != nil {
		storedEvents, err := eventStore.List(r.Context(), events.Query{SessionID: session.ID})
		if err != nil {
			return sessions.DTO{}, fmt.Errorf("%w: %v", errEventRepository, err)
		}
		eventCount = len(storedEvents)
	}

	return sessions.NewDTO(session, frameCount, eventCount), nil
}

var (
	errFrameRepository = errors.New("frame repository error")
	errEventRepository = errors.New("event repository error")
)

func writeSessionDTOError(w http.ResponseWriter, err error) {
	message := "event repository error"
	if errors.Is(err, errFrameRepository) {
		message = "frame repository error"
	}
	writeJSON(w, http.StatusInternalServerError, httperror.Envelope(httperror.Internal(message)))
}

func writeSessionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sessions.ErrNotFound):
		writeJSON(w, http.StatusNotFound, httperror.Envelope(httperror.Error{Code: "session_not_found", Message: err.Error()}))
	case errors.Is(err, sessions.ErrAlreadyFinished):
		writeJSON(w, http.StatusConflict, httperror.Envelope(httperror.Error{Code: "session_finished", Message: err.Error()}))
	case errors.Is(err, sessions.ErrAlreadyExists):
		writeJSON(w, http.StatusConflict, httperror.Envelope(httperror.Conflict(err.Error())))
	case errors.Is(err, sessions.ErrInvalidEndedAt), errors.Is(err, sessions.ErrMissingID):
		writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.Error{Code: "invalid_session_id", Message: err.Error()}))
	default:
		writeJSON(w, http.StatusInternalServerError, httperror.Envelope(httperror.Internal("session repository error")))
	}
}

func validateSession(ctx context.Context, repo sessions.Repository, sessionID string, requireActive bool) (sessions.Session, error) {
	if sessionID == "" {
		return sessions.Session{}, sessions.ErrMissingID
	}
	session, err := repo.FindByID(ctx, sessionID)
	if err != nil {
		return sessions.Session{}, err
	}
	if requireActive && session.Status() == sessions.StatusFinished {
		return sessions.Session{}, sessions.ErrAlreadyFinished
	}
	return session, nil
}

func catalogHasTrack(catalog tracks.Catalog, trackID string) bool {
	for _, track := range catalog.Tracks {
		if track.ID == trackID {
			return true
		}
	}

	return false
}

func ingestFramesHandler(frameStore telemetry.Store, eventStore events.Repository, sessionRepo sessions.Repository, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("sessionId")
		if _, err := validateSession(r.Context(), sessionRepo, sessionID, true); err != nil {
			writeSessionError(w, err)
			return
		}

		var request telemetry.IngestBatchRequest
		if err := decodeJSON(r, &request); err != nil {
			writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest("invalid JSON body")))
			return
		}

		if request.SessionID == "" {
			request.SessionID = sessionID
		}
		if request.SessionID != sessionID {
			writeTelemetryRejection(w, telemetry.SessionIDMismatchError())
			return
		}

		result, err := request.Normalize()
		if err != nil {
			writeTelemetryRejection(w, err)
			return
		}

		if len(result.Frames) > 0 {
			if err := frameStore.Append(r.Context(), request.SessionID, result.Frames); err != nil {
				switch {
				case errors.Is(err, telemetry.ErrSessionNotFound):
					writeJSON(w, http.StatusNotFound, httperror.Envelope(httperror.Error{Code: "session_not_found", Message: err.Error()}))
				case errors.Is(err, telemetry.ErrSessionFinished):
					writeJSON(w, http.StatusConflict, httperror.Envelope(httperror.Error{Code: "session_finished", Message: err.Error()}))
				default:
					writeJSON(w, http.StatusInternalServerError, httperror.Envelope(httperror.Internal("frame persistence error")))
				}
				return
			}

			if eventStore != nil {
				generated, err := events.GenerateFrameEvents(request.SessionID, result.Frames)
				if err != nil {
					writeJSON(w, http.StatusInternalServerError, httperror.Envelope(httperror.Internal("event generation error")))
					return
				}
				for _, event := range generated {
					if _, accepted, err := eventStore.Append(r.Context(), event); err != nil {
						writeJSON(w, http.StatusInternalServerError, httperror.Envelope(httperror.Internal("event persistence error")))
						return
					} else if !accepted {
						logger.Info("event deduplicated", "session_id", event.SessionID, "event_id", event.EventID, "type", event.Type, "lap_number", event.LapNumber)
					}
				}
			}
		}

		fromUnixMs, toUnixMs := acceptedTimeRange(result.Frames)
		status := "accepted"
		if len(result.Frames) == 0 {
			status = "rejected"
		} else if len(result.Rejections) > 0 {
			status = "partial"
		}

		var summary *telemetry.RejectionSummary
		if len(result.Rejections) > 0 {
			s := telemetry.BuildRejectionSummary(result.Rejections)
			summary = &s
		}

		resp := telemetry.IngestBatchResponse{
			SessionID:          request.SessionID,
			ReceivedFrames:     len(request.Frames),
			AcceptedFrames:     len(result.Frames),
			RejectedFrames:     len(result.Rejections),
			AcceptedFromUnixMs: fromUnixMs,
			AcceptedToUnixMs:   toUnixMs,
			Status:             status,
			RejectionSummary:   summary,
		}

		writeJSON(w, http.StatusAccepted, resp)

		logger.Info("frames ingested",
			"session_id", resp.SessionID,
			"received", resp.ReceivedFrames,
			"accepted", resp.AcceptedFrames,
			"rejected", resp.RejectedFrames,
			"status", resp.Status,
			"top_reasons", formatTopReasons(summary),
		)
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
	if len(frames) == 0 {
		return 0, 0
	}
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

func formatTopReasons(summary *telemetry.RejectionSummary) []string {
	if summary == nil || len(summary.Reasons) == 0 {
		return nil
	}
	top := summary.Reasons
	if len(top) > 3 {
		top = top[:3]
	}
	result := make([]string, len(top))
	for i, r := range top {
		result[i] = r.Code
	}
	return result
}

func detectTrackHandler(frameStore telemetry.Store, catalog tracks.Catalog, sessionRepo sessions.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("sessionId")
		if _, err := validateSession(r.Context(), sessionRepo, sessionID, false); err != nil {
			writeSessionError(w, err)
			return
		}

		frames, err := frameStore.Frames(r.Context(), sessionID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, httperror.Envelope(httperror.Internal("frame persistence error")))
			return
		}

		result := tracks.DetectTrack(frames, catalog, tracks.DetectionOptions{})
		writeJSON(w, http.StatusOK, result)
	}
}

func listEventsHandler(store events.Repository, sessionRepo sessions.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("sessionId")
		if _, err := validateSession(r.Context(), sessionRepo, sessionID, false); err != nil {
			writeSessionError(w, err)
			return
		}

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

func analyzeHandler(aiSvc ai.AIService, sessionRepo sessions.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("sessionId")
		if _, err := validateSession(r.Context(), sessionRepo, sessionID, false); err != nil {
			writeSessionError(w, err)
			return
		}

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

func routesWithAI(cfg config.Config, logger *slog.Logger, frameStore telemetry.Store, catalog tracks.Catalog, eventStore events.Repository, aiSvc ai.AIService) http.Handler {
	sessionRepo := sessions.NewMemoryRepository()
	statsRepo := admin.NewMemoryStatsRepo(sessionRepo, frameStore)
	summaryRepo := sessions.NewMemorySummaryRepository(sessionRepo, frameStore, eventStore)
	return routesWithAIAndSessions(cfg, logger, frameStore, catalog, eventStore, sessionRepo, aiSvc, statsRepo, summaryRepo)
}

func routesWithAIAndSessions(cfg config.Config, logger *slog.Logger, frameStore telemetry.Store, catalog tracks.Catalog, eventStore events.Repository, sessionRepo sessions.Repository, aiSvc ai.AIService, statsRepo admin.StatsRepository, summaryRepo sessions.SummaryRepository) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler(cfg))
	mux.HandleFunc("GET /api/v1/health", healthHandler(cfg))
	mux.HandleFunc("POST /api/v1/sessions", createSessionHandler(sessionRepo, catalog))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}", getSessionHandler(sessionRepo, frameStore, eventStore))
	mux.HandleFunc("POST /api/v1/sessions/{sessionId}/finish", finishSessionHandler(sessionRepo, frameStore, eventStore))
	mux.HandleFunc("POST /api/v1/sessions/{sessionId}/frames", ingestFramesHandler(frameStore, eventStore, sessionRepo, logger))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}/track", detectTrackHandler(frameStore, catalog, sessionRepo))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}/events", listEventsHandler(eventStore, sessionRepo))
	mux.HandleFunc("POST /api/v1/sessions/{sessionId}/analyze", analyzeHandler(aiSvc, sessionRepo))
	mux.HandleFunc("POST /api/v1/sessions/{sessionId}/race-engineer/advice", raceEngineerAdviceHandler(aiSvc, eventStore, sessionRepo))
	mux.Handle("GET /api/v1/admin/ingest-stats", adminAuthMiddleware(cfg, ingestStatsHandler(statsRepo)))
	mux.HandleFunc("GET /api/v1/sessions", listSessionsHandler(summaryRepo))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}/summary", sessionSummaryHandler(summaryRepo))
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

func adminAuthMiddleware(cfg config.Config, next http.Handler) http.Handler {
	if cfg.AdminToken == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			writeJSON(w, http.StatusUnauthorized, httperror.Envelope(httperror.Error{Code: "unauthorized", Message: "missing or malformed authorization header"}))
			return
		}
		token := strings.TrimPrefix(auth, "Bearer ")
		if token != cfg.AdminToken {
			writeJSON(w, http.StatusUnauthorized, httperror.Envelope(httperror.Error{Code: "unauthorized", Message: "invalid token"}))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clampQueryParam(r *http.Request, name string, defaultVal, min, max int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return defaultVal
	}
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

func ingestStatsHandler(statsRepo admin.StatsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := clampQueryParam(r, "limit", admin.DefaultLimit, admin.MinLimit, admin.MaxLimit)
		days := clampQueryParam(r, "days", admin.DefaultDays, admin.MinDays, admin.MaxDays)

		resp, err := statsRepo.Stats(r.Context(), limit, days)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, httperror.Envelope(httperror.Internal("failed to load ingest stats")))
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func listSessionsHandler(summaryRepo sessions.SummaryRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := clampQueryParam(r, "limit", sessions.DefaultListLimit, sessions.MinListLimit, sessions.MaxListLimit)

		resp, err := summaryRepo.List(r.Context(), sessions.SummaryFilter{Limit: limit})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, httperror.Envelope(httperror.Internal("failed to list sessions")))
			return
		}

		if resp.Sessions == nil {
			resp.Sessions = []sessions.SessionSummaryItem{}
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func sessionSummaryHandler(summaryRepo sessions.SummaryRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("sessionId")

		resp, err := summaryRepo.Summary(r.Context(), sessionID)
		if err != nil {
			writeSessionError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("http request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds())
	})
}
