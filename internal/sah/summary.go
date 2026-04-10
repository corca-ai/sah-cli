package sah

import (
	"fmt"
	"strings"
)

func SummarizeContribution(assignment Assignment, payload map[string]any) string {
	switch assignment.TaskType {
	case "extraction":
		return summarizeExtraction(assignment, payload)
	case "verification":
		return summarizeVerification(assignment, payload)
	case "linking":
		return summarizeLinking(assignment, payload)
	case "refinement":
		return summarizeRefinement(assignment, payload)
	default:
		return describeSubject(assignment)
	}
}

func summarizeExtraction(assignment Assignment, payload map[string]any) string {
	title := quoteIfPresent(stringField(assignment.Payload, "title"))
	claims := arrayLen(payload["claims"])
	concepts := arrayLen(payload["concepts"])
	if title != "" {
		return fmt.Sprintf("extracted %d claims and %d concepts from %s", claims, concepts, title)
	}
	return fmt.Sprintf("extracted %d claims and %d concepts", claims, concepts)
}

func summarizeVerification(assignment Assignment, payload map[string]any) string {
	subject := quoteIfPresent(stringField(assignment.Payload, "title"))
	if subject == "" {
		subject = describeSubject(assignment)
	}
	verdict := strings.TrimSpace(stringField(payload, "verdict"))
	reason := strings.TrimSpace(stringField(payload, "reason"))
	if verdict == "" {
		return fmt.Sprintf("reviewed %s", subject)
	}
	if reason == "" {
		return fmt.Sprintf("%s %s", verdict, subject)
	}
	return fmt.Sprintf("%s %s with reason: %s", verdict, subject, reason)
}

func summarizeLinking(assignment Assignment, payload map[string]any) string {
	subject := describeSubject(assignment)
	conceptLinks := arrayLen(payload["concept_links"])
	claimLinks := arrayLen(payload["claim_links"])
	return fmt.Sprintf(
		"added %d concept links and %d claim links for %s",
		conceptLinks,
		claimLinks,
		subject,
	)
}

func summarizeRefinement(assignment Assignment, payload map[string]any) string {
	subType := stringField(payload, "sub_type")
	if subType == "" {
		subType = stringField(assignment.Payload, "sub_type")
	}

	switch subType {
	case "merge":
		return fmt.Sprintf(
			"proposed %d merge groups from %d candidate concepts",
			arrayLen(payload["groups"]),
			arrayLen(assignment.Payload["candidates"]),
		)
	case "enrich":
		concept := nestedStringField(assignment.Payload, "concept", "name")
		if concept == "" {
			concept = nestedStringField(payload, "concept", "name")
		}
		return fmt.Sprintf(
			"enriched %s with %d aliases and %d types",
			quoteOrFallback(concept, "a concept"),
			arrayLen(payload["aliases"]),
			arrayLen(payload["types"]),
		)
	case "reextract":
		title := stringField(assignment.Payload, "title")
		if title == "" {
			title = stringField(payload, "title")
		}
		return fmt.Sprintf(
			"re-extracted %d claims and %d concepts for %s",
			arrayLen(payload["claims"]),
			arrayLen(payload["concepts"]),
			quoteOrFallback(title, describeSubject(assignment)),
		)
	default:
		return fmt.Sprintf("refined %s", describeSubject(assignment))
	}
}

func describeSubject(assignment Assignment) string {
	payload := assignment.Payload
	if title := stringField(payload, "title"); title != "" {
		return quoteIfPresent(title)
	}
	if taskType := assignment.TaskType; taskType == "linking" {
		papers := arrayField(payload, "papers")
		if len(papers) > 0 {
			if paper, ok := papers[0].(map[string]any); ok {
				if title := stringField(paper, "title"); title != "" {
					return fmt.Sprintf("%s cluster", quoteIfPresent(title))
				}
			}
		}
		return "a linking cluster"
	}
	if taskType := assignment.TaskType; taskType == "refinement" {
		if concept := nestedStringField(payload, "concept", "name"); concept != "" {
			return quoteIfPresent(concept)
		}
		if subType := stringField(payload, "sub_type"); subType != "" {
			return fmt.Sprintf("%s refinement", subType)
		}
	}
	if corpusID := stringOrNumberField(payload, "corpus_id"); corpusID != "" {
		return "corpus " + corpusID
	}
	return assignment.TaskType
}

func quoteOrFallback(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return quoteIfPresent(value)
}

func quoteIfPresent(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return fmt.Sprintf("%q", trimmed)
}

func stringField(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func nestedStringField(payload map[string]any, key string, nested string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok {
		return ""
	}
	nestedPayload, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	return stringField(nestedPayload, nested)
}

func stringOrNumberField(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	switch value := payload[key].(type) {
	case string:
		return strings.TrimSpace(value)
	case fmt.Stringer:
		return value.String()
	case int:
		return fmt.Sprintf("%d", value)
	case int32:
		return fmt.Sprintf("%d", value)
	case int64:
		return fmt.Sprintf("%d", value)
	case float64:
		return fmt.Sprintf("%.0f", value)
	default:
		return ""
	}
}

func arrayField(payload map[string]any, key string) []any {
	if payload == nil {
		return nil
	}
	if value, ok := payload[key].([]any); ok {
		return value
	}
	return nil
}

func arrayLen(value any) int {
	if values, ok := value.([]any); ok {
		return len(values)
	}
	return 0
}
