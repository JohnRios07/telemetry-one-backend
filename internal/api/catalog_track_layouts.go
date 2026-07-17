package api

import (
	"net/http"

	"telemetry-one-backend/internal/tracks"
)

func catalogTrackLayoutsHandler(catalog tracks.Catalog) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeVersionedJSON(w, http.StatusOK, tracks.BuildCatalogTrackLayoutSummary(catalog))
	}
}
