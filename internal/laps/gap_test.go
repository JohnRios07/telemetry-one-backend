package laps

import (
	"testing"

	"telemetry-one-backend/internal/telemetry"
)

func TestCountTelemetryGaps(t *testing.T) {
	tests := []struct {
		name   string
		lap    CompletedLap
		frames []telemetry.Frame
		want   int
	}{
		{
			name: "counts median threshold breach",
			lap:  CompletedLap{LapNumber: 1},
			frames: []telemetry.Frame{
				gapFrame(1000, 1),
				gapFrame(1100, 1),
				gapFrame(1200, 1),
				gapFrame(1800, 1),
				gapFrame(1900, 1),
			},
			want: 1,
		},
		{
			name: "uses 250ms floor threshold",
			lap:  CompletedLap{LapNumber: 1},
			frames: []telemetry.Frame{
				gapFrame(1000, 1),
				gapFrame(1010, 1),
				gapFrame(1020, 1),
				gapFrame(1280, 1),
			},
			want: 1,
		},
		{
			name: "preserves fractional even median threshold",
			lap:  CompletedLap{LapNumber: 1},
			frames: []telemetry.Frame{
				gapFrame(1000, 1),
				gapFrame(1100, 1),
				gapFrame(1200, 1),
				gapFrame(1301, 1),
				gapFrame(1702, 1),
			},
			want: 0,
		},
		{
			name: "counts gap above preserved fractional threshold",
			lap:  CompletedLap{LapNumber: 1},
			frames: []telemetry.Frame{
				gapFrame(1000, 1),
				gapFrame(1100, 1),
				gapFrame(1200, 1),
				gapFrame(1301, 1),
				gapFrame(1704, 1),
			},
			want: 1,
		},
		{
			name: "no breach returns zero",
			lap:  CompletedLap{LapNumber: 1},
			frames: []telemetry.Frame{
				gapFrame(1000, 1),
				gapFrame(1100, 1),
				gapFrame(1200, 1),
				gapFrame(1300, 1),
			},
			want: 0,
		},
		{
			name: "insufficient timestamps returns zero",
			lap:  CompletedLap{LapNumber: 1},
			frames: []telemetry.Frame{
				gapFrame(1000, 1),
			},
			want: 0,
		},
		{
			name: "non monotonic duplicate timestamps excluded",
			lap:  CompletedLap{LapNumber: 1},
			frames: []telemetry.Frame{
				gapFrame(1000, 1),
				gapFrame(1000, 1),
				gapFrame(1050, 1),
				gapFrame(1100, 1),
				gapFrame(1700, 1),
				gapFrame(1750, 1),
			},
			want: 1,
		},
		{
			name: "different lap frames are excluded",
			lap:  CompletedLap{LapNumber: 1},
			frames: []telemetry.Frame{
				gapFrame(1000, 1),
				gapFrame(1050, 1),
				gapFrame(1100, 1),
				gapFrame(1150, 1),
				gapFrame(1600, 2),
				gapFrame(1700, 1),
				gapFrame(1750, 1),
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CountTelemetryGaps(tt.lap, tt.frames); got != tt.want {
				t.Fatalf("CountTelemetryGaps() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCountTelemetryGapsCountsCrossBatchEvidenceOnceOnReplay(t *testing.T) {
	lap := CompletedLap{LapNumber: 1}
	frames := []telemetry.Frame{
		gapFrame(1000, 1),
		gapFrame(1050, 1),
		gapFrame(1100, 1),
		gapFrame(1150, 1),
		gapFrame(1700, 1),
		gapFrame(1750, 1),
	}
	replayed := append(append([]telemetry.Frame{}, frames...), frames...)

	if got := CountTelemetryGaps(lap, replayed); got != 1 {
		t.Fatalf("expected replayed accepted frames to keep one gap, got %d", got)
	}
}

func gapFrame(timestamp int64, lapNumber int) telemetry.Frame {
	return telemetry.Frame{TimestampUnixMs: timestamp, LapNumber: lapNumber}
}
