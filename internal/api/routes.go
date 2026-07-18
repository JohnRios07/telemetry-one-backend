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
	"telemetry-one-backend/internal/laps"
	"telemetry-one-backend/internal/platform/httperror"
	"telemetry-one-backend/internal/sessionexport"
	"telemetry-one-backend/internal/sessions"
	"telemetry-one-backend/internal/telemetry"
	"telemetry-one-backend/internal/tracks"
)

type healthResponse struct {
	Status string `json:"status"`
	Env    string `json:"env"`
	Time   string `json:"time"`
}

const responseAPIVersion = "telemetry-one.api.v2"

func routes(cfg config.Config, logger *slog.Logger) http.Handler {
	frameStore := telemetry.NewFrameStore(cfg.RetainedFramesPerSession)
	rejectionStore := telemetry.NewMemoryRejectionSummaryStore()
	catalog, err := prepareRuntimeCatalog(cfg, tracks.OfficialGT7SeedCatalog())
	if err != nil {
		panic(err)
	}
	eventStore := events.NewStore(events.DefaultStoredEventsLimit, events.DedupOptions{})
	lapRepo := laps.NewMemoryRepository()
	sessionRepo := sessions.NewMemoryRepository()
	pipeCfg := ai.PipelineConfigFromConfig(cfg)
	aiSvc := ai.ComposePipeline(pipeCfg, logger)
	statsRepo := admin.NewMemoryStatsRepo(sessionRepo, frameStore)
	summaryRepo := sessions.NewMemorySummaryRepository(sessionRepo, frameStore, eventStore, rejectionStore)
	return routesWithAIAndSessionsAndLaps(cfg, logger, frameStore, catalog, eventStore, lapRepo, lapRepo, sessionRepo, aiSvc, statsRepo, summaryRepo, rejectionStore)
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
	return routesWithLapRepository(cfg, logger, frameStore, catalog, eventStore, laps.NewMemoryRepository(), sessionRepo)
}

func routesWithLapRepository(cfg config.Config, logger *slog.Logger, frameStore telemetry.Store, catalog tracks.Catalog, eventStore events.Repository, lapRepo laps.Repository, sessionRepo sessions.Repository) http.Handler {
	var sampleRepo laps.SampleRepository
	if repo, ok := lapRepo.(laps.SampleRepository); ok {
		sampleRepo = repo
	}
	return routesWithLapAndSampleRepositories(cfg, logger, frameStore, catalog, eventStore, lapRepo, sampleRepo, sessionRepo)
}

func routesWithLapAndSampleRepositories(cfg config.Config, logger *slog.Logger, frameStore telemetry.Store, catalog tracks.Catalog, eventStore events.Repository, lapRepo laps.Repository, sampleRepo laps.SampleRepository, sessionRepo sessions.Repository) http.Handler {
	rejectionStore := telemetry.NewMemoryRejectionSummaryStore()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler(cfg))
	mux.HandleFunc("GET /api/v1/health", healthHandler(cfg))
	mux.HandleFunc("GET /api/v1/settings/bootstrap", settingsBootstrapHandler(cfg))
	mux.HandleFunc("GET /api/v1/catalog/track-layouts", catalogTrackLayoutsHandler(catalog))
	mux.HandleFunc("POST /api/v1/sessions", createSessionHandler(sessionRepo, catalog))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}", getSessionHandler(sessionRepo, frameStore, eventStore, catalog))
	mux.HandleFunc("PUT /api/v1/sessions/{sessionId}/track-layout", sessionTrackLayoutHandler(sessionRepo, catalog, frameStore, eventStore))
	mux.HandleFunc("POST /api/v1/sessions/{sessionId}/finish", finishSessionHandler(sessionRepo, frameStore, eventStore, catalog))
	mux.HandleFunc("POST /api/v1/sessions/{sessionId}/frames", ingestFramesHandler(frameStore, eventStore, lapRepo, sampleRepo, sessionRepo, catalog, logger, rejectionStore))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}/track", detectTrackHandler(frameStore, catalog, sessionRepo))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}/events", listEventsHandler(eventStore, sessionRepo))

	summaryRepo := sessions.NewMemorySummaryRepository(sessionRepo, frameStore, eventStore, rejectionStore)
	mux.HandleFunc("GET /api/v1/sessions", listSessionsHandler(summaryRepo))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}/summary", sessionSummaryHandler(summaryRepo))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}/export/trackbuilder", sessionExportHandler(cfg, sessionexport.Exporter{SessionReader: sessionRepo, FrameReader: frameStore}))

	return loggingMiddleware(logger, mux)
}

