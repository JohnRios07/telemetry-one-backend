package ai

import (
	"strings"
	"testing"

	"telemetry-one-backend/internal/events"
)

func TestPromptBuilderBuildReturnsEngineerPromptSet(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)

	prompt := builder.Build(req)

	if prompt.Mode != GatewayModeEngineer {
		t.Fatalf("expected engineer mode, got %s", prompt.Mode)
	}
	if prompt.SystemPrompt == "" {
		t.Fatalf("expected non-empty system prompt")
	}
	if prompt.UserPrompt == "" {
		t.Fatalf("expected non-empty user prompt")
	}
}

func TestPromptBuilderBuildReturnsCoachPromptSet(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeCoach)

	prompt := builder.Build(req)

	if prompt.Mode != GatewayModeCoach {
		t.Fatalf("expected coach mode, got %s", prompt.Mode)
	}
	if prompt.SystemPrompt == "" {
		t.Fatalf("expected non-empty system prompt")
	}
	if prompt.UserPrompt == "" {
		t.Fatalf("expected non-empty user prompt")
	}
}

func TestPromptBuilderEngineerSystemPromptContainsRole(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)

	prompt := builder.Build(req)

	if !strings.Contains(prompt.SystemPrompt, "Telemetry One Engineer") {
		t.Fatalf("engineer system prompt should mention Engineer role")
	}
	if !strings.Contains(prompt.SystemPrompt, "technical") {
		t.Fatalf("engineer system prompt should have technical tone")
	}
}

func TestPromptBuilderCoachSystemPromptContainsRole(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeCoach)

	prompt := builder.Build(req)

	if !strings.Contains(prompt.SystemPrompt, "Telemetry One Coach") {
		t.Fatalf("coach system prompt should mention Coach role")
	}
	if !strings.Contains(prompt.SystemPrompt, "coaching") {
		t.Fatalf("coach system prompt should have coaching tone")
	}
}

func TestPromptBuilderEngineerAndCoachDiffer(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)

	engPrompt := builder.Build(req)
	req.Mode = GatewayModeCoach
	coachPrompt := builder.Build(req)

	if engPrompt.SystemPrompt == coachPrompt.SystemPrompt {
		t.Fatalf("engineer and coach system prompts must differ")
	}
}

func TestPromptBuilderSystemPromptContainsAllowedInputKinds(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)

	prompt := builder.Build(req)

	if !strings.Contains(prompt.SystemPrompt, AllowedInputEngineerEvents) {
		t.Fatalf("system prompt should list allowed input engineer_events")
	}
	if !strings.Contains(prompt.SystemPrompt, AllowedInputDerivedMetrics) {
		t.Fatalf("system prompt should list allowed input derived_metrics")
	}
	if !strings.Contains(prompt.SystemPrompt, AllowedInputCatalogRefs) {
		t.Fatalf("system prompt should list allowed input catalog_refs")
	}
}

func TestPromptBuilderSystemPromptContainsConstraints(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)

	prompt := builder.Build(req)
	lower := strings.ToLower(prompt.SystemPrompt)

	checks := []string{
		"do not invent",
		"do not assume",
		"do not fabricate",
		"derive",
	}
	for _, check := range checks {
		if !strings.Contains(lower, check) {
			t.Fatalf("system prompt should contain constraint %q", check)
		}
	}
}

func TestPromptBuilderSystemPromptContainsRedactionPolicy(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)

	prompt := builder.Build(req)

	if !strings.Contains(prompt.SystemPrompt, req.Input.Safety.RedactionPolicy) {
		t.Fatalf("system prompt should contain redaction policy from input")
	}
}

func TestPromptBuilderSystemPromptContainsCallerConstraints(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)
	req.Input.Constraints = append(req.Input.Constraints, "custom constraint from caller")

	prompt := builder.Build(req)

	if !strings.Contains(prompt.SystemPrompt, "custom constraint from caller") {
		t.Fatalf("system prompt should contain caller constraints")
	}
}

func TestPromptBuilderUserPromptContainsEventSummaries(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)

	prompt := builder.Build(req)

	if !strings.Contains(prompt.UserPrompt, "late_throttle") {
		t.Fatalf("user prompt should contain event type late_throttle")
	}
	if !strings.Contains(prompt.UserPrompt, "session-1") {
		t.Fatalf("user prompt should contain session ID")
	}
}

func TestPromptBuilderUserPromptContainsMetrics(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)

	prompt := builder.Build(req)

	if !strings.Contains(prompt.UserPrompt, "throttleReapplicationDeltaMs") {
		t.Fatalf("user prompt should contain derived metric name")
	}
	if !strings.Contains(prompt.UserPrompt, "320") {
		t.Fatalf("user prompt should contain metric value")
	}
}

func TestPromptBuilderUserPromptDoesNotContainRawTelemetry(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)

	prompt := builder.Build(req)

	for _, field := range []string{"positionX", "positionY", "positionZ", "speedMps", "brake", "steering", "wheelSpeedFL", "fuelLiters"} {
		if strings.Contains(prompt.UserPrompt, field) {
			t.Fatalf("user prompt must not contain raw telemetry field %q", field)
		}
	}
}

func TestPromptBuilderSystemPromptDoesNotContainRawTelemetry(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)

	prompt := builder.Build(req)

	for _, field := range []string{"positionX", "positionY", "speedMps", "throttle", "brake", "steering"} {
		if strings.Contains(prompt.SystemPrompt, field) {
			t.Fatalf("system prompt must not contain raw telemetry field %q", field)
		}
	}
}

