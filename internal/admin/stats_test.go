package admin

import (
	"context"
	"fmt"
	"testing"
	"time"

	"telemetry-one-backend/internal/sessions"
	"telemetry-one-backend/internal/telemetry"
)

func TestMemoryStatsRepo_Empty(t *testing.T) {
	repo := NewMemoryStatsRepo(sessions.NewMemoryRepository(), telemetry.NewFrameStore(100))
	resp, err := repo.Stats(context.Background(), DefaultLimit, DefaultDays)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp.Mode != ModeMemory {
		t.Fatalf("expected mode memory, got %q", resp.Mode)
	}
	if resp.Totals.Sessions != 0 || resp.Totals.ActiveSessions != 0 || resp.Totals.FinishedSessions != 0 {
		t.Fatalf("expected zero totals, got %+v", resp.Totals)
	}
	if resp.Totals.FrameBatches != nil {
		t.Fatal("expected nil frame batches in memory mode")
	}
	if resp.Totals.EngineerEvents != nil {
		t.Fatal("expected nil engineer events in memory mode")
	}
	if resp.Totals.AIAuditLogs != nil {
		t.Fatal("expected nil ai audit logs in memory mode")
	}
	if resp.Totals.PersistedFrames == nil || *resp.Totals.PersistedFrames != 0 {
		t.Fatal("expected persisted frames 0 in memory mode")
	}
	if len(resp.RecentSessions) != 0 {
		t.Fatalf("expected empty recent sessions, got %d", len(resp.RecentSessions))
	}
	if len(resp.Daily) != 0 {
		t.Fatalf("expected empty daily stats, got %d", len(resp.Daily))
	}
}

func TestMemoryStatsRepo_WithData(t *testing.T) {
	ctx := context.Background()
	sessionRepo := sessions.NewMemoryRepository()
	frameStore := telemetry.NewFrameStore(1000)

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, time.UTC)
	startedAt := today

	sessionRepo.Create(ctx, sessions.Session{
		ID: "session-1", Source: "test", Game: "gt7",
		Platform: "ps5", StartedAt: startedAt,
	})

	s2, err := sessionRepo.Create(ctx, sessions.Session{
		ID: "session-2", Source: "test", Game: "gt7",
		Platform: "ps5", StartedAt: startedAt.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("create session 2: %v", err)
	}

	sessionRepo.End(ctx, s2.ID, startedAt.Add(2*time.Minute))

	if err := frameStore.Append(ctx, "session-1", []telemetry.Frame{{TimestampUnixMs: 1}}); err != nil {
		t.Fatalf("append frame: %v", err)
	}
	if err := frameStore.Append(ctx, "session-2", []telemetry.Frame{{TimestampUnixMs: 2}, {TimestampUnixMs: 3}}); err != nil {
		t.Fatalf("append frames: %v", err)
	}

	repo := NewMemoryStatsRepo(sessionRepo, frameStore)
	resp, err := repo.Stats(ctx, 10, 30)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if resp.Totals.Sessions != 2 {
		t.Fatalf("expected 2 sessions, got %d", resp.Totals.Sessions)
	}
	if resp.Totals.ActiveSessions != 1 {
		t.Fatalf("expected 1 active session, got %d", resp.Totals.ActiveSessions)
	}
	if resp.Totals.FinishedSessions != 1 {
		t.Fatalf("expected 1 finished session, got %d", resp.Totals.FinishedSessions)
	}
	if resp.Totals.PersistedFrames == nil || *resp.Totals.PersistedFrames != 3 {
		t.Fatalf("expected 3 persisted frames, got %v", resp.Totals.PersistedFrames)
	}

	if len(resp.RecentSessions) != 2 {
		t.Fatalf("expected 2 recent sessions, got %d", len(resp.RecentSessions))
	}
	if resp.RecentSessions[0].ID != "session-2" || resp.RecentSessions[1].ID != "session-1" {
		t.Fatalf("expected sessions ordered by started_at desc")
	}
	if resp.RecentSessions[0].Status != "finished" || resp.RecentSessions[1].Status != "active" {
		t.Fatalf("expected correct statuses, got %q and %q", resp.RecentSessions[0].Status, resp.RecentSessions[1].Status)
	}
	if resp.RecentSessions[0].PersistedFrames != 2 || resp.RecentSessions[1].PersistedFrames != 1 {
		t.Fatalf("expected frame counts per session")
	}

	if len(resp.Daily) != 1 {
		t.Fatalf("expected 1 daily entry, got %d", len(resp.Daily))
	}
	if resp.Daily[0].Sessions != 2 || resp.Daily[0].PersistedFrames != 3 {
		t.Fatalf("expected 2 sessions, 3 frames for the day, got %+v", resp.Daily[0])
	}
}

