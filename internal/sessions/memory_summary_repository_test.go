package sessions

import (
	"context"
	"testing"
	"time"

	"telemetry-one-backend/internal/events"
	"telemetry-one-backend/internal/telemetry"
)

func TestMemorySummaryRepositoryListEmpty(t *testing.T) {
	repo := NewMemorySummaryRepository(NewMemoryRepository(), telemetry.NewFrameStore(10), events.NewStore(10, events.DedupOptions{}))

	resp, err := repo.List(context.Background(), SummaryFilter{Limit: 10})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(resp.Sessions))
	}
}

func TestMemorySummaryRepositoryListSeeded(t *testing.T) {
	sessionRepo := NewMemoryRepository()
	frameStore := telemetry.NewFrameStore(100)
	eventStore := events.NewStore(100, events.DedupOptions{})
	ctx := context.Background()
	startedAt := time.UnixMilli(1720656000000).UTC()

	s1, _ := sessionRepo.Create(ctx, Session{ID: "session-1", Source: "flutter", Game: "gt7", Platform: "ps5", DriverAlias: "alex", StartedAt: startedAt})
	s2, _ := sessionRepo.Create(ctx, Session{ID: "session-2", Source: "unity", Game: "gt7", Platform: "ps5", StartedAt: startedAt.Add(time.Hour)})

	if err := frameStore.Append(ctx, "session-1", []telemetry.Frame{{TimestampUnixMs: 1}, {TimestampUnixMs: 2}}); err != nil {
		t.Fatalf("append frames: %v", err)
	}

	repo := NewMemorySummaryRepository(sessionRepo, frameStore, eventStore)

	t.Run("default order", func(t *testing.T) {
		resp, err := repo.List(ctx, SummaryFilter{Limit: 10})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resp.Sessions) != 2 {
			t.Fatalf("expected 2 sessions, got %d", len(resp.Sessions))
		}
		if resp.Sessions[0].ID != "session-2" || resp.Sessions[1].ID != "session-1" {
			t.Fatalf("expected session-2 first, got %s then %s", resp.Sessions[0].ID, resp.Sessions[1].ID)
		}
		if resp.Sessions[1].PersistedFrames != 2 {
			t.Fatalf("expected session-1 to have 2 frames, got %d", resp.Sessions[1].PersistedFrames)
		}
		if resp.Sessions[1].Source != "flutter" || resp.Sessions[1].Game != "gt7" || resp.Sessions[1].Platform != "ps5" {
			t.Fatalf("unexpected metadata: %+v", resp.Sessions[1])
		}
		if resp.Sessions[1].DriverAlias != "alex" {
			t.Fatalf("expected driverAlias alex, got %q", resp.Sessions[1].DriverAlias)
		}
		_ = s1
		_ = s2
	})

	t.Run("limit", func(t *testing.T) {
		resp, err := repo.List(ctx, SummaryFilter{Limit: 1})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if len(resp.Sessions) != 1 {
			t.Fatalf("expected 1 session, got %d", len(resp.Sessions))
		}
		if resp.Sessions[0].ID != "session-2" {
			t.Fatalf("expected session-2, got %s", resp.Sessions[0].ID)
		}
	})
}

func TestMemorySummaryRepositorySummaryFound(t *testing.T) {
	sessionRepo := NewMemoryRepository()
	frameStore := telemetry.NewFrameStore(100)
	eventStore := events.NewStore(100, events.DedupOptions{})
	ctx := context.Background()
	startedAt := time.UnixMilli(1720656000000).UTC()

	sessionRepo.Create(ctx, Session{ID: "session-summary", Source: "flutter", Game: "gt7", Platform: "ps5", StartedAt: startedAt})

	if err := frameStore.Append(ctx, "session-summary", []telemetry.Frame{
		{TimestampUnixMs: 100, LapNumber: 1},
		{TimestampUnixMs: 200, LapNumber: 1},
		{TimestampUnixMs: 300, LapNumber: 2},
	}); err != nil {
		t.Fatalf("append frames: %v", err)
	}

	_, accepted, err := eventStore.Append(ctx, validSummaryEvent("event-1", "session-summary"))
	if err != nil || !accepted {
		t.Fatalf("expected event accepted, err=%v accepted=%v", err, accepted)
	}

	repo := NewMemorySummaryRepository(sessionRepo, frameStore, eventStore)

	summary, err := repo.Summary(ctx, "session-summary")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if summary.Session.ID != "session-summary" {
		t.Fatalf("expected session-summary, got %s", summary.Session.ID)
	}
	if summary.PersistedFrames != 3 {
		t.Fatalf("expected 3 frames, got %d", summary.PersistedFrames)
	}
	if summary.LapsDetected != 2 {
		t.Fatalf("expected 2 laps, got %d", summary.LapsDetected)
	}
	if summary.FrameBatches != 0 {
		t.Fatalf("expected 0 frame batches in memory mode, got %d", summary.FrameBatches)
	}
	if summary.TimeRangeMs == nil || summary.TimeRangeMs.From != 100 || summary.TimeRangeMs.To != 300 {
		t.Fatalf("expected time range [100, 300], got %+v", summary.TimeRangeMs)
	}
	if summary.EngineerEventCount != 1 {
		t.Fatalf("expected 1 engineer event, got %d", summary.EngineerEventCount)
	}
	if summary.AIAuditLogCount != 0 {
		t.Fatalf("expected 0 ai audit logs, got %d", summary.AIAuditLogCount)
	}
}

func validSummaryEvent(eventID, sessionID string) events.EngineerEvent {
	return events.EngineerEvent{
		EventID:         eventID,
		SessionID:       sessionID,
		Version:         "telemetry-one.engineer-event.v1",
		Type:            events.TypeLateThrottle,
		Severity:        events.SeverityMedium,
		Confidence:      0.8,
		TimestampUnixMs: 1720656000000,
		LapNumber:       1,
		Metrics: []events.MetricEvidence{
			{Name: "throttleReapplicationDeltaMs", Value: 320, Unit: "ms", Status: events.MetricStatusAvailable, Role: events.MetricRoleDelta},
		},
		Source:          events.EventSource{Kind: events.SourceDeterministicRule, RuleID: "late_throttle.v1", RuleVersion: "v1"},
	}
}

func TestMemorySummaryRepositorySummaryNotFound(t *testing.T) {
	repo := NewMemorySummaryRepository(NewMemoryRepository(), telemetry.NewFrameStore(10), events.NewStore(10, events.DedupOptions{}))

	_, err := repo.Summary(context.Background(), "session-missing")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
