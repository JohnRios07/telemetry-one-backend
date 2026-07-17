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

	threshold := telemetryGapThresholdMs(deltas)
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

func telemetryGapThresholdMs(values []int64) int64 {
	if len(values) == 0 {
		return telemetryGapFloorMs
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	mid := len(sorted) / 2
	var threshold int64
	if len(sorted)%2 == 1 {
		threshold = 4 * sorted[mid]
	} else {
		threshold = 2 * (sorted[mid-1] + sorted[mid])
	}
	if threshold < telemetryGapFloorMs {
		return telemetryGapFloorMs
	}
	return threshold
}
