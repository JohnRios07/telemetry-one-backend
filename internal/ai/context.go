package ai

import (
	"fmt"
	"strings"

	"telemetry-one-backend/internal/events"
)

type ContextBuilder struct{}

type ContextSummary struct {
	Session SessionSummary
	Events  []EventSummary
	Safety  SafetySummary
}

type SessionSummary struct {
	SessionID string
	Track     string
	Layout    string
}

type EventSummary struct {
	EventID       string
	Type          events.EventType
	Severity      events.Severity
	Confidence    float64
	LapNumber     int
	Corner        string
	MetricSummary string
}

type SafetySummary struct {
	RedactionPolicy string
	AllowedKinds    []string
}

func NewContextBuilder() ContextBuilder {
	return ContextBuilder{}
}

func (b ContextBuilder) Build(input ConsumerInput) ContextSummary {
	session := b.sessionSummary(input.Session)
	events_ := b.eventSummaries(input.Events)
	safety := b.safetySummary(input.Safety)

	return ContextSummary{
		Session: session,
		Events:  events_,
		Safety:  safety,
	}
}

func (b ContextBuilder) BuildPromptSummary(input ConsumerInput) string {
	summary := b.Build(input)

	var parts []string

	trackInfo := "unknown"
	if summary.Session.Track != "" {
		trackInfo = summary.Session.Track
	}
	layoutInfo := "unknown"
	if summary.Session.Layout != "" {
		layoutInfo = summary.Session.Layout
	}

	parts = append(parts, fmt.Sprintf("Session: %s", summary.Session.SessionID))
	parts = append(parts, fmt.Sprintf("Track: %s", trackInfo))
	parts = append(parts, fmt.Sprintf("Layout: %s", layoutInfo))
	parts = append(parts, "")
	parts = append(parts, fmt.Sprintf("Engineer Events (%d total):", len(summary.Events)))

	for _, ev := range summary.Events {
		cornerStr := "unknown corner"
		if ev.Corner != "" {
			cornerStr = ev.Corner
		}
		parts = append(parts, fmt.Sprintf(
			"  - [%s] %s (severity: %s, confidence: %.2f, lap: %d, corner: %s)",
			ev.EventID, ev.Type, ev.Severity, ev.Confidence, ev.LapNumber, cornerStr,
		))
		if ev.MetricSummary != "" {
			parts = append(parts, fmt.Sprintf("    metrics: %s", ev.MetricSummary))
		}
	}

	parts = append(parts, "")
	parts = append(parts, fmt.Sprintf("Safety: %s", summary.Safety.RedactionPolicy))

	return strings.Join(parts, "\n")
}

func (b ContextBuilder) sessionSummary(s SessionContext) SessionSummary {
	trackStr := ""
	if s.Track != nil && s.Track.Name != nil {
		trackStr = *s.Track.Name
	}
	layoutStr := ""
	if s.Layout != nil && s.Layout.Name != nil {
		layoutStr = *s.Layout.Name
	}
	return SessionSummary{
		SessionID: s.SessionID,
		Track:     trackStr,
		Layout:    layoutStr,
	}
}

func (b ContextBuilder) eventSummaries(envelopes []EventEnvelope) []EventSummary {
	summaries := make([]EventSummary, 0, len(envelopes))
	for _, env := range envelopes {
		e := env.Event
		cornerStr := ""
		if e.Corner != nil && e.Corner.Name != nil {
			cornerStr = *e.Corner.Name
		}
		metricSummary := b.metricSummary(e.Metrics)
		summaries = append(summaries, EventSummary{
			EventID:       e.EventID,
			Type:          e.Type,
			Severity:      e.Severity,
			Confidence:    e.Confidence,
			LapNumber:     e.LapNumber,
			Corner:        cornerStr,
			MetricSummary: metricSummary,
		})
	}
	return summaries
}

func (b ContextBuilder) metricSummary(metrics []events.MetricEvidence) string {
	if len(metrics) == 0 {
		return ""
	}
	parts := make([]string, 0, len(metrics))
	for _, m := range metrics {
		unitStr := ""
		if m.Unit != "" {
			unitStr = " " + m.Unit
		}
		roleStr := ""
		if m.Role != "" {
			roleStr = fmt.Sprintf(" (%s)", m.Role)
		}
		parts = append(parts, fmt.Sprintf("%s=%.2f%s%s", m.Name, m.Value, unitStr, roleStr))
	}
	return strings.Join(parts, ", ")
}

func (b ContextBuilder) safetySummary(s SafetyMetadata) SafetySummary {
	return SafetySummary{
		RedactionPolicy: s.RedactionPolicy,
		AllowedKinds:    s.AllowedInputKinds,
	}
}
