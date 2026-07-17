package laps

import (
	"sort"

	"telemetry-one-backend/internal/telemetry"
)

const telemetryGapFloorMs int64 = 250

func CountTelemetryGaps(lap CompletedLap, frames []telemetry.Frame) int {
	if len(frames) == 0 {
		return 0
	}

	lapFrames := make([]telemetry.Frame, 0, len(frames))
	for _, frame := range frames {
		if frame.LapNumber == lap.LapNumber {
			lapFrames = append(lapFrames, frame)
		}
	}
	if len(lapFrames) < 2 {
		return 0
	}

	sort.SliceStable(lapFrames, func(i, j int) bool {
		return lapFrames[i].TimestampUnixMs < lapFrames[j].TimestampUnixMs
	})

	deltas := positiveTimestampDeltas(lapFrames)
	if len(deltas) == 0 {
		return 0
	}

	threshold := telemetryGapThresholdMs(medianInt64(deltas))
	gapCount := 0
	for _, delta := range deltas {
		if delta > threshold {
			gapCount++
		}
	}
	return gapCount
}

func positiveTimestampDeltas(frames []telemetry.Frame) []int64 {
	deltas := make([]int64, 0, len(frames)-1)
	for i := 1; i < len(frames); i++ {
		delta := frames[i].TimestampUnixMs - frames[i-1].TimestampUnixMs
		if delta <= 0 {
			continue
		}
		deltas = append(deltas, delta)
	}
	return deltas
}

func medianInt64(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

func telemetryGapThresholdMs(expectedMs int64) int64 {
	threshold := 4 * expectedMs
	if threshold < telemetryGapFloorMs {
		return telemetryGapFloorMs
	}
	return threshold
}
