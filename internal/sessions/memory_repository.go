package sessions

import (
	"context"
	"sync"
	"time"
)

type MemoryRepository struct {
	mu       sync.RWMutex
	sessions map[string]Session
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{sessions: make(map[string]Session)}
}

func (r *MemoryRepository) Create(_ context.Context, session Session) (Session, error) {
	if session.ID == "" {
		return Session{}, ErrMissingID
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.sessions[session.ID]; ok {
		return Session{}, ErrAlreadyExists
	}
	r.sessions[session.ID] = cloneSession(session)
	return cloneSession(session), nil
}

func (r *MemoryRepository) FindByID(_ context.Context, id string) (Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	session, ok := r.sessions[id]
	if !ok {
		return Session{}, ErrNotFound
	}

	return cloneSession(session), nil
}

func (r *MemoryRepository) End(ctx context.Context, id string, endedAt time.Time) (Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	session, ok := r.sessions[id]
	if !ok {
		return Session{}, ErrNotFound
	}
	if session.EndedAt != nil {
		return Session{}, ErrAlreadyFinished
	}
	if endedAt.Before(session.StartedAt) {
		return Session{}, ErrInvalidEndedAt
	}

	endedAt = endedAt.UTC()
	session.EndedAt = &endedAt
	r.sessions[id] = cloneSession(session)
	_ = ctx

	return cloneSession(session), nil
}

func (r *MemoryRepository) Update(_ context.Context, session Session) (Session, error) {
	if session.ID == "" {
		return Session{}, ErrMissingID
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.sessions[session.ID]; !ok {
		return Session{}, ErrNotFound
	}
	r.sessions[session.ID] = cloneSession(session)

	return cloneSession(session), nil
}

func (r *MemoryRepository) SetDetectedTrackLayout(_ context.Context, id string, trackID, layoutID string) (Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	session, ok := r.sessions[id]
	if !ok {
		return Session{}, ErrNotFound
	}

	session.DetectedTrackID = trackID
	session.DetectedLayoutID = layoutID
	r.sessions[id] = cloneSession(session)

	return cloneSession(session), nil
}

func (r *MemoryRepository) List(_ context.Context) ([]Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Session, 0, len(r.sessions))
	for _, session := range r.sessions {
		result = append(result, cloneSession(session))
	}

	return result, nil
}

func cloneSession(session Session) Session {
	if session.EndedAt == nil {
		return session
	}
	endedAt := *session.EndedAt
	session.EndedAt = &endedAt

	return session
}
