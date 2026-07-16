package sessions

import (
	"context"
	"os"
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

func TestMemoryRepositoryCreateRejectsDuplicateID(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	startedAt := time.UnixMilli(1720656000000).UTC()
	original := Session{ID: "session-1", Source: "flutter", Game: "gt7", Platform: "ps5", StartedAt: startedAt}

	if _, err := repo.Create(ctx, original); err != nil {
		t.Fatalf("expected initial create success: %v", err)
	}
	if _, err := repo.Create(ctx, Session{ID: "session-1", Source: "other", Game: "gt7", Platform: "ps5", StartedAt: startedAt}); err != ErrAlreadyExists {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}

	found, err := repo.FindByID(ctx, "session-1")
	if err != nil {
		t.Fatalf("expected original session to remain stored: %v", err)
	}
	if found.Source != original.Source {
		t.Fatalf("expected duplicate create not to overwrite original, got source %q", found.Source)
	}
}

func TestPostgresRepositoryEndUsesAtomicActiveSessionUpdate(t *testing.T) {
	content, err := os.ReadFile("postgres_repository.go")
	if err != nil {
		t.Fatalf("read postgres repository: %v", err)
	}

	source := string(content)
	if !strings.Contains(source, "WHERE id = $1 AND ended_at IS NULL") {
		t.Fatal("expected PostgresRepository.End to update only active sessions atomically")
	}
	if strings.Contains(source, "func (r *PostgresRepository) End(ctx context.Context, id string, endedAt time.Time) (Session, error) {\n\tcurrent, err := r.FindByID") {
		t.Fatal("expected PostgresRepository.End not to pre-read session state before updating")
	}
}

func TestNewDTOIncludesDetectedFieldsWhenSet(t *testing.T) {
	s := Session{
		ID:               "session-detect",
		Source:           "test",
		Game:             "gt7",
		Platform:         "ps5",
		StartedAt:        time.UnixMilli(1720656000000).UTC(),
		DetectedTrackID:  "gt7_watkins_glen_international",
		DetectedLayoutID: "gt7_layout_1240",
	}
	dto := NewDTO(s, 0, 0)
	if dto.DetectedTrackID == nil || *dto.DetectedTrackID != "gt7_watkins_glen_international" {
		t.Fatalf("expected detectedTrackId, got %+v", dto.DetectedTrackID)
	}
	if dto.DetectedLayoutID == nil || *dto.DetectedLayoutID != "gt7_layout_1240" {
		t.Fatalf("expected detectedLayoutId, got %+v", dto.DetectedLayoutID)
	}
}

func TestNewDTOOmitsDetectedFieldsWhenEmpty(t *testing.T) {
	s := Session{
		ID: "session-no-detect", Source: "test", Game: "gt7",
		Platform: "ps5", StartedAt: time.UnixMilli(1720656000000).UTC(),
	}
	dto := NewDTO(s, 0, 0)
	if dto.DetectedTrackID != nil {
		t.Fatalf("expected nil detectedTrackId when empty, got %v", *dto.DetectedTrackID)
	}
	if dto.DetectedLayoutID != nil {
		t.Fatalf("expected nil detectedLayoutId when empty, got %v", *dto.DetectedLayoutID)
	}
}

func TestMemoryRepositoryUpdatePreservesDetectedFields(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	startedAt := time.UnixMilli(1720656000000).UTC()

	created, err := repo.Create(ctx, Session{
		ID: "session-detect-update", Source: "test", Game: "gt7",
		Platform: "ps5", StartedAt: startedAt,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	created.DetectedTrackID = "gt7_watkins_glen_international"
	created.DetectedLayoutID = "gt7_layout_1240"
	updated, err := repo.Update(ctx, created)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.DetectedTrackID != "gt7_watkins_glen_international" {
		t.Fatalf("expected detectedTrackId preserved, got %q", updated.DetectedTrackID)
	}
	if updated.DetectedLayoutID != "gt7_layout_1240" {
		t.Fatalf("expected detectedLayoutId preserved, got %q", updated.DetectedLayoutID)
	}

	found, err := repo.FindByID(ctx, "session-detect-update")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found.DetectedTrackID != "gt7_watkins_glen_international" {
		t.Fatalf("expected detectedTrackId persisted, got %q", found.DetectedTrackID)
	}
	if found.DetectedLayoutID != "gt7_layout_1240" {
		t.Fatalf("expected detectedLayoutId persisted, got %q", found.DetectedLayoutID)
	}
}

func TestMemoryRepositorySetDetectedTrackLayoutPreservesEndedAt(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	startedAt := time.UnixMilli(1720656000000).UTC()

	_, err := repo.Create(ctx, Session{
		ID: "session-set-detect", Source: "test", Game: "gt7",
		Platform: "ps5", StartedAt: startedAt,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	endedAt := time.UnixMilli(1720656123456).UTC()
	finished, err := repo.End(ctx, "session-set-detect", endedAt)
	if err != nil {
		t.Fatalf("end: %v", err)
	}
	if finished.EndedAt == nil || !finished.EndedAt.Equal(endedAt) {
		t.Fatalf("expected endedAt set after End, got %v", finished.EndedAt)
	}

	updated, err := repo.SetDetectedTrackLayout(ctx, "session-set-detect", "gt7_watkins_glen_international", "gt7_layout_1240")
	if err != nil {
		t.Fatalf("SetDetectedTrackLayout: %v", err)
	}
	if updated.EndedAt == nil || !updated.EndedAt.Equal(endedAt) {
		t.Fatal("SetDetectedTrackLayout must not clear EndedAt")
	}
	if updated.DetectedTrackID != "gt7_watkins_glen_international" {
		t.Fatalf("expected detectedTrackId set, got %q", updated.DetectedTrackID)
	}
	if updated.DetectedLayoutID != "gt7_layout_1240" {
		t.Fatalf("expected detectedLayoutId set, got %q", updated.DetectedLayoutID)
	}

	found, err := repo.FindByID(ctx, "session-set-detect")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found.EndedAt == nil || !found.EndedAt.Equal(endedAt) {
		t.Fatal("EndedAt must remain set after SetDetectedTrackLayout in store")
	}
}

func TestMemoryRepositorySetDetectedTrackLayoutIdempotent(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	startedAt := time.UnixMilli(1720656000000).UTC()

	_, err := repo.Create(ctx, Session{
		ID: "session-detect-idempotent", Source: "test", Game: "gt7",
		Platform: "ps5", StartedAt: startedAt,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	first, err := repo.SetDetectedTrackLayout(ctx, "session-detect-idempotent", "gt7_watkins_glen_international", "gt7_layout_1240")
	if err != nil {
		t.Fatalf("first SetDetectedTrackLayout: %v", err)
	}
	if first.DetectedTrackID != "gt7_watkins_glen_international" {
		t.Fatalf("expected detectedTrackId after first call, got %q", first.DetectedTrackID)
	}

	second, err := repo.SetDetectedTrackLayout(ctx, "session-detect-idempotent", "gt7_watkins_glen_international", "gt7_layout_1240")
	if err != nil {
		t.Fatalf("second SetDetectedTrackLayout: %v", err)
	}
	if second.DetectedTrackID != "gt7_watkins_glen_international" {
		t.Fatalf("expected detectedTrackId preserved after second call, got %q", second.DetectedTrackID)
	}
	if second.DetectedLayoutID != "gt7_layout_1240" {
		t.Fatalf("expected detectedLayoutId preserved after second call, got %q", second.DetectedLayoutID)
	}
}

func TestPostgresRepositorySetDetectedTrackLayoutIsPartialUpdate(t *testing.T) {
	content, err := os.ReadFile("postgres_repository.go")
	if err != nil {
		t.Fatalf("read postgres repository: %v", err)
	}

	source := string(content)
	if !strings.Contains(source, "func (r *PostgresRepository) SetDetectedTrackLayout") {
		t.Fatal("expected SetDetectedTrackLayout method on PostgresRepository")
	}

	// Locate the method body by finding the UPDATE statement within it
	methodStart := strings.Index(source, "func (r *PostgresRepository) SetDetectedTrackLayout")
	methodEnd := strings.Index(source[methodStart:], "\n}\n") + methodStart + 3
	methodBody := source[methodStart:methodEnd]

	if !strings.Contains(methodBody, "SET detected_track_id = $2, detected_layout_id = $3, updated_at = now()") {
		t.Fatal("expected SetDetectedTrackLayout to SET only detected_track_id, detected_layout_id, updated_at")
	}
	if strings.Contains(methodBody, "SET source = $2") {
		t.Fatal("expected SetDetectedTrackLayout NOT to SET source or other full-row columns")
	}
	if !strings.Contains(methodBody, "WHERE id = $1 AND detected_track_id IS NULL AND detected_layout_id IS NULL") {
		t.Fatal("expected SetDetectedTrackLayout to use idempotent WHERE clause")
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