func TestPromptBuilderWithUnknownCatalogRefsShowsUnknown(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)
	req.Input.Session.Track = nil
	req.Input.Session.Layout = nil
	req.Input.Events[0].Event.Corner = nil

	prompt := builder.Build(req)

	if !strings.Contains(prompt.UserPrompt, "unknown") {
		t.Fatalf("user prompt should indicate unknown track/layout/corner, got: %s", prompt.UserPrompt)
	}
}

func TestPromptBuilderWithMultipleEventsShowsAll(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)

	secondEvent := validConsumerInput().Events[0]
	secondEvent.Event.EventID = "event-2"
	secondEvent.Event.Type = events.TypeLowExitSpeed
	secondEvent.Event.Severity = events.SeverityHigh
	req.Input.Events = append(req.Input.Events, secondEvent)

	prompt := builder.Build(req)

	if !strings.Contains(prompt.UserPrompt, "event-1") {
		t.Fatalf("user prompt should contain first event ID")
	}
	if !strings.Contains(prompt.UserPrompt, "event-2") {
		t.Fatalf("user prompt should contain second event ID")
	}
	if !strings.Contains(prompt.UserPrompt, string(events.TypeLowExitSpeed)) {
		t.Fatalf("user prompt should contain second event type")
	}
}

func TestPromptBuilderUserPromptContainsSafetyPolicy(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)

	prompt := builder.Build(req)

	if !strings.Contains(prompt.UserPrompt, "Safety:") {
		t.Fatalf("user prompt should contain safety policy section")
	}
}

func TestPromptBuilderUserPromptContainsResponseFormatInstruction(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)

	prompt := builder.Build(req)

	if !strings.Contains(prompt.UserPrompt, "structured") {
		t.Fatalf("user prompt should request structured output")
	}
}

func TestPromptBuilderEngineerSystemPromptPromisesEventExplanations(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)

	prompt := builder.Build(req)

	if !strings.Contains(prompt.SystemPrompt, "Explanation") {
		t.Fatalf("engineer system prompt should mention explanation per event")
	}
	if !strings.Contains(prompt.SystemPrompt, "Relevance") {
		t.Fatalf("engineer system prompt should mention relevance score")
	}
}

func TestPromptBuilderCoachSystemPromptPromisesActionableAdvice(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeCoach)

	prompt := builder.Build(req)

	if !strings.Contains(prompt.SystemPrompt, "actionable") {
		t.Fatalf("coach system prompt should mention actionable advice")
	}
	if !strings.Contains(prompt.SystemPrompt, "encouraging") {
		t.Fatalf("coach system prompt should have encouraging tone")
	}
}

func TestPromptBuilderDeterministicSameInputSameOutput(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)

	prompt1 := builder.Build(req)
	prompt2 := builder.Build(req)

	if prompt1.SystemPrompt != prompt2.SystemPrompt {
		t.Fatalf("prompt builder must be deterministic: system prompts differ")
	}
	if prompt1.UserPrompt != prompt2.UserPrompt {
		t.Fatalf("prompt builder must be deterministic: user prompts differ")
	}
}

func TestPromptBuilderEngineerSystemPromptMentionsSessionSummary(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)

	prompt := builder.Build(req)

	if !strings.Contains(prompt.SystemPrompt, "session summary") {
		t.Fatalf("engineer system prompt should mention session summary output")
	}
	if !strings.Contains(prompt.SystemPrompt, "recommendations") {
		t.Fatalf("engineer system prompt should mention recommendations output")
	}
}

func TestPromptBuilderCoachSystemPromptMentionsPatterns(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeCoach)

	prompt := builder.Build(req)

	if !strings.Contains(prompt.SystemPrompt, "patterns") {
		t.Fatalf("coach system prompt should mention identifying patterns")
	}
}

func TestPromptBuilderEngineerSystemPromptNotesImpactType(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)

	prompt := builder.Build(req)

	if !strings.Contains(prompt.SystemPrompt, "lap time") && !strings.Contains(prompt.SystemPrompt, "consistency") {
		t.Fatalf("engineer system prompt should mention lap time or consistency impact")
	}
}

func TestPromptBuilderCoachSystemPromptPrioritizesBySeverity(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeCoach)

	prompt := builder.Build(req)

	if !strings.Contains(prompt.SystemPrompt, "severity") && !strings.Contains(prompt.SystemPrompt, "frequent") {
		t.Fatalf("coach system prompt should mention prioritizing by severity or frequency")
	}
}

func TestPromptBuilderWithNoCallerConstraints(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)
	req.Input.Constraints = nil

	prompt := builder.Build(req)

	if prompt.SystemPrompt == "" {
		t.Fatalf("expected non-empty system prompt even without caller constraints")
	}
}

func TestPromptBuilderCoachUserPromptContainsCoachingFraming(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeCoach)

	prompt := builder.Build(req)

	if !strings.Contains(prompt.UserPrompt, "coaching") {
		t.Fatalf("coach user prompt should mention coaching")
	}
}

func TestPromptBuilderEngineerUserPromptContainsAnalysisFraming(t *testing.T) {
	builder := NewPromptBuilder()
	req := validGatewayRequest(t, GatewayModeEngineer)

	prompt := builder.Build(req)

	if !strings.Contains(prompt.UserPrompt, "Analyze") {
		t.Fatalf("engineer user prompt should request analysis")
	}
}
