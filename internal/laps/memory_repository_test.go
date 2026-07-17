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
