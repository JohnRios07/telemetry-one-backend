package telemetry

import "testing"

func TestFrameStoreAppendsAndRetrievesFramesInOrder(t *testing.T) {
	store := NewFrameStore(10)
	first := validFrame()
	second := withFrame(func(frame *Frame) { frame.TimestampUnixMs++ })

	store.Append("session-1", []Frame{first})
	store.Append("session-1", []Frame{second})

	frames := store.Frames("session-1")
	if len(frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(frames))
	}
	if frames[0].TimestampUnixMs != first.TimestampUnixMs || frames[1].TimestampUnixMs != second.TimestampUnixMs {
		t.Fatalf("frames were not retained in append order: %+v", frames)
	}
}

func TestFrameStoreIsolatesSessions(t *testing.T) {
	store := NewFrameStore(10)
	store.Append("session-1", []Frame{validFrame()})
	store.Append("session-2", []Frame{withFrame(func(frame *Frame) { frame.TimestampUnixMs += 10 })})

	if got := len(store.Frames("session-1")); got != 1 {
		t.Fatalf("expected session-1 to have 1 frame, got %d", got)
	}
	if got := len(store.Frames("session-2")); got != 1 {
		t.Fatalf("expected session-2 to have 1 frame, got %d", got)
	}
	if store.Frames("session-1")[0].TimestampUnixMs == store.Frames("session-2")[0].TimestampUnixMs {
		t.Fatalf("expected different retained frames per session")
	}
}

func TestFrameStoreEvictsOldestFramesPerSession(t *testing.T) {
	store := NewFrameStore(3)
	for i := 0; i < 5; i++ {
		offset := int64(i)
		store.Append("session-1", []Frame{withFrame(func(frame *Frame) { frame.TimestampUnixMs += offset })})
	}

	frames := store.Frames("session-1")
	if len(frames) != 3 {
		t.Fatalf("expected 3 retained frames, got %d", len(frames))
	}
	if frames[0].TimestampUnixMs != validFrame().TimestampUnixMs+2 || frames[2].TimestampUnixMs != validFrame().TimestampUnixMs+4 {
		t.Fatalf("expected oldest frames to be evicted, got %+v", frames)
	}
}

func TestFrameStoreReturnsDefensiveCopies(t *testing.T) {
	store := NewFrameStore(10)
	store.Append("session-1", []Frame{validFrame()})

	frames := store.Frames("session-1")
	frames[0].TimestampUnixMs = 1
	*frames[0].YawRadians = 99

	stored := store.Frames("session-1")
	if stored[0].TimestampUnixMs == 1 {
		t.Fatalf("expected stored frame value to be isolated from caller mutation")
	}
	if *stored[0].YawRadians == 99 {
		t.Fatalf("expected optional pointer fields to be isolated from caller mutation")
	}
}
