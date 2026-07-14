package events

import (
	"fmt"
	"strings"
)

const (
	DefaultEventCooldownWindowMs int64 = 30000

	DedupDecisionAccepted   = "accepted"
	DedupDecisionSuppressed = "suppressed"

	DedupReasonFirstSeen          = "first_seen"
	DedupReasonDifferentContext   = "different_context"
	DedupReasonCooldownExpired    = "cooldown_expired"
	DedupReasonDuplicateCooldown  = "duplicate_inside_cooldown"
	DedupReasonOutOfOrderCooldown = "out_of_order_inside_cooldown"
)

type DedupOptions struct {
	CooldownWindowMs int64
}

type DedupDecision struct {
	Event                   EngineerEvent
	Key                     string
	Decision                string
	Reason                  string
	PreviousEventID         string
	PreviousTimestampUnixMs int64
}

type EventDeduplicator struct {
	options DedupOptions
	seen    map[string]EngineerEvent
}

func NewEventDeduplicator(options DedupOptions) *EventDeduplicator {
	return &EventDeduplicator{
		options: normalizeDedupOptions(options),
		seen:    make(map[string]EngineerEvent),
	}
}

func (d *EventDeduplicator) Filter(events []EngineerEvent) ([]EngineerEvent, []DedupDecision) {
	if d.seen == nil {
		d.seen = make(map[string]EngineerEvent)
	}
	d.options = normalizeDedupOptions(d.options)

	accepted := make([]EngineerEvent, 0, len(events))
	decisions := make([]DedupDecision, 0, len(events))
	for _, event := range events {
		key := DedupKey(event)
		previous, exists := d.seen[key]
		decision := DedupDecision{Event: event, Key: key, Decision: DedupDecisionAccepted, Reason: DedupReasonFirstSeen}

		if exists {
			decision.PreviousEventID = previous.EventID
			decision.PreviousTimestampUnixMs = previous.TimestampUnixMs
			delta := event.TimestampUnixMs - previous.TimestampUnixMs
			if delta >= 0 && delta < d.options.CooldownWindowMs {
				decision.Decision = DedupDecisionSuppressed
				decision.Reason = DedupReasonDuplicateCooldown
			} else if delta < 0 && -delta < d.options.CooldownWindowMs {
				decision.Decision = DedupDecisionSuppressed
				decision.Reason = DedupReasonOutOfOrderCooldown
			} else {
				decision.Reason = DedupReasonCooldownExpired
			}
		} else if len(d.seen) > 0 {
			decision.Reason = DedupReasonDifferentContext
		}

		decisions = append(decisions, decision)
		if decision.Decision == DedupDecisionSuppressed {
			continue
		}

		d.seen[key] = event
		accepted = append(accepted, event)
	}

	return accepted, decisions
}

func DedupKey(event EngineerEvent) string {
	parts := []string{
		strings.TrimSpace(event.SessionID),
		string(event.Type),
		fmt.Sprintf("lap:%d", event.LapNumber),
		"track:" + catalogRefID(event.Track),
		"layout:" + catalogRefID(event.Layout),
		"corner:" + catalogRefID(event.Corner),
		"source_kind:" + string(event.Source.Kind),
		"rule_id:" + event.Source.RuleID,
		"rule_version:" + event.Source.RuleVersion,
	}
	if event.TimeRange != nil {
		parts = append(parts, fmt.Sprintf("time_range:%d-%d", event.TimeRange.StartUnixMs, event.TimeRange.EndUnixMs))
	}
	return strings.Join(parts, "|")
}

func catalogRefID(ref *CatalogRef) string {
	if ref == nil || ref.ID == nil {
		return "<none>"
	}
	id := strings.TrimSpace(*ref.ID)
	if id == "" {
		return "<none>"
	}
	return id
}

func normalizeDedupOptions(options DedupOptions) DedupOptions {
	if options.CooldownWindowMs <= 0 {
		options.CooldownWindowMs = DefaultEventCooldownWindowMs
	}
	return options
}
