package sah

import (
	"encoding/json"
	"fmt"
	"strings"
)

func ResolveAssignmentAgentRequest(assignment Assignment) (*AssignmentAgentRequest, error) {
	if assignment.AgentRequest != nil && strings.TrimSpace(assignment.AgentRequest.Prompt) != "" {
		// Compatibility:
		// - sah-cli v0.9.x uses the server-owned execution contract when present.
		// - Older servers and <= v0.8.x assignment shapes still fall back to the
		//   legacy instructions-based prompt builder below.
		return assignment.AgentRequest, nil
	}

	prompt, err := BuildLegacyAgentPrompt(assignment)
	if err != nil {
		return nil, err
	}
	return &AssignmentAgentRequest{
		Title:          assignment.TaskType,
		Description:    "Return one JSON object that matches the submission schema.",
		Prompt:         prompt,
		ResponseSchema: assignment.Instructions.SubmissionSchema,
	}, nil
}

func BuildAgentPrompt(assignment Assignment) (string, error) {
	request, err := ResolveAssignmentAgentRequest(assignment)
	if err != nil {
		return "", err
	}
	return request.Prompt, nil
}

func BuildLegacyAgentPrompt(assignment Assignment) (string, error) {
	// Compatibility shim for sah-cli <= v0.8.x and for servers that still only
	// expose the prompt-authoring `instructions` shape.
	payloadJSON, err := prettyJSON(assignment.Payload)
	if err != nil {
		return "", fmt.Errorf("encode assignment payload: %w", err)
	}
	schemaJSON, err := prettyJSON(assignment.Instructions.SubmissionSchema)
	if err != nil {
		return "", fmt.Errorf("encode submission schema: %w", err)
	}
	examplesJSON, err := prettyJSON(assignment.Instructions.GoodExamples)
	if err != nil {
		return "", fmt.Errorf("encode good examples: %w", err)
	}

	lines := []string{
		"You are solving exactly one SCIENCE@home assignment.",
		"Return only the submission payload JSON object.",
		"Do not wrap the payload inside assignment_id or task_type.",
		"Do not print Markdown fences, prose, or explanations.",
		"If you cannot produce a compliant payload from the provided data, print a single line starting with ABORT: and a brief reason.",
		"You do not have file or network access. Use only the assignment payload and instructions below.",
		"",
		fmt.Sprintf("assignment_id: %d", assignment.AssignmentID),
		fmt.Sprintf("task_type: %s", assignment.TaskType),
		fmt.Sprintf("task_key: %s", assignment.TaskKey),
		fmt.Sprintf("instruction_version: %s", assignment.InstructionVersion),
		fmt.Sprintf("schema_version: %s", assignment.SchemaVersion),
		"",
		"Assignment payload:",
		payloadJSON,
		"",
		"Instructions summary:",
		coalesceText(assignment.Instructions.Summary, "(none)"),
		"",
		"Rules:",
		formatStringList(assignment.Instructions.Rules),
		"",
		"Bad patterns:",
		formatStringList(assignment.Instructions.BadPatterns),
		"",
		"Stop conditions:",
		formatStringList(assignment.Instructions.StopConditions),
		"",
		"Good examples:",
		examplesJSON,
		"",
		"Submission schema:",
		schemaJSON,
		"",
		"Final requirement:",
		"Print exactly one JSON object that matches the submission schema, or ABORT: <reason>.",
	}

	return strings.Join(lines, "\n"), nil
}

func prettyJSON(value any) (string, error) {
	if value == nil {
		return "null", nil
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func formatStringList(values []string) string {
	if len(values) == 0 {
		return "- (none)"
	}
	lines := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		lines = append(lines, "- "+trimmed)
	}
	if len(lines) == 0 {
		return "- (none)"
	}
	return strings.Join(lines, "\n")
}

func coalesceText(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