func healthHandler(cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payload := healthResponse{
			Status: "ok",
			Env:    cfg.Env,
			Time:   time.Now().UTC().Format(time.RFC3339),
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			writeVersionedJSON(w, http.StatusOK, payload)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, httperror.Envelope(httperror.Internal("failed to encode response")).Error.Message, http.StatusInternalServerError)
	}
}

func writeVersionedJSON(w http.ResponseWriter, status int, payload any) {
	writeJSON(w, status, payloadWithAPIVersion(status, payload))
}

func payloadWithAPIVersion(status int, payload any) any {
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return payload
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return payload
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil || object == nil {
		return payload
	}
	if _, exists := object["apiVersion"]; exists {
		return payload
	}

	version, err := json.Marshal(responseAPIVersion)
	if err != nil {
		return payload
	}
	object["apiVersion"] = version
	return object
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

		dto := sessions.NewDTO(session, 0, 0)
		writeVersionedJSON(w, http.StatusCreated, sessions.Response{Session: enrichSessionDTO(dto, session, catalog)})
	}
}

func getSessionHandler(repo sessions.Repository, frameStore telemetry.Store, eventStore events.Repository, catalog tracks.Catalog) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, err := repo.FindByID(r.Context(), r.PathValue("sessionId"))
		if err != nil {
			writeSessionError(w, err)
			return
		}

		dto, err := sessionDTO(r, session, frameStore, eventStore, catalog)
		if err != nil {
			writeSessionDTOError(w, err)
			return
		}

		writeVersionedJSON(w, http.StatusOK, sessions.Response{Session: dto})
	}
}

var nowUTC = func() time.Time { return time.Now().UTC() }

func finishSessionHandler(repo sessions.Repository, frameStore telemetry.Store, eventStore events.Repository, catalog tracks.Catalog) http.HandlerFunc {
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

		dto, err := sessionDTO(r, session, frameStore, eventStore, catalog)
		if err != nil {
			writeSessionDTOError(w, err)
			return
		}

		writeVersionedJSON(w, http.StatusOK, sessions.Response{Session: dto})
	}
}

