package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"telemetry-one-backend/internal/config"
	"telemetry-one-backend/internal/platform/httperror"
	"telemetry-one-backend/internal/sessionexport"
	"telemetry-one-backend/internal/sessions"
)

func sessionExportHandler(cfg config.Config, exporter sessionexport.Exporter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !cfg.EnableSessionExport {
			writeJSON(w, http.StatusForbidden, httperror.Envelope(httperror.Error{Code: "forbidden", Message: "session export is disabled"}))
			return
		}

		result, err := exporter.Export(r.Context(), r.PathValue("sessionId"))
		if err != nil {
			writeSessionExportError(w, err)
			return
		}

		body, err := sessionexport.MarshalRequest(result.Request)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, httperror.Envelope(httperror.Internal("failed to encode export request")))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, exportAttachmentFilename(result.Session.ID)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}

func writeSessionExportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sessionexport.ErrMissingSession), errors.Is(err, sessions.ErrNotFound):
		writeJSON(w, http.StatusNotFound, httperror.Envelope(httperror.Error{Code: "session_not_found", Message: err.Error()}))
	case errors.Is(err, sessionexport.ErrActiveSessionBlocked):
		writeJSON(w, http.StatusConflict, httperror.Envelope(httperror.Error{Code: "session_export_blocked", Message: "session is still active"}))
	case errors.Is(err, sessionexport.ErrEmptyFrames), errors.Is(err, sessionexport.ErrNonMonotonicTimestamps):
		writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest(err.Error())))
	default:
		writeJSON(w, http.StatusInternalServerError, httperror.Envelope(httperror.Internal("session export failed")))
	}
}

func exportAttachmentFilename(sessionID string) string {
	cleaned := strings.NewReplacer("/", "_", "\\", "_", "\"", "_", " ", "_").Replace(strings.TrimSpace(sessionID))
	if cleaned == "" {
		cleaned = "session"
	}

	return fmt.Sprintf("session-%s-trackbuilder.json", cleaned)
}
