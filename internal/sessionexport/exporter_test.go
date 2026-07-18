package sessionexport

import (
	"context"
	"errors"
	"testing"
	"time"

	"telemetry-one-backend/internal/sessions"
	"telemetry-one-backend/internal/telemetry"
)

type fakeSessionReader struct {
	session sessions.Session
	err     error
}

func (r fakeSessionReader) FindByID(context.Context, string) (sessions.Session, error) {
	if r.err != nil {
		return sessions.Session{}, r.err
	}
	return r.session, nil
}

type fakeFrameReader struct {
	frames []telemetry.Frame
	err    error
}

func (r fakeFrameReader) Frames(context.Context, string) ([]telemetry.Frame, error) {
	if r.err != nil {
		return nil, r.err
	}
	return append([]telemetry.Frame(nil), r.frames...), nil
}

func TestExporterExportRejectsMissingSession(t *testing.T) {
	exporter := Exporter{SessionReader: fakeSessionReader{err: sessions.ErrNotFound}, FrameReader: fakeFrameReader{frames: []telemetry.Frame{validExportFrame(1)}}}

	_, err := exporter.Export(context.Background(), "missing")

	if !errors.Is(err, ErrMissingSession) {
		t.Fatalf("expected missing-session error, got %v", err)
	}
}

func TestExporterExportRejectsActiveSessionByDefault(t *testing.T) {
	exporter := Exporter{
		SessionReader: fakeSessionReader{session: activeSession()},
		FrameReader:   fakeFrameReader{frames: []telemetry.Frame{validExportFrame(1)}},
	}

	_, err := exporter.Export(context.Background(), "session-1")

	if !errors.Is(err, ErrActiveSessionBlocked) {
		t.Fatalf("expected active-session error, got %v", err)
	}
}

func TestExporterExportAllowsActiveSessionWhenOptedIn(t *testing.T) {
	frames := []telemetry.Frame{validExportFrame(1), validExportFrame(2)}
	exporter := Exporter{
		SessionReader:      fakeSessionReader{session: activeSession()},
		FrameReader:        fakeFrameReader{frames: frames},
		AllowActiveSession: true,
	}

	result, err := exporter.Export(context.Background(), "session-1")

	if err != nil {
		t.Fatalf("export active session: %v", err)
	}
	if result.Session.Status() != sessions.StatusActive {
		t.Fatalf("expected active session result, got %v", result.Session.Status())
	}
	if got := result.Request.Frames; len(got) != len(frames) || got[0].TimestampUnixMs != frames[0].TimestampUnixMs || got[1].TimestampUnixMs != frames[1].TimestampUnixMs {
		t.Fatalf("expected persisted order to be preserved, got %#v", got)
	}
}

func TestExporterExportRejectsEmptyFrames(t *testing.T) {
	exporter := Exporter{
		SessionReader: fakeSessionReader{session: finishedSession()},
		FrameReader:   fakeFrameReader{frames: nil},
	}

	_, err := exporter.Export(context.Background(), "session-1")

	if !errors.Is(err, ErrEmptyFrames) {
		t.Fatalf("expected empty-frames error, got %v", err)
	}
}

func TestExporterExportRejectsNonMonotonicTimestamps(t *testing.T) {
	exporter := Exporter{
		SessionReader: fakeSessionReader{session: finishedSession()},
		FrameReader: fakeFrameReader{frames: []telemetry.Frame{
			validExportFrame(2),
			validExportFrame(2),
		}},
	}

	_, err := exporter.Export(context.Background(), "session-1")

	if !errors.Is(err, ErrNonMonotonicTimestamps) {
		t.Fatalf("expected monotonicity error, got %v", err)
	}
}

func TestExporterExportPreservesFrameOrdering(t *testing.T) {
	frames := []telemetry.Frame{validExportFrame(1), validExportFrame(2), validExportFrame(3)}
	exporter := Exporter{SessionReader: fakeSessionReader{session: finishedSession()}, FrameReader: fakeFrameReader{frames: frames}}

	result, err := exporter.Export(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("export frames: %v", err)
	}

	for i, frame := range result.Request.Frames {
		if frame.TimestampUnixMs != frames[i].TimestampUnixMs {
			t.Fatalf("frame %d order changed: got %d want %d", i, frame.TimestampUnixMs, frames[i].TimestampUnixMs)
		}
	}
}

func TestExporterExportPreservesLongSessions(t *testing.T) {
	frames := make([]telemetry.Frame, 0, telemetry.MaxBatchFrames+1)
	for i := 0; i < telemetry.MaxBatchFrames+1; i++ {
		frames = append(frames, validExportFrame(int64(i+1)))
	}
	exporter := Exporter{SessionReader: fakeSessionReader{session: finishedSession()}, FrameReader: fakeFrameReader{frames: frames}}

	result, err := exporter.Export(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("export long session: %v", err)
	}
	if got := len(result.Request.Frames); got != len(frames) {
		t.Fatalf("expected all frames to be preserved, got %d want %d", got, len(frames))
	}
	if result.Request.Frames[len(result.Request.Frames)-1].TimestampUnixMs != frames[len(frames)-1].TimestampUnixMs {
		t.Fatalf("expected export to preserve final frame, got %+v", result.Request.Frames[len(result.Request.Frames)-1])
	}
}

func finishedSession() sessions.Session {
	ended := time.UnixMilli(1720656123456).UTC()
	return sessions.Session{ID: "session-1", StartedAt: time.UnixMilli(1720656000000).UTC(), EndedAt: &ended}
}

func activeSession() sessions.Session {
	return sessions.Session{ID: "session-1", StartedAt: time.UnixMilli(1720656000000).UTC()}
}

func validExportFrame(timestamp int64) telemetry.Frame {
	return telemetry.Frame{
		TimestampUnixMs: timestamp,
		SpeedMps:        58.33,
		RPM:             7100,
		Gear:            4,
		Throttle:        0.7,
		Brake:           0.2,
		Steering:        -0.12,
		FuelLiters:      38.4,
		PositionX:       123.4,
		PositionY:       5.6,
		PositionZ:       789.1,
		LapNumber:       1,
		CurrentLapMs:    81234,
		IsOnTrack:       true,
	}
}
