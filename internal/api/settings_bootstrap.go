package api

import (
	"net/http"

	"telemetry-one-backend/internal/config"
	"telemetry-one-backend/internal/telemetry"
)

const defaultBootstrapUnits = "metric"

type settingsBootstrapResponse struct {
	Bootstrap settingsBootstrap `json:"bootstrap"`
}

type settingsBootstrap struct {
	ClientHints  settingsBootstrapClientHints  `json:"clientHints"`
	Limits       settingsBootstrapLimits       `json:"limits"`
	Capabilities settingsBootstrapCapabilities `json:"capabilities"`
}

type settingsBootstrapClientHints struct {
	Alias string `json:"alias"`
	Units string `json:"units"`
}

type settingsBootstrapLimits struct {
	MaxBatchFrames           int `json:"maxBatchFrames"`
	RetainedFramesPerSession int `json:"retainedFramesPerSession"`
}

type settingsBootstrapCapabilities struct {
	ReadOnly             bool `json:"readOnly"`
	AcceptsPartialIngest bool `json:"acceptsPartialIngest"`
	WriteAPI             bool `json:"writeApi"`
}

func settingsBootstrapHandler(cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeVersionedJSON(w, http.StatusOK, settingsBootstrapResponse{
			Bootstrap: buildSettingsBootstrap(cfg),
		})
	}
}

func buildSettingsBootstrap(cfg config.Config) settingsBootstrap {
	retainedFramesPerSession := cfg.RetainedFramesPerSession
	if retainedFramesPerSession <= 0 {
		retainedFramesPerSession = config.DefaultRetainedFramesPerSession
	}

	return settingsBootstrap{
		ClientHints: settingsBootstrapClientHints{
			Alias: "",
			Units: defaultBootstrapUnits,
		},
		Limits: settingsBootstrapLimits{
			MaxBatchFrames:           telemetry.MaxBatchFrames,
			RetainedFramesPerSession: retainedFramesPerSession,
		},
		Capabilities: settingsBootstrapCapabilities{
			ReadOnly:             true,
			AcceptsPartialIngest: true,
			WriteAPI:             false,
		},
	}
}