func TestRecentSession_DurationMs(t *testing.T) {
	ctx := context.Background()
	sessionRepo := sessions.NewMemoryRepository()
	frameStore := telemetry.NewFrameStore(100)

	startedAt := time.Now().UTC().Add(-time.Hour)
	sessionRepo.Create(ctx, sessions.Session{
		ID: "s1", Source: "test", Game: "gt7",
		Platform: "ps5", StartedAt: startedAt,
	})
	sessionRepo.End(ctx, "s1", startedAt.Add(123456*time.Millisecond))

	repo := NewMemoryStatsRepo(sessionRepo, frameStore)
	resp, err := repo.Stats(ctx, 10, 30)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.RecentSessions) != 1 {
		t.Fatalf("expected 1 recent session, got %d", len(resp.RecentSessions))
	}
	rs := resp.RecentSessions[0]
	if rs.DurationMs == nil || *rs.DurationMs != 123456 {
		t.Fatalf("expected duration 123456, got %v", rs.DurationMs)
	}
	if rs.EndedAt == nil {
		t.Fatal("expected endedAt to be non-nil")
	}
	if rs.Status != "finished" {
		t.Fatalf("expected status finished, got %q", rs.Status)
	}
}

func TestClamp(t *testing.T) {
	if got := clamp(5, 1, 100); got != 5 {
		t.Fatalf("expected 5, got %d", got)
	}
	if got := clamp(0, 1, 100); got != 1 {
		t.Fatalf("expected 1, got %d", got)
	}
	if got := clamp(200, 1, 100); got != 100 {
		t.Fatalf("expected 100, got %d", got)
	}
}

func TestDefaults(t *testing.T) {
	if DefaultLimit != 10 {
		t.Fatalf("expected default limit 10, got %d", DefaultLimit)
	}
	if DefaultDays != 7 {
		t.Fatalf("expected default days 7, got %d", DefaultDays)
	}
	if MinLimit != 1 || MaxLimit != 100 {
		t.Fatalf("expected limit range 1..100")
	}
	if MinDays != 1 || MaxDays != 90 {
		t.Fatalf("expected days range 1..90")
	}
	if ModeMemory != "memory" || ModePostgres != "postgres" {
		t.Fatalf("unexpected mode values")
	}
}

func TestMemoryStatsRepo_LimitRecentSessions(t *testing.T) {
	ctx := context.Background()
	sessionRepo := sessions.NewMemoryRepository()
	frameStore := telemetry.NewFrameStore(100)

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("session-%d", i+1)
		sessionRepo.Create(ctx, sessions.Session{
			ID: id, Source: "test", Game: "gt7",
			Platform: "ps5",
			StartedAt: now.Add(time.Duration(i) * time.Second),
		})
	}

	repo := NewMemoryStatsRepo(sessionRepo, frameStore)

	resp3, err := repo.Stats(ctx, 3, 30)
	if err != nil {
		t.Fatalf("expected no error: %v", err)
	}
	if len(resp3.RecentSessions) != 3 {
		t.Fatalf("expected 3 recent sessions, got %d", len(resp3.RecentSessions))
	}
	if resp3.RecentSessions[0].ID != "session-5" || resp3.RecentSessions[2].ID != "session-3" {
		t.Fatalf("unexpected order with limit 3")
	}

	resp10, err := repo.Stats(ctx, 10, 30)
	if err != nil {
		t.Fatalf("expected no error: %v", err)
	}
	if len(resp10.RecentSessions) != 5 {
		t.Fatalf("expected all 5 sessions when limit exceeds count, got %d", len(resp10.RecentSessions))
	}
}
