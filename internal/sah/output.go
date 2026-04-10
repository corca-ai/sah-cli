package sah

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

type AbortError struct {
	Reason string
}

func (err *AbortError) Error() string {
	if err.Reason == "" {
		return "agent aborted"
	}
	return "agent aborted: " + err.Reason
}

func ParseAgentPayload(raw string) (map[string]any, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("agent produced empty output")
	}

	if reason, ok := parseAbort(trimmed); ok {
		return nil, &AbortError{Reason: reason}
	}

	candidates := []string{trimmed}
	if fenced := extractFencedBlock(trimmed); fenced != "" {
		candidates = append(candidates, fenced)
	}
	if extracted := extractJSONObject(trimmed); extracted != "" {
		candidates = append(candidates, extracted)
	}

	for _, candidate := range candidates {
		payload, err := decodeJSONObject(candidate)
		if err == nil {
			return payload, nil
		}
	}

	return nil, fmt.Errorf("agent output did not contain a valid JSON object")
}

func parseAbort(value string) (string, bool) {
	upper := strings.ToUpper(value)
	if !strings.HasPrefix(upper, "ABORT:") {
		return "", false
	}
	return strings.TrimSpace(value[len("ABORT:"):]), true
}

func extractFencedBlock(value string) string {
	start := strings.Index(value, "```")
	if start < 0 {
		return ""
	}
	rest := value[start+3:]
	if newline := strings.Index(rest, "\n"); newline >= 0 {
		rest = rest[newline+1:]
	}
	end := strings.Index(rest, "```")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

func extractJSONObject(value string) string {
	start := strings.IndexByte(value, '{')
	if start < 0 {
		return ""
	}
	for end := len(value); end > start; end-- {
		candidate := strings.TrimSpace(value[start:end])
		if _, err := decodeJSONObject(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func decodeJSONObject(value string) (map[string]any, error) {
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()

	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}

	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return nil, fmt.Errorf("multiple json documents found")
	} else if !errors.Is(err, io.EOF) {
		return nil, err
	}
	return payload, nil
}
