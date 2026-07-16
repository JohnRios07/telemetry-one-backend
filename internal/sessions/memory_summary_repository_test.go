package sessions

import (
	"context"
	"errors"
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

func TestMemorySummaryRepositoryAggregatesPersistedRejections(t *testing.T) {
	sessionRepo := NewMemoryRepository()
	frameStore := telemetry.NewFrameStore(100)
	eventStore := events.NewStore(100, events.DedupOptions{})
	rejectionStore := telemetry.NewMemoryRejectionSummaryStore()
	ctx := context.Background()
	startedAt := time.UnixMilli(1720656000000).UTC()

	sessionRepo.Create(ctx, Session{ID: "session-1", Source: "flutter", Game: "gt7", Platform: "ps5", StartedAt: startedAt})
	sessionRepo.Create(ctx, Session{ID: "session-2", Source: "flutter", Game: "gt7", Platform: "ps5", StartedAt: startedAt.Add(time.Minute)})
	for _, record := range []telemetry.RejectionSummaryRecord{
		{SessionID: "session-1", RejectedFrames: 1, Summary: telemetry.RejectionSummary{Reasons: []telemetry.RejectionReasonCount{{Code: "invalid_speed", Count: 1}}}},
		{SessionID: "session-1", RejectedFrames: 2, Summary: telemetry.RejectionSummary{Reasons: []telemetry.RejectionReasonCount{{Code: "invalid_throttle", Count: 2}}}},
		{SessionID: "session-2", RejectedFrames: 1, Summary: telemetry.RejectionSummary{Reasons: []telemetry.RejectionReasonCount{{Code: "invalid_brake", Count: 1}}}},
	} {
		if err := rejectionStore.Append(ctx, record); err != nil {
			t.Fatalf("append rejection summary: %v", err)
		}
	}
	repo := NewMemorySummaryRepository(sessionRepo, frameStore, eventStore, rejectionStore)

	list, err := repo.List(ctx, SummaryFilter{Limit: 10})
	if err != nil {
		t.Fatalf("list summaries: %v", err)
	}
	if len(list.Sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(list.Sessions))
	}
	if list.Sessions[0].ID != "session-2" || list.Sessions[0].RejectedFrames != 1 {
		t.Fatalf("expected session-2 scoped total 1, got %+v", list.Sessions[0])
	}
	if list.Sessions[1].ID != "session-1" || list.Sessions[1].RejectedFrames != 3 {
		t.Fatalf("expected session-1 scoped total 3, got %+v", list.Sessions[1])
	}

	summary, err := repo.Summary(ctx, "session-1")
	if err != nil {
		t.Fatalf("session summary: %v", err)
	}
	if summary.Session.RejectedFrames != 3 {
		t.Fatalf("expected detail rejectedFrames 3, got %d", summary.Session.RejectedFrames)
	}
	if summary.RejectionSummary == nil {
		t.Fatal("expected detail rejection summary")
	}
	want := []telemetry.RejectionReasonCount{{Code: "invalid_throttle", Count: 2}, {Code: "invalid_speed", Count: 1}}
	if len(summary.RejectionSummary.Reasons) != len(want) {
		t.Fatalf("expected %d reasons, got %+v", len(want), summary.RejectionSummary.Reasons)
	}
	for i := range want {
		if summary.RejectionSummary.Reasons[i] != want[i] {
			t.Fatalf("reason %d = %+v, want %+v", i, summary.RejectionSummary.Reasons[i], want[i])
		}
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
		Source: events.EventSource{Kind: events.SourceDeterministicRule, RuleID: "late_throttle.v1", RuleVersion: "v1"},
	}
}

type failingFrameStore struct {
	err error
	get int
}

func (s *failingFrameStore) Append(context.Context, string, []telemetry.Frame) error { return nil }

func (s *failingFrameStore) Frames(context.Context, string) ([]telemetry.Frame, error) {
	s.get++
	return nil, s.err
}

type scriptedFrameStore struct {
	snapshots [][]telemetry.Frame
	get       int
}

func (s *scriptedFrameStore) Append(context.Context, string, []telemetry.Frame) error { return nil }

func (s *scriptedFrameStore) Frames(_ context.Context, _ string) ([]telemetry.Frame, error) {
	idx := s.get
	s.get++
	if idx >= len(s.snapshots) {
		idx = len(s.snapshots) - 1
	}
	return s.snapshots[idx], nil
}

type failingEventRepo struct {
	err error
	get int
}

func (r *failingEventRepo) Append(context.Context, events.EngineerEvent) (events.EngineerEvent, bool, error) {
	return events.EngineerEvent{}, false, nil
}

func (r *failingEventRepo) List(context.Context, events.Query) ([]events.EngineerEvent, error) {
	r.get++
	return nil, r.err
}

