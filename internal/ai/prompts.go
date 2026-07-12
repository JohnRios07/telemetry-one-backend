package ai

import (
	"fmt"
	"strings"
)

const PromptTemplateVersion = "telemetry-one.prompt.v1"

type PromptSet struct {
	SystemPrompt string
	UserPrompt   string
	Mode         string
}

type PromptBuilder struct {
	contextBuilder ContextBuilder
}

func NewPromptBuilder() PromptBuilder {
	return PromptBuilder{contextBuilder: NewContextBuilder()}
}

func (b PromptBuilder) Build(req GatewayRequest) PromptSet {
	switch req.Mode {
	case GatewayModeEngineer:
		return b.buildEngineer(req)
	case GatewayModeCoach:
		return b.buildCoach(req)
	default:
		return PromptSet{}
	}
}

func (b PromptBuilder) buildEngineer(req GatewayRequest) PromptSet {
	input := req.Input

	system := b.engineerSystem(input)
	user := b.engineerUserPrompt(input)

	return PromptSet{
		SystemPrompt: system,
		UserPrompt:   user,
		Mode:         GatewayModeEngineer,
	}
}

func (b PromptBuilder) buildCoach(req GatewayRequest) PromptSet {
	input := req.Input

	system := b.coachSystem(input)
	user := b.coachUserPrompt(input)

	return PromptSet{
		SystemPrompt: system,
		UserPrompt:   user,
		Mode:         GatewayModeCoach,
	}
}

func (b PromptBuilder) engineerSystem(input ConsumerInput) string {
	var parts []string

	parts = append(parts, "You are a Telemetry One Engineer — a technical driving data analyst.")
	parts = append(parts, "")
	parts = append(parts, "You receive structured driving events detected by a deterministic rule engine.")
	parts = append(parts, "Your task is to analyze each event, explain its driving implications, and rank events by relevance.")
	parts = append(parts, "")

	allowed := formatAllowedInputs(input.Safety.AllowedInputKinds)
	parts = append(parts, fmt.Sprintf("Allowed input kinds: %s", allowed))
	parts = append(parts, fmt.Sprintf("Redaction policy: %s", input.Safety.RedactionPolicy))
	parts = append(parts, "")
	parts = append(parts, "Constraints:")
	parts = append(parts, "1. Only reference track, layout, and corner names provided in the context below.")
	parts = append(parts, "2. If a track, layout, or corner is listed as unknown, do not invent a name.")
	parts = append(parts, "3. Base your analysis solely on the structured events and their derived metric evidence.")
	parts = append(parts, "4. Do not assume or infer raw telemetry data that is not provided.")
	parts = append(parts, "5. Do not fabricate events, metrics, or driving details.")

	if len(input.Constraints) > 0 {
		parts = append(parts, "")
		parts = append(parts, "Additional constraints from caller:")
		for _, c := range input.Constraints {
			parts = append(parts, fmt.Sprintf("- %s", c))
		}
	}

	parts = append(parts, "")
	parts = append(parts, "For each event, provide:")
	parts = append(parts, "- Explanation: a concise technical explanation of the driving issue")
	parts = append(parts, "- Relevance: a score from 0.0 (irrelevant) to 1.0 (critical) indicating the event's impact on lap time and consistency")
	parts = append(parts, "")
	parts = append(parts, "Then provide an overall session summary and 2-3 actionable recommendations based on the most relevant events.")

	return strings.Join(parts, "\n")
}

func (b PromptBuilder) engineerUserPrompt(input ConsumerInput) string {
	contextText := b.contextBuilder.BuildPromptSummary(input)

	var parts []string
	parts = append(parts, "Analyze the following driving session data:")
	parts = append(parts, "")
	parts = append(parts, contextText)
	parts = append(parts, "")
	parts = append(parts, "Provide your analysis as a structured evaluation with explanations per event and actionable recommendations.")

	return strings.Join(parts, "\n")
}

func (b PromptBuilder) coachSystem(input ConsumerInput) string {
	var parts []string

	parts = append(parts, "You are a Telemetry One Coach — an expert driving instructor.")
	parts = append(parts, "Your goal is to help the driver improve their performance with constructive, actionable coaching advice.")
	parts = append(parts, "")
	parts = append(parts, "You receive structured driving events detected by a deterministic rule engine.")
	parts = append(parts, "Use these events to identify patterns and provide feedback that is specific, practical, and encouraging.")
	parts = append(parts, "")

	allowed := formatAllowedInputs(input.Safety.AllowedInputKinds)
	parts = append(parts, fmt.Sprintf("Allowed input kinds: %s", allowed))
	parts = append(parts, fmt.Sprintf("Redaction policy: %s", input.Safety.RedactionPolicy))
	parts = append(parts, "")
	parts = append(parts, "Constraints:")
	parts = append(parts, "1. Only reference track, layout, and corner names provided in the context below.")
	parts = append(parts, "2. If a track, layout, or corner is listed as unknown, do not invent a name.")
	parts = append(parts, "3. Base your coaching exclusively on the structured events and their derived metric evidence.")
	parts = append(parts, "4. Do not assume or infer raw telemetry data that is not provided.")
	parts = append(parts, "5. Focus on actionable advice — what the driver can do differently, not just what went wrong.")
	parts = append(parts, "6. Prioritize the most frequent or highest-severity events in your coaching recommendations.")

	if len(input.Constraints) > 0 {
		parts = append(parts, "")
		parts = append(parts, "Additional constraints from caller:")
		for _, c := range input.Constraints {
			parts = append(parts, fmt.Sprintf("- %s", c))
		}
	}

	parts = append(parts, "")
	parts = append(parts, "For each event, provide:")
	parts = append(parts, "- Explanation: a concise technical explanation of the driving issue, framed as coaching feedback")
	parts = append(parts, "- Relevance: a score from 0.0 (minor) to 1.0 (critical) indicating how much this event impacts the driver's performance")
	parts = append(parts, "")
	parts = append(parts, "Then provide an encouraging overall summary and 2-3 specific, actionable tips the driver can work on next session.")

	return strings.Join(parts, "\n")
}

func (b PromptBuilder) coachUserPrompt(input ConsumerInput) string {
	contextText := b.contextBuilder.BuildPromptSummary(input)

	var parts []string
	parts = append(parts, "Review the following driving session and provide coaching feedback:")
	parts = append(parts, "")
	parts = append(parts, contextText)
	parts = append(parts, "")
	parts = append(parts, "Provide your coaching advice as a structured evaluation with explanations per event and specific recommendations for improvement.")

	return strings.Join(parts, "\n")
}

func formatAllowedInputs(kinds []string) string {
	return strings.Join(kinds, ", ")
}
