package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/corca-ai/sah-cli/internal/sah"
)

func TestPrintEntryExperienceForSignedOutState(t *testing.T) {
	state := cliState{
		BaseURL:         "https://sah.borca.ai",
		DaemonSupported: true,
		Stage:           stageSignedOut,
	}

	var buffer bytes.Buffer
	printEntryExperience(&buffer, state)
	output := buffer.String()

	for _, snippet := range []string{
		"SCIENCE@home CLI",
		"For the full command guide, run `sah help`.",
		"- Sign-in: not connected",
		"`sah auth login`",
	} {
		if !strings.Contains(output, snippet) {
			t.Fatalf("expected output to contain %q, got:\n%s", snippet, output)
		}
	}
}

func TestSuggestionsForReadyToRunState(t *testing.T) {
	state := cliState{
		AuthConfigured:   true,
		DetectedAgents:   []string{"codex"},
		HasDetectedAgent: true,
		DaemonSupported:  true,
		Stage:            stageReadyToRun,
	}

	suggestions := suggestionsForContext(state, "", nil)
	got := suggestionCommands(suggestions)

	for _, want := range []string{"sah daemon install", "sah run --once", "sah me"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected suggestions to contain %q, got %q", want, got)
		}
	}
}

func TestSuggestionsForDaemonRunningState(t *testing.T) {
	state := cliState{
		AuthConfigured:   true,
		DetectedAgents:   []string{"codex"},
		HasDetectedAgent: true,
		DaemonSupported:  true,
		DaemonInstalled:  true,
		DaemonRunning:    true,
		Stage:            stageDaemonRunning,
	}

	suggestions := suggestionsForContext(state, "daemon status", nil)
	got := suggestionCommands(suggestions)

	for _, want := range []string{"sah contributions", "sah me", "sah daemon status"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected suggestions to contain %q, got %q", want, got)
		}
	}
}

func TestSuggestionsForAuthenticationError(t *testing.T) {
	state := cliState{Stage: stageSignedOut}

	suggestions := suggestionsForContext(
		state,
		"me",
		fmt.Errorf("not authenticated; run `sah auth login` first"),
	)
	got := suggestionCommands(suggestions)

	for _, want := range []string{"sah auth login", "sah help auth", "sah agents"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected suggestions to contain %q, got %q", want, got)
		}
	}
}

func TestSuggestionsForWorkerContractError(t *testing.T) {
	state := cliState{Stage: stageReadyToRun}

	suggestions := suggestionsForContext(
		state,
		"run",
		&sah.WorkerContractViolation{
			RequiredTaskProtocolVersion:   "2026-04-20",
			AdvertisedTaskProtocolVersion: sah.SupportedTaskProtocol,
		},
	)
	got := suggestionCommands(suggestions)

	for _, want := range []string{"sah upgrade", "sah help upgrade", "sah auth status"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected suggestions to contain %q, got %q", want, got)
		}
	}
}

func TestPrintUnknownCommandShowsCatalog(t *testing.T) {
	state := cliState{Stage: stageSignedOut}

	var buffer bytes.Buffer
	printUnknownCommand(&buffer, "autorun", state)
	output := buffer.String()

	for _, snippet := range []string{
		`unknown command "autorun"`,
		"sah auth <login|logout|status>",
		"sah run [flags]",
		"`sah auth login`",
	} {
		if !strings.Contains(output, snippet) {
			t.Fatalf("expected output to contain %q, got:\n%s", snippet, output)
		}
	}
}

func TestNormalizeHelpTopicFallsBackToParentCommand(t *testing.T) {
	if got := normalizeHelpTopic("auth login"); got != "auth" {
		t.Fatalf("expected auth topic, got %q", got)
	}
}

func suggestionCommands(suggestions []commandSuggestion) string {
	commands := make([]string, 0, len(suggestions))
	for _, suggestion := range suggestions {
		commands = append(commands, suggestion.Command)
	}
	return strings.Join(commands, ", ")
}
