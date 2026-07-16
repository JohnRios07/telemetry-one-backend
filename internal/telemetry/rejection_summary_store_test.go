package telemetry

import (
	"context"
	"testing"
)

func TestMemoryRejectionSummaryStoreAggregatesAndOrdersReasons(t *testing.T) {
	store := NewMemoryRejectionSummaryStore()
	ctx := context.Background()

	if err := store.Append(ctx, RejectionSummaryRecord{
		SessionID:      "session-1",
		RejectedFrames: 2,
		Summary: RejectionSummary{Reasons: []RejectionReasonCount{
			{Code: "invalid_speed", Count: 1},
			{Code: "invalid_throttle", Count: 1},
		}},
	}); err != nil {
		t.Fatalf("append first summary: %v", err)
	}
	if err := store.Append(ctx, RejectionSummaryRecord{
		SessionID:      "session-1",
		RejectedFrames: 2,
		Summary: RejectionSummary{Reasons: []RejectionReasonCount{
			{Code: "invalid_speed", Count: 1},
			{Code: "invalid_brake", Count: 1},
		}},
	}); err != nil {
		t.Fatalf("append second summary: %v", err)
	}

	aggregate, err := store.Summary(ctx, "session-1")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if aggregate.RejectedFrames != 4 {
		t.Fatalf("expected 4 rejected frames, got %d", aggregate.RejectedFrames)
	}
	want := []RejectionReasonCount{
		{Code: "invalid_speed", Count: 2},
		{Code: "invalid_brake", Count: 1},
		{Code: "invalid_throttle", Count: 1},
	}
	assertReasons(t, aggregate.Summary.Reasons, want)
}

func TestMemoryRejectionSummaryStoreReturnsDefensiveCopies(t *testing.T) {
	store := NewMemoryRejectionSummaryStore()
	ctx := context.Background()
	record := RejectionSummaryRecord{
		SessionID:      "session-1",
		RejectedFrames: 1,
		Summary:        RejectionSummary{Reasons: []RejectionReasonCount{{Code: "invalid_speed", Count: 1}}},
	}
	if err := store.Append(ctx, record); err != nil {
		t.Fatalf("append summary: %v", err)
	}
	record.Summary.Reasons[0].Count = 99

	aggregate, err := store.Summary(ctx, "session-1")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	aggregate.Summary.Reasons[0].Count = 42

	again, err := store.Summary(ctx, "session-1")
	if err != nil {
		t.Fatalf("summary again: %v", err)
	}
	assertReasons(t, again.Summary.Reasons, []RejectionReasonCount{{Code: "invalid_speed", Count: 1}})
}

func TestMemoryRejectionSummaryStoreAllRejectedKeepsZeroAcceptedRange(t *testing.T) {
	store := NewMemoryRejectionSummaryStore()
	record := RejectionSummaryRecord{
		SessionID:          "session-1",
		Status:             "rejected",
		ReceivedFrames:     1,
		AcceptedFrames:     0,
		RejectedFrames:     1,
		AcceptedFromUnixMs: 0,
		AcceptedToUnixMs:   0,
		Summary:            RejectionSummary{Reasons: []RejectionReasonCount{{Code: "invalid_throttle", Count: 1}}},
	}
	if err := store.Append(context.Background(), record); err != nil {
		t.Fatalf("append summary: %v", err)
	}

	store.mu.RLock()
	stored := store.records["session-1"][0]
	store.mu.RUnlock()
	if stored.AcceptedFromUnixMs != 0 || stored.AcceptedToUnixMs != 0 {
		t.Fatalf("expected all-rejected accepted range 0/0, got %d/%d", stored.AcceptedFromUnixMs, stored.AcceptedToUnixMs)
	}
}

func TestMemoryRejectionSummaryStoreSummariesIsSessionScoped(t *testing.T) {
	store := NewMemoryRejectionSummaryStore()
	ctx := context.Background()
	for _, record := range []RejectionSummaryRecord{
		{SessionID: "session-1", RejectedFrames: 1, Summary: RejectionSummary{Reasons: []RejectionReasonCount{{Code: "invalid_speed", Count: 1}}}},
		{SessionID: "session-2", RejectedFrames: 2, Summary: RejectionSummary{Reasons: []RejectionReasonCount{{Code: "invalid_brake", Count: 2}}}},
	} {
		if err := store.Append(ctx, record); err != nil {
			t.Fatalf("append summary: %v", err)
		}
	}

	aggregates, err := store.Summaries(ctx, []string{"session-1", "session-2", "session-3"})
	if err != nil {
		t.Fatalf("summaries: %v", err)
	}
	if aggregates["session-1"].RejectedFrames != 1 || aggregates["session-2"].RejectedFrames != 2 || aggregates["session-3"].RejectedFrames != 0 {
		t.Fatalf("unexpected scoped totals: %+v", aggregates)
	}
}

func assertReasons(t testing.TB, got, want []RejectionReasonCount) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %d reasons, got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("reason %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
