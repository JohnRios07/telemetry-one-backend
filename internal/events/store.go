package events

import (
	"context"
	"sync"
)

const DefaultStoredEventsLimit = 1000

type Store struct {
	mu           sync.Mutex
	events       []EngineerEvent
	deduplicator *EventDeduplicator
	limit        int
}

func NewStore(limit int, dedupOptions DedupOptions) *Store {
	if limit <= 0 {
		limit = DefaultStoredEventsLimit
	}
	return &Store{
		deduplicator: NewEventDeduplicator(dedupOptions),
		limit:        limit,
	}
}

func (s *Store) Append(ctx context.Context, event EngineerEvent) (EngineerEvent, bool, error) {
	if err := ctx.Err(); err != nil {
		return EngineerEvent{}, false, err
	}
	if err := event.Validate(); err != nil {
		return EngineerEvent{}, false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.deduplicator == nil {
		s.deduplicator = NewEventDeduplicator(DedupOptions{})
	}
	accepted, _ := s.deduplicator.Filter([]EngineerEvent{event})
	if len(accepted) == 0 {
		return event, false, nil
	}

	s.events = append(s.events, event)
	if len(s.events) > s.limit {
		s.events = append([]EngineerEvent(nil), s.events[len(s.events)-s.limit:]...)
	}

	return event, true, nil
}

func (s *Store) List(ctx context.Context, query Query) ([]EngineerEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := query.Validate(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]EngineerEvent, 0, len(s.events))
	for _, event := range s.events {
		if event.SessionID != query.SessionID {
			continue
		}
		if query.LapNumber != nil && event.LapNumber != *query.LapNumber {
			continue
		}
		if query.CornerID != "" && catalogRefID(event.Corner) != query.CornerID {
			continue
		}
		if query.Type != "" && event.Type != query.Type {
			continue
		}
		result = append(result, event)
	}

	return result, nil
}
