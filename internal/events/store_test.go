package events

import (
	"context"
	"errors"
	"testing"
)

func TestStoreAppendsValidEventsAndListsBySession(t *testing.T) {
	store := NewStore(10, DedupOptions{CooldownWindowMs: 1000})
	event := validEvent()

	stored, accepted, err := store.Append(context.Background(), event)
	if err != nil {
		t.Fatalf("expected valid event to store: %v", err)
	}
	if !accepted || stored.EventID != event.EventID {
		t.Fatalf("expected event accepted, got accepted=%v stored=%+v", accepted, stored)
	}

	got, err := store.List(context.Background(), Query{SessionID: event.SessionID})
	if err != nil {
		t.Fatalf("expected list to succeed: %v", err)
	}
	if len(got) != 1 || got[0].EventID != event.EventID {
		t.Fatalf("expected stored event, got %+v", got)
	}
}

func TestStoreRejectsInvalidRawTelemetryMetrics(t *testing.T) {
	store := NewStore(10, DedupOptions{})
	event := validEvent()
	event.Metrics = []MetricEvidence{{Name: "speedMps", Value: 45, Status: MetricStatusAvailable, Role: MetricRoleActual}}

	_, accepted, err := store.Append(context.Background(), event)
	if !errors.Is(err, ErrRawTelemetryMetric) {
		t.Fatalf("expected raw telemetry metric rejection, got %v", err)
	}
	if accepted {
		t.Fatalf("expected invalid event not to be accepted")
	}

	got, err := store.List(context.Background(), Query{SessionID: event.SessionID})
	if err != nil {
		t.Fatalf("expected list to succeed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected invalid event not to be stored, got %+v", got)
	}
}

func TestStoreSuppressesDuplicateInsideCooldown(t *testing.T) {
	store := NewStore(10, DedupOptions{CooldownWindowMs: 1000})
	first := dedupEvent(func(event *EngineerEvent) { event.TimestampUnixMs = 10000 })
	duplicate := dedupEvent(func(event *EngineerEvent) {
		event.EventID = "duplicate"
		event.TimestampUnixMs = 10500
	})

	if _, accepted, err := store.Append(context.Background(), first); err != nil || !accepted {
		t.Fatalf("expected first event accepted, accepted=%v err=%v", accepted, err)
	}
	if _, accepted, err := store.Append(context.Background(), duplicate); err != nil || accepted {
		t.Fatalf("expected duplicate suppressed without error, accepted=%v err=%v", accepted, err)
	}

	got, err := store.List(context.Background(), Query{SessionID: first.SessionID})
	if err != nil {
		t.Fatalf("expected list to succeed: %v", err)
	}
	if len(got) != 1 || got[0].EventID != first.EventID {
		t.Fatalf("expected only first event stored, got %+v", got)
	}
}

func TestStoreFiltersBySessionLapCornerAndType(t *testing.T) {
	store := NewStore(10, DedupOptions{CooldownWindowMs: 1000})
	match := validEvent()
	match.EventID = "match"
	match.SessionID = "session-filter"
	match.LapNumber = 3
	match.Type = TypeLowExitSpeed
	match.Source.RuleID = "low_exit_speed.v1"
	match.Corner = &CatalogRef{ID: stringPtr("corner-filter"), DisplayStrategy: DisplayStrategyIDOnly}
	otherLap := match
	otherLap.EventID = "other-lap"
	otherLap.LapNumber = 4
	otherLap.TimestampUnixMs += 1
	otherSession := match
	otherSession.EventID = "other-session"
	otherSession.SessionID = "session-other"
	otherSession.TimestampUnixMs += 2

	for _, event := range []EngineerEvent{match, otherLap, otherSession} {
		if _, accepted, err := store.Append(context.Background(), event); err != nil || !accepted {
			t.Fatalf("expected event accepted, accepted=%v err=%v", accepted, err)
		}
	}

	lap := 3
	got, err := store.List(context.Background(), Query{SessionID: "session-filter", LapNumber: &lap, CornerID: "corner-filter", Type: TypeLowExitSpeed})
	if err != nil {
		t.Fatalf("expected filtered list to succeed: %v", err)
	}
	if len(got) != 1 || got[0].EventID != "match" {
		t.Fatalf("expected only matching event, got %+v", got)
	}
}

func TestStoreReturnsEmptyResultForUnknownSession(t *testing.T) {
	store := NewStore(10, DedupOptions{})
	got, err := store.List(context.Background(), Query{SessionID: "missing-session"})
	if err != nil {
		t.Fatalf("expected list to succeed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty result, got %+v", got)
	}
}
