package laps

import (
	"context"
	"testing"
)

func TestMemoryRepositoryUpsertIsIdempotentAndListIsSorted(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()

	if err := repo.UpsertCompleted(ctx, []CompletedLap{
		{SessionID: "session-1", LapNumber: 2, LapTimeMs: 92000, CompletedAtUnixMs: 2000},
		{SessionID: "session-1", LapNumber: 1, LapTimeMs: 91000, CompletedAtUnixMs: 1000},
	}); err != nil {
		t.Fatalf("upsert completed laps: %v", err)
	}
	if err := repo.UpsertCompleted(ctx, []CompletedLap{{SessionID: "session-1", LapNumber: 1, LapTimeMs: 99999, CompletedAtUnixMs: 9999}}); err != nil {
		t.Fatalf("upsert duplicate completed lap: %v", err)
	}

	got, err := repo.ListBySession(ctx, "session-1")
	if err != nil {
		t.Fatalf("list completed laps: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected two unique laps, got %+v", got)
	}
	if got[0].LapNumber != 1 || got[0].LapTimeMs != 91000 || got[1].LapNumber != 2 {
		t.Fatalf("expected sorted laps with original duplicate value preserved, got %+v", got)
	}
}

func TestMemoryRepositorySamplesAreIdempotentSortedAndPreserveNilVsZero(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	zero := 0.0
	speed := 12.5

	if err := repo.UpsertSamples(ctx, []LapSample{
		{LapID: "lap-1", DistanceMeters: 5, Source: LapSampleSourceExplicitLapDistance, SpeedMps: &speed, YawRate: nil},
		{LapID: "lap-1", DistanceMeters: 0, Source: LapSampleSourceExplicitLapDistance, SpeedMps: &zero, YawRate: &zero},
	}); err != nil {
		t.Fatalf("upsert samples: %v", err)
	}
	if err := repo.UpsertSamples(ctx, []LapSample{{LapID: "lap-1", DistanceMeters: 5, Source: LapSampleSourceExplicitLapDistance, SpeedMps: &zero}}); err != nil {
		t.Fatalf("upsert duplicate sample: %v", err)
	}

	got, err := repo.ListSamples(ctx, "lap-1")
	if err != nil {
		t.Fatalf("list samples: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected two unique samples, got %+v", got)
	}
	if got[0].DistanceMeters != 0 || got[1].DistanceMeters != 5 {
		t.Fatalf("expected samples sorted by distance, got %+v", got)
	}
	if got[0].SpeedMps == nil || *got[0].SpeedMps != 0 {
		t.Fatalf("expected explicit zero speed preserved, got %+v", got[0].SpeedMps)
	}
	if got[0].YawRate == nil || *got[0].YawRate != 0 {
		t.Fatalf("expected explicit zero yaw rate preserved, got %+v", got[0].YawRate)
	}
	if got[1].YawRate != nil {
		t.Fatalf("expected nil yaw rate preserved, got %+v", got[1].YawRate)
	}
	if got[1].SpeedMps == nil || *got[1].SpeedMps != speed {
		t.Fatalf("expected duplicate sample to preserve original speed, got %+v", got[1].SpeedMps)
	}
}
