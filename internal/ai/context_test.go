package ai

import (
	"strings"
	"testing"

	"telemetry-one-backend/internal/events"
)

func TestContextBuilderBuildReturnsStructuredSummary(t *testing.T) {
	builder := NewContextBuilder()
	input := validConsumerInput()

	summary := builder.Build(input)

	if summary.Session.SessionID != "session-1" {
		t.Fatalf("expected session-1, got %s", summary.Session.SessionID)
	}
	if summary.Session.Track != "Watkins Glen International" {
		t.Fatalf("expected Watkins Glen International, got %s", summary.Session.Track)
	}
	if summary.Session.Layout != "Long Course" {
		t.Fatalf("expected Long Course, got %s", summary.Session.Layout)
	}
	if len(summary.Events) != 1 {
		t.Fatalf("expected 1 event summary, got %d", len(summary.Events))
	}
	if summary.Events[0].Type != events.TypeLateThrottle {
		t.Fatalf("expected late_throttle, got %s", summary.Events[0].Type)
	}
	if summary.Events[0].Severity != events.SeverityMedium {
		t.Fatalf("expected medium, got %s", summary.Events[0].Severity)
	}
	if summary.Events[0].LapNumber != 2 {
		t.Fatalf("expected lap 2, got %d", summary.Events[0].LapNumber)
	}
	if len(summary.Safety.AllowedKinds) != 5 {
		t.Fatalf("expected 5 allowed kinds, got %d", len(summary.Safety.AllowedKinds))
	}
}

func TestContextBuilderBuildPromptSummaryFormatsNicely(t *testing.T) {
	builder := NewContextBuilder()
	input := validConsumerInput()

	promptSummary := builder.BuildPromptSummary(input)

	lines := strings.Split(promptSummary, "\n")
	if len(lines) < 6 {
		t.Fatalf("expected prompt summary to have multiple lines, got %d", len(lines))
	}
	if !strings.Contains(promptSummary, "Session: session-1") {
		t.Fatalf("expected session ID in prompt summary")
	}
	if !strings.Contains(promptSummary, "Engineer Events (1 total)") {
		t.Fatalf("expected event count in prompt summary")
	}
	if !strings.Contains(promptSummary, string(events.TypeLateThrottle)) {
		t.Fatalf("expected event type in prompt summary")
	}
	if !strings.Contains(promptSummary, "Safety: no raw telemetry") {
		t.Fatalf("expected safety policy in prompt summary")
	}
}

func TestContextBuilderBuildPromptSummaryRendersUnknownLayoutCleanly(t *testing.T) {
	builder := NewContextBuilder()
	input := validConsumerInput()
	input.Session.Layout = nil

	promptSummary := builder.BuildPromptSummary(input)

	if !strings.Contains(promptSummary, "Track: Watkins Glen International") {
		t.Fatalf("expected track name in prompt summary")
	}
	if !strings.Contains(promptSummary, "Layout: unknown") {
		t.Fatalf("expected unknown layout in prompt summary, got %q", promptSummary)
	}
}

func TestContextBuilderBuildIncludesMetricEvidence(t *testing.T) {
	builder := NewContextBuilder()
	input := validConsumerInput()

	promptSummary := builder.BuildPromptSummary(input)

	if !strings.Contains(promptSummary, "throttleReapplicationDeltaMs") {
		t.Fatalf("expected metric name in prompt summary")
	}
	if !strings.Contains(promptSummary, "320") {
		t.Fatalf("expected metric value in prompt summary")
	}
}

func TestContextBuilderBuildWithUnknownCatalogRefs(t *testing.T) {
	builder := NewContextBuilder()
	input := validConsumerInput()
	input.Session.Track = nil
	input.Session.Layout = nil
	input.Events[0].Event.Track = nil
	input.Events[0].Event.Layout = nil
	input.Events[0].Event.Corner = nil

	summary := builder.Build(input)

	if summary.Session.Track != "" {
		t.Fatalf("expected empty track for nil ref, got %s", summary.Session.Track)
	}
	if summary.Session.Layout != "" {
		t.Fatalf("expected empty layout for nil ref, got %s", summary.Session.Layout)
	}
	if summary.Events[0].Corner != "" {
		t.Fatalf("expected empty corner for nil ref, got %s", summary.Events[0].Corner)
	}
}

