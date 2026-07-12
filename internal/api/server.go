package api

import (
	"log/slog"
	"net/http"

	"telemetry-one-backend/internal/config"
)

func NewServer(cfg config.Config, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              cfg.Addr,
		Handler:           routes(cfg, logger),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}
}
