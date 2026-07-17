package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"telemetry-one-backend/internal/config"
	"telemetry-one-backend/internal/telemetry"
)

func TestSettingsBootstrapReturnsVersionedWhitelistResponse(t *testing.T) {
	handler := routes(config.Config{Addr: ":0", Env: "test", RetainedFramesPerSession: 321}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/settings/bootstrap", nil)

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response struct {
		APIVersion string `json:"apiVersion"`
		Bootstrap  struct {
			ClientHints struct {
				Alias string `json:"alias"`
				Units string `json:"units"`
			} `json:"clientHints"`
			Limits struct {
				MaxBatchFrames           int `json:"maxBatchFrames"`
				RetainedFramesPerSession int `json:"retainedFramesPerSession"`
			} `json:"limits"`
			Capabilities struct {
				ReadOnly             bool `json:"readOnly"`
				AcceptsPartialIngest bool `json:"acceptsPartialIngest"`
				WriteAPI             bool `json:"writeApi"`
			} `json:"capabilities"`
		} `json:"bootstrap"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("expected valid JSON: %v", err)
	}

	if response.APIVersion != responseAPIVersion {
		t.Fatalf("expected api version %q, got %q", responseAPIVersion, response.APIVersion)
	}
	if response.Bootstrap.ClientHints.Alias != "" || response.Bootstrap.ClientHints.Units != defaultBootstrapUnits {
		t.Fatalf("unexpected client hints: %+v", response.Bootstrap.ClientHints)
	}
	if response.Bootstrap.Limits.MaxBatchFrames != telemetry.MaxBatchFrames || response.Bootstrap.Limits.RetainedFramesPerSession != 321 {
		t.Fatalf("unexpected limits: %+v", response.Bootstrap.Limits)
	}
	if !response.Bootstrap.Capabilities.ReadOnly || !response.Bootstrap.Capabilities.AcceptsPartialIngest || response.Bootstrap.Capabilities.WriteAPI {
		t.Fatalf("unexpected capabilities: %+v", response.Bootstrap.Capabilities)
	}

	for _, forbidden := range []string{"TELEMETRY_ONE_DATABASE_URL", "OpenRouter", "AdminToken", "provider", "raw telemetry"} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("expected bootstrap response to omit %q, got %s", forbidden, recorder.Body.String())
		}
	}
}

func TestSettingsBootstrapFallsBackWhenRetainedFrameLimitMissing(t *testing.T) {
	handler := settingsBootstrapHandler(config.Config{})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/settings/bootstrap", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response struct {
		Bootstrap struct {
			Limits struct {
				RetainedFramesPerSession int `json:"retainedFramesPerSession"`
			} `json:"limits"`
		} `json:"bootstrap"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("expected valid JSON: %v", err)
	}
	if response.Bootstrap.Limits.RetainedFramesPerSession != config.DefaultRetainedFramesPerSession {
		t.Fatalf("expected fallback retained frame limit %d, got %d", config.DefaultRetainedFramesPerSession, response.Bootstrap.Limits.RetainedFramesPerSession)
	}
}

func TestSettingsBootstrapRejectsWriteVerbs(t *testing.T) {
	handler := routes(config.Config{Addr: ":0", Env: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(method, "/api/v1/settings/bootstrap", nil))

			if recorder.Code != http.StatusMethodNotAllowed {
				t.Fatalf("expected status %d for %s, got %d", http.StatusMethodNotAllowed, method, recorder.Code)
			}
		})
	}
}
