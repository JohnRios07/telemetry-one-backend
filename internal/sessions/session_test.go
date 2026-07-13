package sessions

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCreateRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		request CreateRequest
		wantErr error
	}{
		{
			name: "valid",
			request: CreateRequest{
				Source:        "flutter",
				Game:          "gt7",
				Platform:      "ps5",
				StartedUnixMs: 1720656000000,
			},
		},
		{name: "missing source", request: CreateRequest{Game: "gt7", Platform: "ps5", StartedUnixMs: 1}, wantErr: ErrMissingSource},
		{name: "missing game", request: CreateRequest{Source: "flutter", Platform: "ps5", StartedUnixMs: 1}, wantErr: ErrMissingGame},
		{name: "missing platform", request: CreateRequest{Source: "flutter", Game: "gt7", StartedUnixMs: 1}, wantErr: ErrMissingPlatform},
		{name: "invalid start", request: CreateRequest{Source: "flutter", Game: "gt7", Platform: "ps5"}, wantErr: ErrInvalidStartedAt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.request.Validate()
			if err != tt.wantErr {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestSessionStatusAndDuration(t *testing.T) {
	startedAt := time.UnixMilli(1720656000000).UTC()
	session := Session{ID: "session-1", StartedAt: startedAt}

	if session.Status() != StatusActive || session.DurationMs() != nil {
		t.Fatalf("expected active session without duration, got status=%s duration=%v", session.Status(), session.DurationMs())
	}

	endedAt := time.UnixMilli(1720656123456).UTC()
	session.EndedAt = &endedAt
	duration := session.DurationMs()
	if session.Status() != StatusFinished || duration == nil || *duration != 123456 {
		t.Fatalf("expected finished session duration 123456, got status=%s duration=%v", session.Status(), duration)
	}
}

func TestMemoryRepositoryLifecycle(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	startedAt := time.UnixMilli(1720656000000).UTC()

	created, err := repo.Create(ctx, Session{ID: "session-1", Source: "flutter", Game: "gt7", Platform: "ps5", StartedAt: startedAt})
	if err != nil {
		t.Fatalf("expected create success: %v", err)
	}
	if created.Status() != StatusActive {
		t.Fatalf("expected active created session, got %+v", created)
	}

	found, err := repo.FindByID(ctx, "session-1")
	if err != nil || found.ID != "session-1" {
		t.Fatalf("expected found session, got %+v err=%v", found, err)
	}

	endedAt := time.UnixMilli(1720656123456).UTC()
	finished, err := repo.End(ctx, "session-1", endedAt)
	if err != nil {
		t.Fatalf("expected finish success: %v", err)
	}
	if finished.Status() != StatusFinished || finished.EndedAt == nil || !finished.EndedAt.Equal(endedAt) {
		t.Fatalf("expected finished session, got %+v", finished)
	}

	if _, err := repo.End(ctx, "session-1", endedAt); err != ErrAlreadyFinished {
		t.Fatalf("expected ErrAlreadyFinished, got %v", err)
	}
	if _, err := repo.FindByID(ctx, "missing"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestNewIDUsesSessionPrefixedULIDShape(t *testing.T) {
	id, err := NewID()
	if err != nil {
		t.Fatalf("expected id generation success: %v", err)
	}

	if !strings.HasPrefix(id, "session_") || len(id) != len("session_")+26 {
		t.Fatalf("expected session-prefixed ULID, got %q", id)
	}
}
