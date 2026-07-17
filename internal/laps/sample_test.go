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

func TestBuildLapSamplesPreservesOptionalRightEndpointOnExactRightEdge(t *testing.T) {
	lap := CompletedLap{ID: "lap_session-1_1", SessionID: "session-1", LapNumber: 1, LapTimeMs: 90000, CompletedAtUnixMs: 3000}
	left := baseSampleFrame(1000, 1, float64Ptr(0))
	left.YawRate = nil
	right := baseSampleFrame(2000, 1, float64Ptr(5))
	right.YawRate = float64Ptr(0.25)

	samples, ok := BuildLapSamples(lap, []telemetry.Frame{left, right}, DefaultSampleStepMeters)
	if !ok {
		t.Fatalf("expected samples")
	}
	if len(samples) != 2 {
		t.Fatalf("expected buckets 0 and 5, got %+v", samples)
	}
	if samples[1].DistanceMeters != 5 {
		t.Fatalf("expected right edge bucket at 5m, got %+v", samples[1])
	}
	if samples[1].YawRate == nil || *samples[1].YawRate != 0.25 {
		t.Fatalf("expected right endpoint yaw rate 0.25 to be preserved, got %+v", samples[1].YawRate)
	}
}

func TestBuildLapSamplesPrefersLaterFrameForDuplicateExactDistanceBucket(t *testing.T) {
	lap := CompletedLap{ID: "lap_session-1_1", SessionID: "session-1", LapNumber: 1, LapTimeMs: 90000, CompletedAtUnixMs: 3000}
	left := baseSampleFrame(1000, 1, float64Ptr(0))
	duplicateEarly := baseSampleFrame(2000, 1, float64Ptr(5))
	duplicateEarly.YawRate = nil
	duplicateLate := baseSampleFrame(3000, 1, float64Ptr(5))
	duplicateLate.YawRate = float64Ptr(0.75)
	right := baseSampleFrame(4000, 1, float64Ptr(10))
	right.YawRate = float64Ptr(1)

	samples, ok := BuildLapSamples(lap, []telemetry.Frame{left, duplicateEarly, duplicateLate, right}, DefaultSampleStepMeters)
	if !ok {
		t.Fatalf("expected samples")
	}
	if len(samples) != 3 {
		t.Fatalf("expected buckets 0, 5, 10, got %+v", samples)
	}
	if samples[1].DistanceMeters != 5 {
		t.Fatalf("expected middle bucket at 5m, got %+v", samples[1])
	}
	if samples[1].YawRate == nil || *samples[1].YawRate != 0.75 {
		t.Fatalf("expected later duplicate yaw rate 0.75 to be preserved, got %+v", samples[1].YawRate)
	}
	if samples[1].TimestampUnixMs == nil || *samples[1].TimestampUnixMs != 3000 {
		t.Fatalf("expected later duplicate timestamp 3000 to be preserved, got %+v", samples[1].TimestampUnixMs)
	}
}

func TestInterpolateOptionalFloat64PreservesEndpointValues(t *testing.T) {
	rightValue := float64Ptr(12.5)
	rightZero := float64Ptr(0)
	leftZero := float64Ptr(0)

	tests := []struct {
		name      string
		left      *float64
		right     *float64
		ratio     float64
		wantNil   bool
		wantValue float64
	}{
		{name: "exact left endpoint keeps left nil", left: nil, right: rightValue, ratio: 0, wantNil: true},
		{name: "exact right endpoint keeps right value when left missing", left: nil, right: rightValue, ratio: 1, wantValue: 12.5},
		{name: "exact right endpoint keeps explicit zero", left: nil, right: rightZero, ratio: 1, wantValue: 0},
		{name: "exact left endpoint keeps explicit zero", left: leftZero, right: nil, ratio: 0, wantValue: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := interpolateOptionalFloat64(tt.left, tt.right, tt.ratio)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected value %v, got nil", tt.wantValue)
			}
			if *got != tt.wantValue {
				t.Fatalf("expected %v, got %v", tt.wantValue, *got)
			}
		})
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
