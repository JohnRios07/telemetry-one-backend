package telemetry

import "sync"

type FrameStore struct {
	mu       sync.RWMutex
	capacity int
	frames   map[string][]Frame
}

func NewFrameStore(capacity int) *FrameStore {
	if capacity <= 0 {
		capacity = 1
	}

	return &FrameStore{
		capacity: capacity,
		frames:   make(map[string][]Frame),
	}
}

func (s *FrameStore) Append(sessionID string, frames []Frame) {
	if sessionID == "" || len(frames) == 0 {
		return
	}

	incoming := cloneFrames(frames)
	if len(incoming) >= s.capacity {
		incoming = incoming[len(incoming)-s.capacity:]
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	retained := append(s.frames[sessionID], incoming...)
	if len(retained) > s.capacity {
		retained = retained[len(retained)-s.capacity:]
	}
	s.frames[sessionID] = retained
}

func (s *FrameStore) Frames(sessionID string) []Frame {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return cloneFrames(s.frames[sessionID])
}

func cloneFrames(frames []Frame) []Frame {
	cloned := make([]Frame, len(frames))
	for index, frame := range frames {
		cloned[index] = cloneFrame(frame)
	}

	return cloned
}

func cloneFrame(frame Frame) Frame {
	frame.YawRadians = cloneFloat64(frame.YawRadians)
	frame.YawRate = cloneFloat64(frame.YawRate)
	frame.WheelSpeedFL = cloneFloat64(frame.WheelSpeedFL)
	frame.WheelSpeedFR = cloneFloat64(frame.WheelSpeedFR)
	frame.WheelSpeedRL = cloneFloat64(frame.WheelSpeedRL)
	frame.WheelSpeedRR = cloneFloat64(frame.WheelSpeedRR)
	frame.LastLapMs = cloneInt64(frame.LastLapMs)
	frame.BestLapMs = cloneInt64(frame.BestLapMs)

	return frame
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