func TestMemorySummaryRepositorySummaryNotFound(t *testing.T) {
	repo := NewMemorySummaryRepository(NewMemoryRepository(), telemetry.NewFrameStore(10), events.NewStore(10, events.DedupOptions{}))

	_, err := repo.Summary(context.Background(), "session-missing")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMemorySummaryRepositoryListFrameStoreError(t *testing.T) {
	sessionRepo := NewMemoryRepository()
	ctx := context.Background()
	_, _ = sessionRepo.Create(ctx, Session{ID: "session-1", Source: "flutter", Game: "gt7", Platform: "ps5", StartedAt: time.UnixMilli(1).UTC()})

	repo := NewMemorySummaryRepository(sessionRepo, &failingFrameStore{err: errors.New("frame store down")}, nil)

	_, err := repo.List(ctx, SummaryFilter{Limit: 10})
	if err == nil || err.Error() != "load frames for session session-1: frame store down" {
		t.Fatalf("expected frame store error, got %v", err)
	}
}

func TestMemorySummaryRepositorySummaryFrameStoreError(t *testing.T) {
	sessionRepo := NewMemoryRepository()
	ctx := context.Background()
	_, _ = sessionRepo.Create(ctx, Session{ID: "session-1", Source: "flutter", Game: "gt7", Platform: "ps5", StartedAt: time.UnixMilli(1).UTC()})

	repo := NewMemorySummaryRepository(sessionRepo, &failingFrameStore{err: errors.New("frame store down")}, nil)

	_, err := repo.Summary(ctx, "session-1")
	if err == nil || err.Error() != "load frames for session session-1: frame store down" {
		t.Fatalf("expected frame store error, got %v", err)
	}
}

func TestMemorySummaryRepositoryListEventRepoError(t *testing.T) {
	sessionRepo := NewMemoryRepository()
	frameStore := telemetry.NewFrameStore(10)
	ctx := context.Background()
	_, _ = sessionRepo.Create(ctx, Session{ID: "session-1", Source: "flutter", Game: "gt7", Platform: "ps5", StartedAt: time.UnixMilli(1).UTC()})
	if err := frameStore.Append(ctx, "session-1", []telemetry.Frame{{TimestampUnixMs: 1}}); err != nil {
		t.Fatalf("append frames: %v", err)
	}

	repo := NewMemorySummaryRepository(sessionRepo, frameStore, &failingEventRepo{err: errors.New("event repo down")})

	_, err := repo.List(ctx, SummaryFilter{Limit: 10})
	if err == nil || err.Error() != "load events for session session-1: event repo down" {
		t.Fatalf("expected event repo error, got %v", err)
	}
}

func TestMemorySummaryRepositorySummaryEventRepoError(t *testing.T) {
	sessionRepo := NewMemoryRepository()
	frameStore := telemetry.NewFrameStore(10)
	ctx := context.Background()
	_, _ = sessionRepo.Create(ctx, Session{ID: "session-1", Source: "flutter", Game: "gt7", Platform: "ps5", StartedAt: time.UnixMilli(1).UTC()})
	if err := frameStore.Append(ctx, "session-1", []telemetry.Frame{{TimestampUnixMs: 1}}); err != nil {
		t.Fatalf("append frames: %v", err)
	}

	repo := NewMemorySummaryRepository(sessionRepo, frameStore, &failingEventRepo{err: errors.New("event repo down")})

	_, err := repo.Summary(ctx, "session-1")
	if err == nil || err.Error() != "load events for session session-1: event repo down" {
		t.Fatalf("expected event repo error, got %v", err)
	}
}

func TestMemorySummaryRepositorySummaryUsesSingleFrameSnapshot(t *testing.T) {
	sessionRepo := NewMemoryRepository()
	frameStore := &scriptedFrameStore{snapshots: [][]telemetry.Frame{
		{
			{TimestampUnixMs: 100, LapNumber: 1},
			{TimestampUnixMs: 200, LapNumber: 1},
		},
		{
			{TimestampUnixMs: 999, LapNumber: 9},
		},
	}}
	ctx := context.Background()
	_, _ = sessionRepo.Create(ctx, Session{ID: "session-1", Source: "flutter", Game: "gt7", Platform: "ps5", StartedAt: time.UnixMilli(1).UTC()})

	repo := NewMemorySummaryRepository(sessionRepo, frameStore, nil)

	summary, err := repo.Summary(ctx, "session-1")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if frameStore.get != 1 {
		t.Fatalf("expected 1 frame-store call, got %d", frameStore.get)
	}
	if summary.Session.PersistedFrames != 2 || summary.PersistedFrames != 2 {
		t.Fatalf("expected consistent frame counts from one snapshot, got session=%d summary=%d", summary.Session.PersistedFrames, summary.PersistedFrames)
	}
	if summary.LapsDetected != 1 {
		t.Fatalf("expected 1 lap from first snapshot, got %d", summary.LapsDetected)
	}
	if summary.TimeRangeMs == nil || summary.TimeRangeMs.From != 100 || summary.TimeRangeMs.To != 200 {
		t.Fatalf("expected time range [100, 200], got %+v", summary.TimeRangeMs)
	}
}
