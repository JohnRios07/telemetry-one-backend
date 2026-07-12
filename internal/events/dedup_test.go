package events

import (
	"strings"
	"testing"
)

func TestEventDeduplicatorSuppressesDuplicateInsideCooldown(t *testing.T) {
	deduplicator := NewEventDeduplicator(DedupOptions{CooldownWindowMs: 1000})
	first := dedupEvent(func(event *EngineerEvent) {
		event.EventID = "event-1"
		event.TimestampUnixMs = 10000
	})
	duplicate := dedupEvent(func(event *EngineerEvent) {
		event.EventID = "event-2"
		event.TimestampUnixMs = 10500
	})

	accepted, decisions := deduplicator.Filter([]EngineerEvent{first, duplicate})

	if len(accepted) != 1 || accepted[0].EventID != "event-1" {
		t.Fatalf("expected only first event accepted, got %+v", accepted)
	}
	assertDedupDecision(t, decisions[1], DedupDecisionSuppressed, DedupReasonDuplicateCooldown, "event-1", 10000)
	if decisions[0].Key != decisions[1].Key {
		t.Fatalf("expected volatile event ID/timestamp to be excluded from key, got %q and %q", decisions[0].Key, decisions[1].Key)
	}
}

func TestEventDeduplicatorAcceptsSameTypeDifferentCorner(t *testing.T) {
	deduplicator := NewEventDeduplicator(DedupOptions{CooldownWindowMs: 1000})
	first := dedupEvent(nil)
	otherCorner := dedupEvent(func(event *EngineerEvent) {
		event.EventID = "event-other-corner"
		event.TimestampUnixMs = 10200
		event.Corner = &CatalogRef{ID: stringPtr("corner-2"), Name: stringPtr("Catalog Corner 2"), DisplayStrategy: DisplayStrategyCatalogName}
	})

	accepted, decisions := deduplicator.Filter([]EngineerEvent{first, otherCorner})

	if len(accepted) != 2 {
		t.Fatalf("expected different corner to be accepted, got %+v", accepted)
	}
	assertDedupDecision(t, decisions[1], DedupDecisionAccepted, DedupReasonDifferentContext, "", 0)
}

func TestEventDeduplicatorAcceptsSameCornerDifferentLap(t *testing.T) {
	deduplicator := NewEventDeduplicator(DedupOptions{CooldownWindowMs: 1000})
	first := dedupEvent(nil)
	nextLap := dedupEvent(func(event *EngineerEvent) {
		event.EventID = "event-next-lap"
		event.LapNumber = 3
		event.TimestampUnixMs = 10200
	})

	accepted, decisions := deduplicator.Filter([]EngineerEvent{first, nextLap})

	if len(accepted) != 2 {
		t.Fatalf("expected different lap to be accepted, got %+v", accepted)
	}
	assertDedupDecision(t, decisions[1], DedupDecisionAccepted, DedupReasonDifferentContext, "", 0)
}

func TestEventDeduplicatorAcceptsAfterCooldownExpiry(t *testing.T) {
	deduplicator := NewEventDeduplicator(DedupOptions{CooldownWindowMs: 1000})
	first := dedupEvent(func(event *EngineerEvent) {
		event.EventID = "event-1"
		event.TimestampUnixMs = 10000
	})
	afterCooldown := dedupEvent(func(event *EngineerEvent) {
		event.EventID = "event-after-cooldown"
		event.TimestampUnixMs = 11000
	})

	accepted, decisions := deduplicator.Filter([]EngineerEvent{first, afterCooldown})

	if len(accepted) != 2 {
		t.Fatalf("expected event at cooldown boundary to be accepted, got %+v", accepted)
	}
	assertDedupDecision(t, decisions[1], DedupDecisionAccepted, DedupReasonCooldownExpired, "event-1", 10000)
}

func TestEventDeduplicatorAcceptsDifferentEventType(t *testing.T) {
	deduplicator := NewEventDeduplicator(DedupOptions{CooldownWindowMs: 1000})
	first := dedupEvent(nil)
	differentType := dedupEvent(func(event *EngineerEvent) {
		event.EventID = "event-different-type"
		event.Type = TypeLowExitSpeed
		event.Source.RuleID = "low_exit_speed.v1"
		event.TimestampUnixMs = 10200
	})

	accepted, decisions := deduplicator.Filter([]EngineerEvent{first, differentType})

	if len(accepted) != 2 {
		t.Fatalf("expected different event type to be accepted, got %+v", accepted)
	}
	assertDedupDecision(t, decisions[1], DedupDecisionAccepted, DedupReasonDifferentContext, "", 0)
}

func TestEventDeduplicatorHandlesNilRefsSafely(t *testing.T) {
	deduplicator := NewEventDeduplicator(DedupOptions{CooldownWindowMs: 1000})
	first := dedupEvent(func(event *EngineerEvent) {
		event.Track = nil
		event.Layout = nil
		event.Corner = nil
		event.TimestampUnixMs = 10000
	})
	duplicate := dedupEvent(func(event *EngineerEvent) {
		event.EventID = "event-nil-refs-duplicate"
		event.Track = nil
		event.Layout = nil
		event.Corner = nil
		event.TimestampUnixMs = 10500
	})

	accepted, decisions := deduplicator.Filter([]EngineerEvent{first, duplicate})

	if len(accepted) != 1 {
		t.Fatalf("expected duplicate with nil refs to be suppressed safely, got %+v", accepted)
	}
	assertDedupDecision(t, decisions[1], DedupDecisionSuppressed, DedupReasonDuplicateCooldown, first.EventID, first.TimestampUnixMs)
	if want := "track:<none>|layout:<none>|corner:<none>"; !contains(decisions[1].Key, want) {
		t.Fatalf("expected nil refs in stable key as %q, got %q", want, decisions[1].Key)
	}
}

func dedupEvent(mutate func(*EngineerEvent)) EngineerEvent {
	event := validEvent()
	event.EventID = "event-1"
	event.TimestampUnixMs = 10000
	event.TimeRange = &TimeRange{StartUnixMs: 9900, EndUnixMs: 10000}
	event.LapNumber = 2
	event.Type = TypeLateThrottle
	event.Source = EventSource{Kind: SourceDeterministicRule, RuleID: "late_throttle.v1", RuleVersion: "v1"}
	if mutate != nil {
		mutate(&event)
	}
	return event
}

func assertDedupDecision(t *testing.T, decision DedupDecision, wantDecision, wantReason, wantPreviousEventID string, wantPreviousTimestampUnixMs int64) {
	t.Helper()
	if decision.Decision != wantDecision || decision.Reason != wantReason {
		t.Fatalf("expected decision %s/%s, got %+v", wantDecision, wantReason, decision)
	}
	if decision.PreviousEventID != wantPreviousEventID || decision.PreviousTimestampUnixMs != wantPreviousTimestampUnixMs {
		t.Fatalf("expected previous %s/%d, got %+v", wantPreviousEventID, wantPreviousTimestampUnixMs, decision)
	}
}

func contains(value, needle string) bool {
	return strings.Contains(value, needle)
}
