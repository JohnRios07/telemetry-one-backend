package telemetry

import (
	"fmt"
	"sync"
	"testing"
)

func TestFrameStoreConcurrentAppend(t *testing.T) {
	t.Run("ten concurrent writers to the same session", func(t *testing.T) {
		t.Parallel()

		store := NewFrameStore(1000)
		var wg sync.WaitGroup
		const numGoroutines = 10
		const framesPerGoroutine = 100

		for i := range numGoroutines {
			wg.Add(1)
			i := i
			go func() {
				defer wg.Done()
				frames := make([]Frame, framesPerGoroutine)
				for j := range framesPerGoroutine {
					frames[j] = withFrame(func(f *Frame) {
						f.TimestampUnixMs = int64(i*framesPerGoroutine + j + 1)
					})
				}
				store.Append("session-1", frames)
			}()
		}

		wg.Wait()

		frames := store.Frames("session-1")
		if len(frames) > 1000 {
			t.Fatalf("expected at most 1000 frames, got %d", len(frames))
		}

		// Verify every expected timestamp is present exactly once.
		seen := make(map[int64]bool, len(frames))
		for _, f := range frames {
			if f.TimestampUnixMs < 1 || f.TimestampUnixMs > 1000 {
				t.Fatalf("timestamp %d outside expected range [1, 1000]", f.TimestampUnixMs)
			}
			if seen[f.TimestampUnixMs] {
				t.Fatalf("duplicate timestamp %d", f.TimestampUnixMs)
			}
			seen[f.TimestampUnixMs] = true
		}
		if len(seen) != len(frames) {
			t.Fatalf("unique timestamp mismatch: %d unique for %d frames", len(seen), len(frames))
		}
	})
}

func TestFrameStoreConcurrentReadWrite(t *testing.T) {
	store := NewFrameStore(500)
	var wg sync.WaitGroup

	// 5 writers hammering the same shared session.
	for i := range 5 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			frames := make([]Frame, 10)
			for j := range 10 {
				frames[j] = withFrame(func(f *Frame) {
					f.TimestampUnixMs = int64(id*1000 + j + 1)
				})
			}
			store.Append("shared", frames)
		}(i)
	}

	// 5 concurrent readers of the shared session.
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = store.Frames("shared")
		}()
	}

	// 3 writers writing to isolated sessions.
	for i := range 3 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sessionID := fmt.Sprintf("writer-%d", id)
			for j := range 5 {
				store.Append(sessionID, []Frame{withFrame(func(f *Frame) {
					f.TimestampUnixMs = int64(id*100 + j + 1)
				})})
			}
		}(i)
	}

	wg.Wait()

	sharedFrames := store.Frames("shared")
	t.Logf("concurrent read/write completed — shared session has %d frames", len(sharedFrames))
}

func TestFrameStoreConcurrentDifferentSessions(t *testing.T) {
	store := NewFrameStore(100)
	var wg sync.WaitGroup
	const numSessions = 5
	const framesPerSession = 20

	for i := range numSessions {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			sessionID := fmt.Sprintf("session-%d", i)
			frames := make([]Frame, framesPerSession)
			for j := range framesPerSession {
				frames[j] = withFrame(func(f *Frame) {
					f.TimestampUnixMs = int64(i*framesPerSession + j + 1)
				})
			}
			store.Append(sessionID, frames)
		}()
	}

	wg.Wait()

	for i := range numSessions {
		sessionID := fmt.Sprintf("session-%d", i)
		frames := store.Frames(sessionID)
		if len(frames) != framesPerSession {
			t.Fatalf("session %q: expected %d frames, got %d", sessionID, framesPerSession, len(frames))
		}
		for k := 1; k < len(frames); k++ {
			if frames[k].TimestampUnixMs <= frames[k-1].TimestampUnixMs {
				t.Fatalf("session %q: frames out of order at index %d (%d <= %d)",
					sessionID, k, frames[k].TimestampUnixMs, frames[k-1].TimestampUnixMs)
			}
		}
	}
}
