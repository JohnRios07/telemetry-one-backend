package telemetry

import (
	"context"
	"testing"
)

func mustAppend(t testing.TB, store Store, sessionID string, frames []Frame) {
	t.Helper()
	if err := store.Append(context.Background(), sessionID, frames); err != nil {
		t.Fatalf("append frames: %v", err)
	}
}

func mustFrames(t testing.TB, store Store, sessionID string) []Frame {
	t.Helper()
	frames, err := store.Frames(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("get frames: %v", err)
	}
	return frames
}

func TestFrameStoreAppendsAndRetrievesFramesInOrder(t *testing.T) {
	store := NewFrameStore(10)
	first := validFrame()
	second := withFrame(func(frame *Frame) { frame.TimestampUnixMs++ })

	mustAppend(t, store, "session-1", []Frame{first})
	mustAppend(t, store, "session-1", []Frame{second})

	frames := mustFrames(t, store, "session-1")
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(frames))
	}
	if frames[0].TimestampUnixMs != first.TimestampUnixMs || frames[1].TimestampUnixMs != second.TimestampUnixMs {
		t.Fatalf("frames were not retained in append order: %+v", frames)
	}
}

func TestFrameStoreIsolatesSessions(t *testing.T) {
	store := NewFrameStore(10)
	mustAppend(t, store, "session-1", []Frame{validFrame()})
	mustAppend(t, store, "session-2", []Frame{withFrame(func(frame *Frame) { frame.TimestampUnixMs += 10 })})

	if got := len(mustFrames(t, store, "session-1")); got != 1 {
		t.Fatalf("expected session-1 to have 1 frame, got %d", got)
	}
	if got := len(mustFrames(t, store, "session-2")); got != 1 {
		t.Fatalf("expected session-2 to have 1 frame, got %d", got)
	}
	if mustFrames(t, store, "session-1")[0].TimestampUnixMs == mustFrames(t, store, "session-2")[0].TimestampUnixMs {
		t.Fatalf("expected different retained frames per session")
	}
}

func TestFrameStoreEvictsOldestFramesPerSession(t *testing.T) {
	store := NewFrameStore(3)
	for i := 0; i < 5; i++ {
		offset := int64(i)
		mustAppend(t, store, "session-1", []Frame{withFrame(func(frame *Frame) { frame.TimestampUnixMs += offset })})
	}

	frames := mustFrames(t, store, "session-1")
	if len(frames) != 3 {
		t.Fatalf("expected 3 retained frames, got %d", len(frames))
	}
	if frames[0].TimestampUnixMs != validFrame().TimestampUnixMs+2 || frames[2].TimestampUnixMs != validFrame().TimestampUnixMs+4 {
		t.Fatalf("expected oldest frames to be evicted, got %+v", frames)
	}
}

func TestFrameStoreReturnsDefensiveCopies(t *testing.T) {
	store := NewFrameStore(10)
	mustAppend(t, store, "session-1", []Frame{validFrame()})

	frames := mustFrames(t, store, "session-1")
	frames[0].TimestampUnixMs = 1
	*frames[0].YawRadians = 99

	stored := mustFrames(t, store, "session-1")
	if stored[0].TimestampUnixMs == 1 {
		t.Fatalf("expected stored frame value to be isolated from caller mutation")
	}
	if *stored[0].YawRadians == 99 {
		t.Fatalf("expected optional pointer fields to be isolated from caller mutation")
	}
}
