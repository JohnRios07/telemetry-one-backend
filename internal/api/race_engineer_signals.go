package api

import (
	"context"
	"fmt"
	"math"
	"sort"

	"telemetry-one-backend/internal/ai"
	"telemetry-one-backend/internal/events"
	"telemetry-one-backend/internal/laps"
)

func listRaceEngineerAdviceCompletedLaps(ctx context.Context, lapRepo laps.Repository, sessionID string) []laps.CompletedLap {
	if lapRepo == nil || sessionID == "" {
		return nil
	}

	completedLaps, err := lapRepo.ListBySession(ctx, sessionID)
	if err != nil {
		return nil
	}

	return completedLaps
}

func buildRaceEngineerAdviceSignals(storedEvents []events.EngineerEvent, completedLaps []laps.CompletedLap) []ai.Signal {
	signals := make([]ai.Signal, 0, 3)
	if signal := buildLapPaceRegressionSignal(completedLaps); signal != nil {
		signals = append(signals, *signal)
	}
	if signal := buildTelemetryGapSignal(completedLaps); signal != nil {
		signals = append(signals, *signal)
	}
	if signal := buildOffTrackSignal(storedEvents); signal != nil {
		signals = append(signals, *signal)
	}

	return signals
}

func buildLapPaceRegressionSignal(completedLaps []laps.CompletedLap) *ai.Signal {
	if len(completedLaps) < 2 {
		return nil
	}

	sorted := append([]laps.CompletedLap(nil), completedLaps...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].LapNumber == sorted[j].LapNumber {
			return sorted[i].CompletedAtUnixMs < sorted[j].CompletedAtUnixMs
		}
		return sorted[i].LapNumber < sorted[j].LapNumber
	})

	latest := sorted[len(sorted)-1]
	best := sorted[0]
	for _, lap := range sorted[1:] {
		if lap.LapTimeMs > 0 && lap.LapTimeMs < best.LapTimeMs {
			best = lap
		}
	}

	if latest.LapTimeMs <= 0 || best.LapTimeMs <= 0 {
		return nil
	}

	thresholdMs := maxInt64(1500, int64(math.Ceil(float64(best.LapTimeMs)*0.03)))
	bestDeltaMs := latest.LapTimeMs - best.LapTimeMs
	referenceLabel := "session best"
	deltaMs := bestDeltaMs
	if recentAverageMs, ok := recentAverageLapMs(sorted, 3); ok {
		recentDeltaMs := latest.LapTimeMs - recentAverageMs
		details := []string{
			fmt.Sprintf("latestLapMs=%d", latest.LapTimeMs),
			fmt.Sprintf("bestLapMs=%d", best.LapTimeMs),
			fmt.Sprintf("bestDeltaMs=%d", bestDeltaMs),
			fmt.Sprintf("recentAverageLapMs=%d", recentAverageMs),
			fmt.Sprintf("recentDeltaMs=%d", recentDeltaMs),
		}
		if bestDeltaMs < thresholdMs && recentDeltaMs < thresholdMs {
			return nil
		}
		if recentDeltaMs >= thresholdMs && recentDeltaMs > bestDeltaMs {
			deltaMs = recentDeltaMs
			referenceLabel = "recent average"
		}
		severity := events.SeverityMedium
		if deltaMs >= thresholdMs*2 {
			severity = events.SeverityHigh
		}
		return &ai.Signal{
			Kind:     "lap_pace_regression",
			Severity: severity,
			Summary:  fmt.Sprintf("Latest completed lap is %s slower than the %s.", formatDurationMs(deltaMs), referenceLabel),
			Details:  details,
		}
	}

	if bestDeltaMs < thresholdMs {
		return nil
	}

	details := []string{
		fmt.Sprintf("latestLapMs=%d", latest.LapTimeMs),
		fmt.Sprintf("bestLapMs=%d", best.LapTimeMs),
		fmt.Sprintf("bestDeltaMs=%d", bestDeltaMs),
	}

	severity := events.SeverityMedium
	if bestDeltaMs >= thresholdMs*2 {
		severity = events.SeverityHigh
	}

	return &ai.Signal{
		Kind:     "lap_pace_regression",
		Severity: severity,
		Summary:  fmt.Sprintf("Latest completed lap is %s slower than the session best.", formatDurationMs(bestDeltaMs)),
		Details:  details,
	}
}

