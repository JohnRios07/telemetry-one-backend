package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"telemetry-one-backend/internal/config"
	"telemetry-one-backend/internal/telemetry"
)

func BenchmarkIngestFramesHTTP(b *testing.B) {
	for _, size := range []int{20, 60, 300, telemetry.MaxBatchFrames} {
		b.Run(strconv.Itoa(size)+"_frames", func(b *testing.B) {
			body := benchmarkIngestBody(b, size)
			handler := routesWithFrameStore(
				config.Config{Addr: ":0", Env: "bench", RetainedFramesPerSession: 12000},
				slog.New(slog.NewTextHandler(io.Discard, nil)),
				telemetry.NewFrameStore(12000),
			)

			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/session-bench/frames", bytes.NewReader(body))

				handler.ServeHTTP(recorder, request)

				if recorder.Code != http.StatusAccepted {
					b.Fatalf("expected status %d, got %d with body %s", http.StatusAccepted, recorder.Code, recorder.Body.String())
				}
			}
		})
	}
}

func benchmarkIngestBody(b *testing.B, count int) []byte {
	b.Helper()

	body, err := json.Marshal(telemetry.IngestBatchRequest{
		SessionID: "session-bench",
		Frames:    benchmarkAPIFrames(count),
	})
	if err != nil {
		b.Fatal(err)
	}
	return body
}

func benchmarkAPIFrames(count int) []telemetry.Frame {
	frames := make([]telemetry.Frame, count)
	for i := 0; i < count; i++ {
		frames[i] = benchmarkAPIFrame(i)
	}
	return frames
}

func benchmarkAPIFrame(index int) telemetry.Frame {
	yaw := 1.57
	yawRate := 0.03
	wheelSpeedFL := 58.1
	wheelSpeedFR := 58.2
	wheelSpeedRL := 58.4
	wheelSpeedRR := 58.3
	lastLapMs := int64(91345)
	bestLapMs := int64(90210)

	return telemetry.Frame{
		TimestampUnixMs: 1720656000000 + int64(index),
		SpeedMps:        58.33,
		RPM:             7100,
		Gear:            4,
		Throttle:        0.7,
		Brake:           0.2,
		Steering:        -0.12,
		FuelLiters:      38.4,
		PositionX:       123.4 + float64(index),
		PositionY:       5.6,
		PositionZ:       789.1,
		YawRadians:      &yaw,
		YawRate:         &yawRate,
		WheelSpeedFL:    &wheelSpeedFL,
		WheelSpeedFR:    &wheelSpeedFR,
		WheelSpeedRL:    &wheelSpeedRL,
		WheelSpeedRR:    &wheelSpeedRR,
		LapNumber:       1,
		CurrentLapMs:    81234 + int64(index),
		LastLapMs:       &lastLapMs,
		BestLapMs:       &bestLapMs,
		IsOnTrack:       true,
	}
}
