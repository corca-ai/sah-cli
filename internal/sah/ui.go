package sah

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const (
	ansiReset = "\x1b[0m"
	ansiRed   = "\x1b[31m"
)

func PrintRunBanner(writer io.Writer) {
	if writer == nil {
		return
	}

	at := "@"
	if useColor() {
		at = ansiRed + "@" + ansiReset
	}

	_, _ = fmt.Fprintf(writer, "SCIENCE%shome\n\n", at)
}

func PrintRunPlan(writer io.Writer, config Config, options WorkerOptions, pool []AgentSpec) {
	if writer == nil {
		return
	}

	names := make([]string, 0, len(pool))
	for _, agent := range pool {
		names = append(names, agent.Name)
	}

	_, _ = fmt.Fprintf(writer, "Base URL:   %s\n", config.BaseURL)
	_, _ = fmt.Fprintf(writer, "Agents:     %s\n", strings.Join(names, " -> "))
	if model := strings.TrimSpace(options.Model); model != "" {
		_, _ = fmt.Fprintf(writer, "Model:      %s\n", model)
	}
	if models := FormatAgentModels(options.Models); models != "" {
		_, _ = fmt.Fprintf(writer, "Per-agent:  %s\n", models)
	}
	_, _ = fmt.Fprintf(writer, "Interval:   %s\n", options.Interval)
	_, _ = fmt.Fprintf(writer, "Timeout:    %s\n", options.Timeout)
	if options.TaskType != "" {
		_, _ = fmt.Fprintf(writer, "Task type:  %s\n", options.TaskType)
	}
	if options.Once {
		_, _ = fmt.Fprintln(writer, "Mode:       single cycle")
	} else {
		_, _ = fmt.Fprintln(writer, "Mode:       continuous")
	}
	_, _ = fmt.Fprintln(writer)
}

func PrintCycleSummary(
	writer io.Writer,
	assignment Assignment,
	result *AgentResult,
	response *SubmitContributionResponse,
) {
	if writer == nil || result == nil || response == nil {
		return
	}

	_, _ = fmt.Fprintln(writer, strings.Repeat("=", 72))
	_, _ = fmt.Fprintf(writer, "Assignment   #%d  %s / %s\n", assignment.AssignmentID, assignment.TaskType, assignment.TaskKey)
	if result.Model != "" {
		_, _ = fmt.Fprintf(writer, "Agent        %s (%s)\n", result.Agent.Name, result.Model)
	} else {
		_, _ = fmt.Fprintf(writer, "Agent        %s\n", result.Agent.Name)
	}
	_, _ = fmt.Fprintf(writer, "Runtime      %s\n", humanDuration(result.Duration))
	_, _ = fmt.Fprintf(writer, "Tokens       %s\n", formatTokenUsage(result.Usage))
	_, _ = fmt.Fprintf(writer, "Contribution %s\n", SummarizeContribution(assignment, result.Payload))
	_, _ = fmt.Fprintf(
		writer,
		"Submitted    contribution #%d, pending credits %d\n",
		response.ContributionID,
		response.PendingCredits,
	)
	_, _ = fmt.Fprintln(writer, strings.Repeat("=", 72))
}

func formatTokenUsage(usage TokenUsage) string {
	if !usage.Available {
		return "unavailable"
	}

	parts := []string{
		fmt.Sprintf("%s in", formatInt(usage.InputTokens)),
		fmt.Sprintf("%s out", formatInt(usage.OutputTokens)),
	}
	if usage.TotalTokens > 0 {
		parts = append(parts, fmt.Sprintf("%s total", formatInt(usage.TotalTokens)))
	}
	if usage.CachedTokens > 0 {
		parts = append(parts, fmt.Sprintf("%s cached", formatInt(usage.CachedTokens)))
	}
	return strings.Join(parts, " / ")
}

func formatInt(value int64) string {
	raw := fmt.Sprintf("%d", value)
	if len(raw) <= 3 {
		return raw
	}

	var parts []string
	for len(raw) > 3 {
		parts = append([]string{raw[len(raw)-3:]}, parts...)
		raw = raw[:len(raw)-3]
	}
	parts = append([]string{raw}, parts...)
	return strings.Join(parts, ",")
}

func humanDuration(value time.Duration) string {
	if value < time.Second {
		return value.Round(10 * time.Millisecond).String()
	}
	return value.Round(100 * time.Millisecond).String()
}

func useColor() bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return true
}
