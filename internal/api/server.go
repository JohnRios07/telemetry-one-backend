package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"telemetry-one-backend/internal/admin"
	"telemetry-one-backend/internal/ai"
	"telemetry-one-backend/internal/config"
	"telemetry-one-backend/internal/events"
	"telemetry-one-backend/internal/laps"
	"telemetry-one-backend/internal/persistence"
	"telemetry-one-backend/internal/sessions"
	"telemetry-one-backend/internal/telemetry"
	"telemetry-one-backend/internal/tracks"
)

func NewServer(ctx context.Context, cfg config.Config, logger *slog.Logger) (*http.Server, error) {
	handler, cleanup, err := runtimeHandler(ctx, cfg, logger)
	if err != nil {
		return nil, err
	}

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}
	if cleanup != nil {
		server.RegisterOnShutdown(cleanup)
	}

	return server, nil
}

func runtimeHandler(ctx context.Context, cfg config.Config, logger *slog.Logger) (http.Handler, func(), error) {
	if cfg.DatabaseURL == "" {
		return routes(cfg, logger), nil, nil
	}

	db, err := persistence.OpenPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("open postgres runtime: %w", err)
	}

	frameStore := telemetry.NewPostgresStore(db.Pool)
	rejectionStore := telemetry.NewPostgresRejectionSummaryStore(db.Pool)
	catalog := tracks.OfficialGT7SeedCatalog()
	eventStore := events.NewPostgresRepository(db.Pool, events.DedupOptions{})
	lapRepo := laps.NewPostgresRepository(db.Pool)
	sessionRepo := sessions.NewPostgresRepository(db.Pool)
	aiSvc := ai.ComposePipelineWithAudit(ai.PipelineConfigFromConfig(cfg), logger, ai.NewPostgresAuditStore(db.Pool))
	statsRepo := admin.NewPostgresStatsRepo(db.Pool)
	summaryRepo := sessions.NewPostgresSummaryRepository(db.Pool, rejectionStore)

	return routesWithAIAndSessionsAndLaps(cfg, logger, frameStore, catalog, eventStore, lapRepo, sessionRepo, aiSvc, statsRepo, summaryRepo, rejectionStore), db.Close, nil
}
