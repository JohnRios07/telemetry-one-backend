package sessionexport

import (
	"context"
	"errors"
	"fmt"

	"telemetry-one-backend/internal/sessions"
	"telemetry-one-backend/internal/telemetry"
)

var (
	ErrActiveSessionBlocked      = errors.New("active session export blocked")
	ErrEmptyFrames               = errors.New("no persisted frames found")
	ErrNonMonotonicTimestamps    = errors.New("exported timestamps must strictly increase")
	ErrMissingSession            = errors.New("session not found")
	ErrMissingExporterDependency = errors.New("exporter dependency is required")
)

type SessionReader interface {
	FindByID(ctx context.Context, id string) (sessions.Session, error)
}

type FrameReader interface {
	Frames(ctx context.Context, sessionID string) ([]telemetry.Frame, error)
}

type Exporter struct {
	SessionReader      SessionReader
	FrameReader        FrameReader
	AllowActiveSession bool
}

type Result struct {
	Request telemetry.IngestBatchRequest
	Session sessions.Session
}

func (e Exporter) Export(ctx context.Context, sessionID string) (Result, error) {
	if e.SessionReader == nil || e.FrameReader == nil {
		return Result{}, ErrMissingExporterDependency
	}

	session, err := e.SessionReader.FindByID(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sessions.ErrNotFound) {
			return Result{}, fmt.Errorf("%w: %s", ErrMissingSession, sessionID)
		}
		return Result{}, fmt.Errorf("load session %q: %w", sessionID, err)
	}

	if session.Status() == sessions.StatusActive && !e.AllowActiveSession {
		return Result{}, ErrActiveSessionBlocked
	}

	frames, err := e.FrameReader.Frames(ctx, session.ID)
	if err != nil {
		return Result{}, fmt.Errorf("load frames for %q: %w", session.ID, err)
	}
	if len(frames) == 0 {
		return Result{}, fmt.Errorf("%w: %s", ErrEmptyFrames, session.ID)
	}
	if err := validateTimestamps(frames); err != nil {
		return Result{}, err
	}

	request := telemetry.IngestBatchRequest{
		SessionID: session.ID,
		Frames:    append([]telemetry.Frame(nil), frames...),
	}

	return Result{Request: request, Session: session}, nil
}

func validateTimestamps(frames []telemetry.Frame) error {
	for i := 1; i < len(frames); i++ {
		if frames[i].TimestampUnixMs <= frames[i-1].TimestampUnixMs {
			return fmt.Errorf("%w at frame %d: %d <= %d", ErrNonMonotonicTimestamps, i, frames[i].TimestampUnixMs, frames[i-1].TimestampUnixMs)
		}
	}

	return nil
}