func buildTelemetryGapSignal(completedLaps []laps.CompletedLap) *ai.Signal {
	if len(completedLaps) == 0 {
		return nil
	}

	totalGaps := 0
	affectedLaps := 0
	worstLapGaps := 0
	latestAffectedLap := 0
	for _, lap := range completedLaps {
		if lap.TelemetryGapCount <= 0 {
			continue
		}
		affectedLaps++
		totalGaps += lap.TelemetryGapCount
		if lap.TelemetryGapCount > worstLapGaps {
			worstLapGaps = lap.TelemetryGapCount
		}
		if lap.LapNumber > latestAffectedLap {
			latestAffectedLap = lap.LapNumber
		}
	}
	if totalGaps == 0 {
		return nil
	}
	if totalGaps < 2 && worstLapGaps < 2 {
		return nil
	}

	severity := events.SeverityLow
	if totalGaps >= 4 || worstLapGaps >= 3 {
		severity = events.SeverityHigh
	} else if totalGaps >= 2 || affectedLaps >= 2 {
		severity = events.SeverityMedium
	}

	details := []string{
		fmt.Sprintf("affectedLaps=%d", affectedLaps),
		fmt.Sprintf("totalGapCount=%d", totalGaps),
		fmt.Sprintf("worstLapGapCount=%d", worstLapGaps),
	}
	if latestAffectedLap > 0 {
		details = append(details, fmt.Sprintf("latestAffectedLap=%d", latestAffectedLap))
	}

	return &ai.Signal{
		Kind:     "telemetry_gap_warning",
		Severity: severity,
		Summary:  fmt.Sprintf("Telemetry gaps were recorded on %d completed lap(s).", affectedLaps),
		Details:  details,
	}
}

func buildOffTrackSignal(storedEvents []events.EngineerEvent) *ai.Signal {
	count := 0
	latestTimestamp := int64(0)
	severity := events.SeverityLow
	for _, event := range storedEvents {
		if event.Type != events.TypeOffTrackStint {
			continue
		}
		count++
		if event.TimestampUnixMs > latestTimestamp {
			latestTimestamp = event.TimestampUnixMs
		}
		if event.Severity == events.SeverityHigh {
			severity = events.SeverityHigh
		} else if event.Severity == events.SeverityMedium && severity != events.SeverityHigh {
			severity = events.SeverityMedium
		}
	}
	if count == 0 {
		return nil
	}
	if count >= 2 && severity != events.SeverityHigh {
		severity = events.SeverityMedium
	}

	details := []string{fmt.Sprintf("eventCount=%d", count)}
	if latestTimestamp > 0 {
		details = append(details, fmt.Sprintf("latestEventUnixMs=%d", latestTimestamp))
	}

	return &ai.Signal{
		Kind:     "off_track_stint_warning",
		Severity: severity,
		Summary:  fmt.Sprintf("Off-track stints are still appearing (%d event(s)).", count),
		Details:  details,
	}
}

func recentAverageLapMs(completedLaps []laps.CompletedLap, limit int) (int64, bool) {
	if len(completedLaps) == 0 || limit <= 0 {
		return 0, false
	}
	if limit > len(completedLaps) {
		limit = len(completedLaps)
	}

	window := completedLaps[len(completedLaps)-limit:]
	var total int64
	count := 0
	for _, lap := range window {
		if lap.LapTimeMs <= 0 {
			continue
		}
		total += lap.LapTimeMs
		count++
	}
	if count == 0 {
		return 0, false
	}

	return total / int64(count), true
}

func formatDurationMs(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	seconds := float64(ms) / 1000.0
	return fmt.Sprintf("%.1fs", seconds)
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