func TestContextBuilderBuildPromptSummaryDoesNotContainRawTelemetry(t *testing.T) {
	builder := NewContextBuilder()
	input := validConsumerInput()

	promptSummary := builder.BuildPromptSummary(input)

	for _, field := range []string{"positionX", "positionY", "positionZ", "speedMps", "brake", "steering"} {
		if strings.Contains(promptSummary, field) {
			t.Fatalf("expected prompt summary not to contain raw telemetry field %q", field)
		}
	}
	if strings.Contains(promptSummary, "throttle") && !strings.Contains(promptSummary, "throttleReapplicationDeltaMs") {
		t.Fatalf("expected prompt summary not to contain standalone raw telemetry field 'throttle'")
	}
}

func TestContextBuilderBuildWithMultipleEvents(t *testing.T) {
	builder := NewContextBuilder()
	input := validConsumerInput()

	secondEvent := validConsumerInput().Events[0]
	secondEvent.Event.EventID = "event-2"
	secondEvent.Event.Type = events.TypeLowExitSpeed
	secondEvent.Event.Severity = events.SeverityHigh
	input.Events = append(input.Events, secondEvent)

	summary := builder.Build(input)

	if len(summary.Events) != 2 {
		t.Fatalf("expected 2 event summaries, got %d", len(summary.Events))
	}
	if summary.Events[1].Type != events.TypeLowExitSpeed {
		t.Fatalf("expected low_exit_speed, got %s", summary.Events[1].Type)
	}
	if summary.Events[1].Severity != events.SeverityHigh {
		t.Fatalf("expected high severity, got %s", summary.Events[1].Severity)
	}
}

func TestContextBuilderBuildWithEmptyMetrics(t *testing.T) {
	builder := NewContextBuilder()
	input := validConsumerInput()
	input.Events[0].Event.Metrics = []events.MetricEvidence{
		{Name: "throttleReapplicationDeltaMs", Value: 320, Unit: "ms", Status: events.MetricStatusAvailable, Role: events.MetricRoleDelta},
	}

	summary := builder.Build(input)
	first := summary.Events[0]

	if first.MetricSummary == "" {
		t.Fatalf("expected metric summary to be non-empty")
	}
	if !strings.Contains(first.MetricSummary, "320") {
		t.Fatalf("expected metric value in summary")
	}
}

func TestContextBuilderMetricSummaryHandlesUnavailableStatus(t *testing.T) {
	builder := NewContextBuilder()
	metrics := []events.MetricEvidence{
		{
			Name: "referenceExitSpeedKph", Value: 0,
			Unit: "kph", Status: events.MetricStatusUnavailable,
			Reason: "no reference lap available", Role: events.MetricRoleReference,
		},
	}

	summary := builder.metricSummary(metrics)

	if !strings.Contains(summary, "referenceExitSpeedKph") {
		t.Fatalf("expected metric name, got %q", summary)
	}
	if !strings.Contains(summary, "reference") {
		t.Fatalf("expected role in metric summary, got %q", summary)
	}
}

func TestContextBuilderEventSummaryWithAllFieldTypes(t *testing.T) {
	builder := NewContextBuilder()
	input := validConsumerInput()

	cornerID := "corner-5"
	cornerName := "Turn 5"
	input.Events[0].Event.Corner = &events.CatalogRef{
		ID:              &cornerID,
		Name:            &cornerName,
		DisplayStrategy: events.DisplayStrategyCatalogName,
	}

	summary := builder.Build(input)

	if summary.Events[0].Corner != "Turn 5" {
		t.Fatalf("expected 'Turn 5', got %q", summary.Events[0].Corner)
	}
}
