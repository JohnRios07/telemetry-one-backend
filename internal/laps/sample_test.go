package laps

import (
	"testing"

	"telemetry-one-backend/internal/telemetry"
)

func TestBuildLapSamplesSkipsMissingAndNonMonotonicDistance(t *testing.T) {
	lap := CompletedLap{ID: "lap_session-1_1", SessionID: "session-1", LapNumber: 1, LapTimeMs: 90000, CompletedAtUnixMs: 3000}

	tests := []struct {
		name   string
		frames []telemetry.Frame
	}{
		{
			name: "missing distance",
			frames: []telemetry.Frame{
				baseSampleFrame(1000, 1, nil),
				baseSampleFrame(2000, 1, nil),
			},
		},
		{
			name: "non monotonic distance",
			frames: []telemetry.Frame{
				baseSampleFrame(1000, 1, float64Ptr(10)),
				baseSampleFrame(2000, 1, float64Ptr(5)),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := BuildLapSamples(lap, tt.frames, DefaultSampleStepMeters)
			if ok || len(got) != 0 {
				t.Fatalf("expected skip, got ok=%v samples=%+v", ok, got)
			}
		})
	}
}

func TestBuildLapSamplesInterpolatesFiveMeterBucketsAndPreservesZeroVsNil(t *testing.T) {
	lap := CompletedLap{ID: "lap_session-1_1", SessionID: "session-1", LapNumber: 1, LapTimeMs: 90000, CompletedAtUnixMs: 3000}
	left := baseSampleFrame(1000, 1, float64Ptr(0))
	left.SpeedMps = 0
	left.RPM = 1000
	left.Brake = 0
	left.YawRate = float64Ptr(0)
	right := baseSampleFrame(2000, 1, float64Ptr(10))
	right.SpeedMps = 10
	right.RPM = 3000
	right.Brake = 1
	right.YawRate = nil

	samples, ok := BuildLapSamples(lap, []telemetry.Frame{left, right}, DefaultSampleStepMeters)
	if !ok {
		t.Fatalf("expected samples")
	}
	if len(samples) != 3 {
		t.Fatalf("expected buckets 0,5,10, got %+v", samples)
	}
	mid := samples[1]
	if mid.DistanceMeters != 5 {
		t.Fatalf("expected middle bucket at 5m, got %+v", mid)
	}
	if mid.SpeedMps == nil || *mid.SpeedMps != 5 {
		t.Fatalf("expected interpolated speed 5, got %+v", mid.SpeedMps)
	}
	if mid.RPM == nil || *mid.RPM != 2000 {
		t.Fatalf("expected interpolated rpm 2000, got %+v", mid.RPM)
	}
	if mid.Brake == nil || *mid.Brake != 0.5 {
		t.Fatalf("expected interpolated brake 0.5, got %+v", mid.Brake)
	}
	if samples[0].SpeedMps == nil || *samples[0].SpeedMps != 0 {
		t.Fatalf("expected explicit zero speed to stay present, got %+v", samples[0].SpeedMps)
	}
	if samples[0].YawRate == nil || *samples[0].YawRate != 0 {
		t.Fatalf("expected explicit optional zero yaw rate to stay present, got %+v", samples[0].YawRate)
	}
	if mid.YawRate != nil {
		t.Fatalf("expected optional yaw rate to stay nil when interpolation endpoint is missing, got %+v", mid.YawRate)
	}
}

func baseSampleFrame(timestamp int64, lapNumber int, distance *float64) telemetry.Frame {
	return telemetry.Frame{
		TimestampUnixMs:   timestamp,
		SpeedMps:          1,
		RPM:               1000,
		Gear:              2,
		Throttle:          0.5,
		Brake:             0,
		Steering:          0,
		FuelLiters:        10,
		PositionX:         1,
		PositionY:         2,
		PositionZ:         3,
		LapDistanceMeters: distance,
		LapNumber:         lapNumber,
		CurrentLapMs:      timestamp,
		IsOnTrack:         true,
	}
}
