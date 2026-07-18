package api

import (
	"errors"
	"net/http"

	"telemetry-one-backend/internal/events"
	"telemetry-one-backend/internal/platform/httperror"
	"telemetry-one-backend/internal/sessions"
	"telemetry-one-backend/internal/telemetry"
	"telemetry-one-backend/internal/tracks"
)

type sessionTrackLayoutRequest struct {
	TrackID  string `json:"trackId"`
	LayoutID string `json:"layoutId"`
}

func (r sessionTrackLayoutRequest) Validate() error {
	if r.TrackID == "" {
		return errors.New("trackId is required")
	}
	if r.LayoutID == "" {
		return errors.New("layoutId is required")
	}

	return nil
}

func sessionTrackLayoutHandler(sessionRepo sessions.Repository, catalog tracks.Catalog, frameStore telemetry.Store, eventStore events.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request sessionTrackLayoutRequest
		if err := decodeJSON(r, &request); err != nil {
			writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest("invalid JSON body")))
			return
		}
		if err := request.Validate(); err != nil {
			writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest(err.Error())))
			return
		}

		if _, _, ok := tracks.FindSelectableTrackLayout(catalog, request.TrackID, request.LayoutID); !ok {
			writeJSON(w, http.StatusBadRequest, httperror.Envelope(httperror.BadRequest("trackId and layoutId must match a sourced catalog layout")))
			return
		}

		sessionID := r.PathValue("sessionId")
		if _, err := validateSession(r.Context(), sessionRepo, sessionID, false); err != nil {
			writeSessionError(w, err)
			return
		}

		updated, err := sessionRepo.SetTrackLayout(r.Context(), sessionID, request.TrackID, request.LayoutID)
		if err != nil {
			writeSessionError(w, err)
			return
		}

		// Preserve the same success-object shape as other session reads.
		dto, err := sessionDTO(r, updated, frameStore, eventStore, catalog)
		if err != nil {
			writeSessionDTOError(w, err)
			return
		}

		writeVersionedJSON(w, http.StatusOK, sessions.Response{Session: dto})
	}
}
