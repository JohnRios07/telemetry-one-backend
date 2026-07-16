package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"time"

	"telemetry-one-backend/internal/config"
	"telemetry-one-backend/internal/corners"
	"telemetry-one-backend/internal/events"
	"telemetry-one-backend/internal/sessions"
	"telemetry-one-backend/internal/telemetry"
	"telemetry-one-backend/internal/tracks"
)

func TestE2EPipelineFullFlow(t *testing.T) {
	ctx := context.Background()
	cfg := config.Config{Addr: ":0", Env: "test"}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	frameStore := telemetry.NewFrameStore(100)
	catalog := tracks.OfficialGT7SeedCatalog()
	eventStore := events.NewStore(100, events.DedupOptions{})
	sessionRepo := sessions.NewMemoryRepository()
	if _, err := sessionRepo.Create(ctx, sessions.Session{ID: "e2e-session", Source: "test", Game: "gt7", Platform: "ps5", StartedAt: time.UnixMilli(1720656000000).UTC()}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	handler := routesWithSessionRepository(cfg, logger, frameStore, catalog, eventStore, sessionRepo)

	var generatedEvents []events.EngineerEvent

	// ──────────────────────────────────────────────
	// Phase 1: Ingest 32 frames simulating Watkins Glen (5423m)
	// ──────────────────────────────────────────────
	t.Run("ingest", func(t *testing.T) {
		const lengthMeters = 5423.0
		const count = 32
		lastIndex := count - 1
		timestampStart := int64(1720656000001)
		frames := make([]telemetry.Frame, count)

		for i := 0; i < count; i++ {
			lapNumber := 1
			if i == lastIndex {
				lapNumber = 2
			}
			frames[i] = telemetry.Frame{
				TimestampUnixMs: timestampStart + int64(i),
				SpeedMps:        58.33,
				RPM:             7100,
				Gear:            4,
				Throttle:        0.7,
				Brake:           0,
				Steering:        -0.1,
				FuelLiters:      38.4,
				PositionX:       lengthMeters * float64(i) / float64(lastIndex),
				PositionY:       5.6,
				PositionZ:       789.1,
				LapNumber:       lapNumber,
				CurrentLapMs:    81234,
				IsOnTrack:       true,
			}
		}

		body, err := json.Marshal(telemetry.IngestBatchRequest{
			SessionID: "e2e-session",
			Frames:    frames,
		})
		if err != nil {
			t.Fatalf("failed to marshal ingest request: %v", err)
		}

		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/e2e-session/frames", bytes.NewReader(body))
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusAccepted {
			t.Fatalf("expected status %d, got %d with body %s", http.StatusAccepted, recorder.Code, recorder.Body.String())
		}

		var response telemetry.IngestBatchResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("failed to decode ingest response: %v", err)
		}
		if response.ReceivedFrames != count || response.AcceptedFrames != count || response.RejectedFrames != 0 {
			t.Fatalf("expected all %d frames accepted, got received=%d accepted=%d rejected=%d",
				count, response.ReceivedFrames, response.AcceptedFrames, response.RejectedFrames)
		}
	})

	// ──────────────────────────────────────────────
	// Phase 2: Detect track from ingested frames
	// ──────────────────────────────────────────────
	t.Run("detect", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/e2e-session/track", nil)
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
		}

		var result tracks.DetectionResult
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatalf("failed to decode detection result: %v", err)
		}
		if result.Status != tracks.DetectionStatusDetected {
			t.Fatalf("expected status %q, got %q", tracks.DetectionStatusDetected, result.Status)
		}
		if result.TrackID == nil || *result.TrackID != "gt7_watkins_glen_international" {
			t.Fatalf("expected trackId %q, got %+v", "gt7_watkins_glen_international", result.TrackID)
		}
		if result.LayoutID == nil || *result.LayoutID != "gt7_layout_1240" {
			t.Fatalf("expected layoutId %q, got %+v", "gt7_layout_1240", result.LayoutID)
		}
		if result.TrackName == nil || *result.TrackName == "" {
			t.Fatalf("expected non-nil trackName, got %+v", result.TrackName)
		}
		if result.LayoutName == nil || *result.LayoutName == "" {
			t.Fatalf("expected non-nil layoutName, got %+v", result.LayoutName)
		}

		foundLengthMatch := false
		for _, reason := range result.Reasons {
			if reason == tracks.DetectionReasonLengthMatch {
				foundLengthMatch = true
				break
			}
		}
		if !foundLengthMatch {
			t.Fatalf("expected reasons to contain %q, got %+v", tracks.DetectionReasonLengthMatch, result.Reasons)
		}
	})

	// ──────────────────────────────────────────────
	// Phase 3: Generate engineer events via rules engine
	// ──────────────────────────────────────────────
	t.Run("events-generate", func(t *testing.T) {
		input := events.RuleInput{
			SessionID:       "e2e-session",
			LapNumber:       3,
			TimestampUnixMs: 1720656012345,
			CornerAnalysis: corners.Analysis{
				Status:      corners.AnalysisStatusComplete,
				CornerID:    "e2e-corner-1",
				CornerName:  "E2E Test Corner",
				Number:      1,
				SampleCount: 10,
				EntrySpeedKph: corners.ScalarMetric{
					Status: corners.MetricStatusAvailable,
					Value:  120,
				},
				MinimumSpeedKph: corners.ScalarMetric{
					Status: corners.MetricStatusAvailable,
					Value:  80,
				},
				ExitSpeedKph: corners.ScalarMetric{
					Status: corners.MetricStatusAvailable,
					Value:  140,
				},
				MaxBrakePercent: corners.ScalarMetric{
					Status: corners.MetricStatusUnavailable,
					Reason: corners.ReasonMissingBrakeData,
				},
				FirstThrottleReapplication: corners.PointMetric{
					Status:            corners.MetricStatusAvailable,
					DistanceFromStart: 250,
					TimestampUnixMs:   1720656012345,
					TimeOffsetMs:      520,
				},
				ExitAccelerationDeltaKph: corners.ScalarMetric{
					Status: corners.MetricStatusUnavailable,
					Reason: corners.ReasonInsufficientSamples,
				},
			},
			Reference: &events.ReferenceMetrics{
				ExitSpeedKph: events.ScalarReference{
					Available: true,
					Value:     158,
				},
			},
			Track: &events.CatalogRef{
				ID:              apiStringPtr("gt7_watkins_glen_international"),
				Name:            apiStringPtr("Watkins Glen International"),
				DisplayStrategy: events.DisplayStrategyCatalogName,
			},
			Layout: &events.CatalogRef{
				ID:              apiStringPtr("gt7_watkins_glen_long_course"),
				Name:            apiStringPtr("Watkins Glen Long Course"),
				DisplayStrategy: events.DisplayStrategyCatalogName,
			},
			Corner: &events.CatalogRef{
				ID:              apiStringPtr("e2e-corner-1"),
				Name:            apiStringPtr("E2E Test Corner"),
				DisplayStrategy: events.DisplayStrategyCatalogName,
			},
		}

		var err error
		generatedEvents, err = events.Generate(input)
		if err != nil {
			t.Fatalf("events.Generate failed: %v", err)
		}
		if len(generatedEvents) == 0 {
			t.Fatal("expected at least one generated event, got none")
		}

		for _, event := range generatedEvents {
			_, accepted, err := eventStore.Append(ctx, event)
			if err != nil {
				t.Fatalf("failed to append event %s: %v", event.EventID, err)
			}
			if !accepted {
				t.Fatalf("expected event %s to be accepted on first append (session=%s lap=%d type=%s)",
					event.EventID, event.SessionID, event.LapNumber, event.Type)
			}
		}
	})

	// ──────────────────────────────────────────────
	// Phase 4: List events via HTTP and verify shape
	// ──────────────────────────────────────────────
	t.Run("events-list", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/e2e-session/events", nil)
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
		}

		body := recorder.Body.String()

		if !strings.Contains(body, `"version":"telemetry-one.engineer-event.v1"`) {
			t.Fatalf("expected version telemetry-one.engineer-event.v1, got %s", body)
		}
		if !strings.Contains(body, `"kind":"deterministic_rule"`) {
			t.Fatalf("expected source kind deterministic_rule, got %s", body)
		}

		for _, forbidden := range []string{`"positionX":`, `"speedMps":`, `"throttle":`, `"brake":`, `"steering":`} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("expected event response not to leak raw telemetry field %s in: %s", forbidden, body)
			}
		}
	})

	// ──────────────────────────────────────────────
	// Phase 5: Verify dedup suppresses re-append
	// ──────────────────────────────────────────────
	t.Run("dedup", func(t *testing.T) {
		for _, event := range generatedEvents {
			_, accepted, err := eventStore.Append(ctx, event)
			if err != nil {
				t.Fatalf("failed to append duplicate event %s: %v", event.EventID, err)
			}
			if accepted {
				t.Fatalf("expected duplicate event %s to be suppressed by dedup, but it was accepted", event.EventID)
			}
		}
	})
}
