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

	fmt.Fprintf(writer, "%s\n", strings.TrimSpace(fmt.Sprintf(`
  ____   ____ ___ _____ _   _  ____ _____
 / ___| / ___|_ _| ____| \\ | |/ ___| ____|
 \\___ \\| |    | ||  _| |  \\| | |   |  _|
  ___) | |___ | || |___| |\\  | |___| |___
 |____/ \\____|___|_____|_| \\_|\\____|_____|

                %shome
`, at)))
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "  contributor loop warming up...")
	fmt.Fprintln(writer)
}

func PrintRunPlan(writer io.Writer, config Config, options WorkerOptions, pool []AgentSpec) {
	if writer == nil {
		return
	}

	names := make([]string, 0, len(pool))
	for _, agent := range pool {
		names = append(names, agent.Name)
	}

	fmt.Fprintf(writer, "Base URL:   %s\n", config.BaseURL)
	fmt.Fprintf(writer, "Agents:     %s\n", strings.Join(names, " -> "))
	if model := strings.TrimSpace(options.Model); model != "" {
		fmt.Fprintf(writer, "Model:      %s\n", model)
	}
	if models := FormatAgentModels(options.Models); models != "" {
		fmt.Fprintf(writer, "Per-agent:  %s\n", models)
	}
	fmt.Fprintf(writer, "Interval:   %s\n", options.Interval)
	fmt.Fprintf(writer, "Timeout:    %s\n", options.Timeout)
	if options.TaskType != "" {
		fmt.Fprintf(writer, "Task type:  %s\n", options.TaskType)
	}
	if options.Once {
		fmt.Fprintln(writer, "Mode:       single cycle")
	} else {
		fmt.Fprintln(writer, "Mode:       continuous")
	}
	fmt.Fprintln(writer)
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

	fmt.Fprintln(writer, strings.Repeat("=", 72))
	fmt.Fprintf(writer, "Assignment   #%d  %s / %s\n", assignment.AssignmentID, assignment.TaskType, assignment.TaskKey)
	if result.Model != "" {
		fmt.Fprintf(writer, "Agent        %s (%s)\n", result.Agent.Name, result.Model)
	} else {
		fmt.Fprintf(writer, "Agent        %s\n", result.Agent.Name)
	}
	fmt.Fprintf(writer, "Runtime      %s\n", humanDuration(result.Duration))
	fmt.Fprintf(writer, "Tokens       %s\n", formatTokenUsage(result.Usage))
	fmt.Fprintf(writer, "Contribution %s\n", SummarizeContribution(assignment, result.Payload))
	fmt.Fprintf(
		writer,
		"Submitted    contribution #%d, pending credits %d\n",
		response.ContributionID,
		response.PendingCredits,
	)
	fmt.Fprintln(writer, strings.Repeat("=", 72))
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