func sessionDTO(r *http.Request, session sessions.Session, frameStore telemetry.Store, eventStore events.Repository, catalog tracks.Catalog) (sessions.DTO, error) {
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

	dto := sessions.NewDTO(session, frameCount, eventCount)
	return enrichSessionDTO(dto, session, catalog), nil
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

func prepareRuntimeCatalog(cfg config.Config, catalog tracks.Catalog) (tracks.Catalog, error) {
	return catalog.PrepareApprovedGeometry(cfg.SkipInvalidApprovedGeometry)
}

func enrichSessionDTO(dto sessions.DTO, session sessions.Session, catalog tracks.Catalog) sessions.DTO {
	if caps, ok := sessionTrackCapabilities(session, catalog); ok {
		dto.TrackCapabilities = &caps
	}

	return dto
}

func sessionTrackCapabilities(session sessions.Session, catalog tracks.Catalog) (tracks.LayoutCapabilities, bool) {
	trackID := session.TrackID
	if trackID == "" {
		trackID = session.DetectedTrackID
	}
	layoutID := session.EffectiveLayoutID()
	if trackID == "" || layoutID == "" {
		return tracks.LayoutCapabilities{}, false
	}

	return catalog.LayoutCapabilities(trackID, layoutID), true
}

func ingestFramesHandler(frameStore telemetry.Store, eventStore events.Repository, lapRepo laps.Repository, sampleRepo laps.SampleRepository, sessionRepo sessions.Repository, catalog tracks.Catalog, logger *slog.Logger, rejectionStore telemetry.RejectionSummaryStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("sessionId")
		session, err := validateSession(r.Context(), sessionRepo, sessionID, true)
		if err != nil {
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

			completedLaps := laps.ExtractCompleted(request.SessionID, result.Frames)
			if lapRepo != nil && len(completedLaps) > 0 {
				if err := lapRepo.UpsertCompleted(r.Context(), completedLaps); err != nil {
					logger.Error("failed to persist completed laps",
						"session_id", request.SessionID,
						"completed_laps", len(completedLaps),
						"error", err,
					)
					writeJSON(w, http.StatusInternalServerError, httperror.Envelope(httperror.Internal("lap persistence error")))
					return
				}
				if err := persistTelemetryGapCounts(r.Context(), frameStore, lapRepo, request.SessionID, completedLaps); err != nil {
					logger.Error("failed to persist telemetry gap counts",
						"session_id", request.SessionID,
						"completed_laps", len(completedLaps),
						"error", err,
					)
					writeJSON(w, http.StatusInternalServerError, httperror.Envelope(httperror.Internal("lap persistence error")))
					return
				}
			}
			if lapRepo != nil && sampleRepo != nil && len(completedLaps) > 0 {
				if err := persistLapSamples(r.Context(), frameStore, sampleRepo, request.SessionID, completedLaps); err != nil {
					logger.Error("failed to persist lap samples",
						"session_id", request.SessionID,
						"completed_laps", len(completedLaps),
						"error", err,
					)
					writeJSON(w, http.StatusInternalServerError, httperror.Envelope(httperror.Internal("lap sample persistence error")))
					return
				}
			}

			if eventStore != nil {
				accumulatedFrames, err := frameStore.Frames(r.Context(), request.SessionID)
				if err != nil {
					writeJSON(w, http.StatusInternalServerError, httperror.Envelope(httperror.Internal("frame persistence error")))
					return
				}

				var trackRef, layoutRef *events.CatalogRef
				if hasLapOneBegan(accumulatedFrames) {
					detectResult := tracks.DetectTrack(accumulatedFrames, catalog, tracks.DetectionOptions{})
					if detectResult.Status == tracks.DetectionStatusDetected &&
						detectResult.TrackID != nil && detectResult.TrackName != nil &&
						detectResult.LayoutID != nil && detectResult.LayoutName != nil {
						trackRef = &events.CatalogRef{
							ID:              detectResult.TrackID,
							Name:            detectResult.TrackName,
							DisplayStrategy: events.DisplayStrategyCatalogName,
						}
						layoutRef = &events.CatalogRef{
							ID:              detectResult.LayoutID,
							Name:            detectResult.LayoutName,
							DisplayStrategy: events.DisplayStrategyCatalogName,
						}

						if session.TrackID == "" && session.DetectedTrackID == "" {
							if _, err := sessionRepo.SetDetectedTrackLayout(r.Context(), sessionID, *detectResult.TrackID, *detectResult.LayoutID); err != nil {
								logger.Warn("failed to persist detected track/layout on session",
									"session_id", sessionID,
									"detected_track_id", *detectResult.TrackID,
									"detected_layout_id", *detectResult.LayoutID,
									"error", err,
								)
							}
						}
					}
				}

				generated, err := events.GenerateFrameEventsForAppendWithRefs(request.SessionID, accumulatedFrames, result.Frames, trackRef, layoutRef)
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

		if rejectionStore != nil && summary != nil && len(result.Rejections) > 0 {
			record := telemetry.RejectionSummaryRecord{
				SessionID:          request.SessionID,
				Status:             status,
				ReceivedFrames:     len(request.Frames),
				AcceptedFrames:     len(result.Frames),
				RejectedFrames:     len(result.Rejections),
				AcceptedFromUnixMs: fromUnixMs,
				AcceptedToUnixMs:   toUnixMs,
				Summary:            *summary,
				IngestedAt:         time.Now().UTC(),
			}
			if err := rejectionStore.Append(r.Context(), record); err != nil {
				logger.Warn("failed to persist ingest rejection summary",
					"session_id", request.SessionID,
					"rejected", len(result.Rejections),
					"status", status,
					"error", err,
				)
			}
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

		writeVersionedJSON(w, http.StatusAccepted, resp)

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

func persistTelemetryGapCounts(ctx context.Context, frameStore telemetry.Store, lapRepo laps.Repository, sessionID string, completedLaps []laps.CompletedLap) error {
	if frameStore == nil || lapRepo == nil || len(completedLaps) == 0 {
		return nil
	}
	accumulatedFrames, err := frameStore.Frames(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("load frames for telemetry gap counts: %w", err)
	}
	for _, lap := range completedLaps {
		count := laps.CountTelemetryGaps(lap, accumulatedFrames)
		if err := lapRepo.UpdateTelemetryGapCount(ctx, sessionID, lap.LapNumber, count); err != nil {
			return err
		}
	}
	return nil
}

func persistLapSamples(ctx context.Context, frameStore telemetry.Store, sampleRepo laps.SampleRepository, sessionID string, completedLaps []laps.CompletedLap) error {
	if frameStore == nil || sampleRepo == nil || len(completedLaps) == 0 {
		return nil
	}
	accumulatedFrames, err := frameStore.Frames(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("load frames for lap samples: %w", err)
	}
	for _, lap := range completedLaps {
		samples, ok := laps.BuildLapSamples(lap, accumulatedFrames, laps.DefaultSampleStepMeters)
		if !ok {
			continue
		}
		if err := sampleRepo.UpsertSamples(ctx, samples); err != nil {
			return err
		}
	}
	return nil
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
		if result.Status == tracks.DetectionStatusDetected && result.TrackID != nil && result.LayoutID != nil {
			caps := catalog.LayoutCapabilities(*result.TrackID, *result.LayoutID)
			result.Capabilities = &caps
		}
		writeVersionedJSON(w, http.StatusOK, result)
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

		writeVersionedJSON(w, http.StatusOK, events.ListResponse{SessionID: query.SessionID, Events: storedEvents})
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

		writeVersionedJSON(w, http.StatusOK, resp)
	}
}

func routesWithAI(cfg config.Config, logger *slog.Logger, frameStore telemetry.Store, catalog tracks.Catalog, eventStore events.Repository, aiSvc ai.AIService) http.Handler {
	sessionRepo := sessions.NewMemoryRepository()
	statsRepo := admin.NewMemoryStatsRepo(sessionRepo, frameStore)
	rejectionStore := telemetry.NewMemoryRejectionSummaryStore()
	summaryRepo := sessions.NewMemorySummaryRepository(sessionRepo, frameStore, eventStore, rejectionStore)
	lapRepo := laps.NewMemoryRepository()
	return routesWithAIAndSessionsAndLaps(cfg, logger, frameStore, catalog, eventStore, lapRepo, lapRepo, sessionRepo, aiSvc, statsRepo, summaryRepo, rejectionStore)
}

func routesWithAIAndSessions(cfg config.Config, logger *slog.Logger, frameStore telemetry.Store, catalog tracks.Catalog, eventStore events.Repository, sessionRepo sessions.Repository, aiSvc ai.AIService, statsRepo admin.StatsRepository, summaryRepo sessions.SummaryRepository, rejectionStores ...telemetry.RejectionSummaryStore) http.Handler {
	lapRepo := laps.NewMemoryRepository()
	return routesWithAIAndSessionsAndLaps(cfg, logger, frameStore, catalog, eventStore, lapRepo, lapRepo, sessionRepo, aiSvc, statsRepo, summaryRepo, rejectionStores...)
}

func routesWithAIAndSessionsAndLaps(cfg config.Config, logger *slog.Logger, frameStore telemetry.Store, catalog tracks.Catalog, eventStore events.Repository, lapRepo laps.Repository, sampleRepo laps.SampleRepository, sessionRepo sessions.Repository, aiSvc ai.AIService, statsRepo admin.StatsRepository, summaryRepo sessions.SummaryRepository, rejectionStores ...telemetry.RejectionSummaryStore) http.Handler {
	var rejectionStore telemetry.RejectionSummaryStore
	if len(rejectionStores) > 0 {
		rejectionStore = rejectionStores[0]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHandler(cfg))
	mux.HandleFunc("GET /api/v1/health", healthHandler(cfg))
	mux.HandleFunc("GET /api/v1/settings/bootstrap", settingsBootstrapHandler(cfg))
	mux.HandleFunc("GET /api/v1/catalog/track-layouts", catalogTrackLayoutsHandler(catalog))
	mux.HandleFunc("POST /api/v1/sessions", createSessionHandler(sessionRepo, catalog))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}", getSessionHandler(sessionRepo, frameStore, eventStore, catalog))
	mux.HandleFunc("PUT /api/v1/sessions/{sessionId}/track-layout", sessionTrackLayoutHandler(sessionRepo, catalog, frameStore, eventStore))
	mux.HandleFunc("POST /api/v1/sessions/{sessionId}/finish", finishSessionHandler(sessionRepo, frameStore, eventStore, catalog))
	mux.HandleFunc("POST /api/v1/sessions/{sessionId}/frames", ingestFramesHandler(frameStore, eventStore, lapRepo, sampleRepo, sessionRepo, catalog, logger, rejectionStore))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}/track", detectTrackHandler(frameStore, catalog, sessionRepo))
	mux.HandleFunc("GET /api/v1/sessions/{sessionId}/events", listEventsHandler(eventStore, sessionRepo))
	mux.HandleFunc("POST /api/v1/sessions/{sessionId}/analyze", analyzeHandler(aiSvc, sessionRepo))
	mux.HandleFunc("POST /api/v1/sessions/{sessionId}/race-engineer/advice", raceEngineerAdviceHandler(aiSvc, eventStore, sessionRepo, frameStore, catalog))
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

func validatedQueryParam(r *http.Request, name string, defaultVal, min, max int) (int, error) {
	values, ok := r.URL.Query()[name]
	if !ok {
		return defaultVal, nil
	}
	raw := values[0]
	if raw == "" {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	if val < min {
		return 0, fmt.Errorf("%s must be between %d and %d", name, min, max)
	}
	if val > max {
		return 0, fmt.Errorf("%s must be between %d and %d", name, min, max)
	}
	return val, nil
}

func ingestStatsHandler(statsRepo admin.StatsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, err := validatedQueryParam(r, "limit", admin.DefaultLimit, admin.MinLimit, admin.MaxLimit)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest(err.Error())))
			return
		}
		days, err := validatedQueryParam(r, "days", admin.DefaultDays, admin.MinDays, admin.MaxDays)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest(err.Error())))
			return
		}

		resp, err := statsRepo.Stats(r.Context(), limit, days)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, httperror.Envelope(httperror.Internal("failed to load ingest stats")))
			return
		}

		writeVersionedJSON(w, http.StatusOK, resp)
	}
}

func listSessionsHandler(summaryRepo sessions.SummaryRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, err := validatedQueryParam(r, "limit", sessions.DefaultListLimit, sessions.MinListLimit, sessions.MaxListLimit)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest(err.Error())))
			return
		}

		resp, err := summaryRepo.List(r.Context(), sessions.SummaryFilter{Limit: limit})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, httperror.Envelope(httperror.Internal("failed to list sessions")))
			return
		}

		if resp.Sessions == nil {
			resp.Sessions = []sessions.SessionSummaryItem{}
		}

		writeVersionedJSON(w, http.StatusOK, resp)
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

		writeVersionedJSON(w, http.StatusOK, resp)
	}
}

func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("http request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds())
	})
}
