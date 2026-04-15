package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corca-ai/sah-cli/internal/sah"
)

func stubServerNavigation(t *testing.T) {
	t.Helper()

	original := serverNavigationResolver
	serverNavigationResolver = func(
		state cliState,
		commandKey string,
		err error,
	) (*sah.ServiceDocument, []commandSuggestion) {
		return nil, fallbackSuggestions(state, commandKey, err)
	}
	t.Cleanup(func() {
		serverNavigationResolver = original
	})
}

func TestPrintEntryExperienceForSignedOutState(t *testing.T) {
	stubServerNavigation(t)

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

func TestPrintHelpShowsTopicGuideAndSuggestions(t *testing.T) {
	stubServerNavigation(t)

	state := cliState{
		AuthConfigured:   true,
		DetectedAgents:   []string{"codex"},
		HasDetectedAgent: true,
		DaemonSupported:  true,
		Stage:            stageReadyToRun,
	}

	var buffer bytes.Buffer
	printHelp(&buffer, "auth login", state)
	output := buffer.String()

	for _, snippet := range []string{
		"Help: auth",
		"sah auth login",
		"Current status",
		"`sah daemon install`",
	} {
		if !strings.Contains(output, snippet) {
			t.Fatalf("expected output to contain %q, got:\n%s", snippet, output)
		}
	}
}

func TestSuggestionsForReadyToRunState(t *testing.T) {
	stubServerNavigation(t)

	state := cliState{
		AuthConfigured:   true,
		DetectedAgents:   []string{"codex"},
		HasDetectedAgent: true,
		DaemonSupported:  true,
		Stage:            stageReadyToRun,
	}

	suggestions := suggestionsForContext(state, "", nil)
	got := suggestionCommands(suggestions)

	for _, want := range []string{"sah daemon install", "sah run", "sah me"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected suggestions to contain %q, got %q", want, got)
		}
	}
}

func TestSuggestionsForDaemonRunningState(t *testing.T) {
	stubServerNavigation(t)

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
	stubServerNavigation(t)

	state := cliState{Stage: stageSignedOut}

	suggestions := suggestionsForContext(
		state,
		"me",
		fmt.Errorf("not authenticated; run `sah auth login` first"),
	)
	got := suggestionCommands(suggestions)

	for _, want := range []string{"sah auth login", "sah auth status", "sah agents"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected suggestions to contain %q, got %q", want, got)
		}
	}
}

func TestSuggestionsForRejectedStoredCredentialError(t *testing.T) {
	stubServerNavigation(t)

	state := cliState{Stage: stageSignedOut}

	suggestions := suggestionsForContext(
		state,
		"me",
		fmt.Errorf("stored credential rejected; run `sah auth login` again"),
	)
	got := suggestionCommands(suggestions)

	for _, want := range []string{"sah auth login", "sah auth status", "sah agents"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected suggestions to contain %q, got %q", want, got)
		}
	}
}

func TestSuggestionsForUnauthorizedStatusError(t *testing.T) {
	stubServerNavigation(t)

	state := cliState{Stage: stageSignedOut}

	suggestions := suggestionsForContext(
		state,
		"me",
		&sah.StatusError{StatusCode: 401, Message: "Expired service token"},
	)
	got := suggestionCommands(suggestions)

	for _, want := range []string{"sah auth login", "sah auth status", "sah agents"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected suggestions to contain %q, got %q", want, got)
		}
	}
}

func TestSuggestionsForInvalidRefreshTokenError(t *testing.T) {
	stubServerNavigation(t)

	state := cliState{Stage: stageSignedOut}

	suggestions := suggestionsForContext(
		state,
		"me",
		&sah.StatusError{StatusCode: 400, ErrorCode: "invalid_grant", Message: "Refresh token is invalid"},
	)
	got := suggestionCommands(suggestions)

	for _, want := range []string{"sah auth login", "sah auth status", "sah agents"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected suggestions to contain %q, got %q", want, got)
		}
	}
}

func TestSuggestionsForWorkerContractError(t *testing.T) {
	stubServerNavigation(t)

	state := cliState{Stage: stageReadyToRun, DaemonSupported: true}

	suggestions := suggestionsForContext(
		state,
		"run",
		&sah.WorkerContractViolation{
			RequiredTaskProtocolVersion:   "2026-04-20",
			AdvertisedTaskProtocolVersion: sah.SupportedTaskProtocol,
		},
	)
	got := suggestionCommands(suggestions)

	for _, want := range []string{"sah daemon install", "sah run", "sah me"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected suggestions to contain %q, got %q", want, got)
		}
	}
}

func TestPrintUnknownCommandShowsCatalog(t *testing.T) {
	stubServerNavigation(t)

	state := cliState{Stage: stageSignedOut}

	var buffer bytes.Buffer
	printUnknownCommand(&buffer, "autorun", state)
	output := buffer.String()

	for _, snippet := range []string{
		`unknown command "autorun"`,
		"sah auth login",
		"sah run",
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

func TestDocumentActionsFallbackIncludesRemoteReadCommands(t *testing.T) {
	actions := documentActions(nil)
	commands := make([]string, 0, len(actions))
	for _, action := range actions {
		commands = append(commands, action.Command)
	}
	got := strings.Join(commands, ", ")

	for _, want := range []string{"me", "contributions", "leaderboard"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected fallback catalog to contain %q, got %q", want, got)
		}
	}
}

func TestDocumentActionsMergeLocalBuiltinsIntoPartialServiceDocument(t *testing.T) {
	actions := documentActions(&sah.ServiceDocument{
		Actions: []sah.CommandAction{
			{Command: "me", Description: "Server-provided account command."},
		},
	})

	commands := make([]string, 0, len(actions))
	for _, action := range actions {
		commands = append(commands, action.Command)
	}
	got := strings.Join(commands, ", ")

	for _, want := range []string{"me", "auth login", "auth logout", "agents", "version"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected merged command catalog to contain %q, got %q", want, got)
		}
	}
}

func TestResolveServerNavigationFallsBackWhenServerReturnsNoActions(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/s@h":
			_, _ = writer.Write([]byte(`{
				"title": "SCIENCE@home CLI",
				"description": "Server document",
				"actions": [{"command":"me","description":"My account"}]
			}`))
		case "/s@h/navigation":
			_, _ = writer.Write([]byte(`{
				"title": "Try next",
				"description": "No recommendations right now",
				"actions": []
			}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	document, suggestions := resolveServerNavigation(
		cliState{
			BaseURL:         server.URL,
			DaemonSupported: true,
			Stage:           stageSignedOut,
		},
		"",
		nil,
	)

	if document == nil {
		t.Fatal("expected service document")
	}
	if got := suggestionCommands(suggestions); !strings.Contains(got, "sah auth login") {
		t.Fatalf("expected fallback suggestions when navigation is empty, got %q", got)
	}
}

func TestSuggestionsForReadyToRunStateWithoutDaemonSupport(t *testing.T) {
	stubServerNavigation(t)

	state := cliState{
		AuthConfigured:   true,
		DetectedAgents:   []string{"codex"},
		HasDetectedAgent: true,
		DaemonSupported:  false,
		Stage:            stageReadyToRun,
	}

	suggestions := suggestionsForContext(state, "", nil)
	got := suggestionCommands(suggestions)

	for _, want := range []string{"sah run", "sah me", "sah agents"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected suggestions to contain %q, got %q", want, got)
		}
	}
	if strings.Contains(got, "sah daemon install") {
		t.Fatalf("did not expect daemon suggestion on unsupported platform, got %q", got)
	}
}

func TestCanonicalCommandKey(t *testing.T) {
	testCases := []struct {
		name string
		args []string
		want string
	}{
		{name: "empty", args: nil, want: ""},
		{name: "version alias", args: []string{"--version"}, want: "version"},
		{name: "auth subcommand", args: []string{"auth", "login"}, want: "auth login"},
		{name: "auth flag only", args: []string{"auth", "--base-url", "http://localhost:8000"}, want: "auth"},
		{name: "daemon subcommand", args: []string{"daemon", "status"}, want: "daemon status"},
		{name: "run", args: []string{"run", "--once"}, want: "run"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := canonicalCommandKey(testCase.args); got != testCase.want {
				t.Fatalf("expected %q, got %q", testCase.want, got)
			}
		})
	}
}

func TestPrintStateSummaryIncludesUpdateLine(t *testing.T) {
	state := cliState{
		BaseURL: "https://sah.borca.ai",
		ReleaseStatus: &sah.ClientReleaseStatus{
			CurrentVersion:     "v0.7.0",
			LatestVersion:      "v0.8.0",
			RecommendedVersion: "v0.8.0",
			UpdateAvailable:    true,
		},
	}

	var buffer bytes.Buffer
	printStateSummary(&buffer, state)
	output := buffer.String()

	for _, snippet := range []string{
		"- Base URL: https://sah.borca.ai",
		"- Update: available: v0.8.0 (current v0.7.0)",
	} {
		if !strings.Contains(output, snippet) {
			t.Fatalf("expected output to contain %q, got:\n%s", snippet, output)
		}
	}
}

func TestReleaseTargetVersionFallsBackToLatest(t *testing.T) {
	status := &sah.ClientReleaseStatus{
		LatestVersion: "v0.8.0",
	}
	if got := releaseTargetVersion(status); got != "v0.8.0" {
		t.Fatalf("expected latest version fallback, got %q", got)
	}
}

func suggestionCommands(suggestions []commandSuggestion) string {
	commands := make([]string, 0, len(suggestions))
	for _, suggestion := range suggestions {
		commands = append(commands, suggestion.Command)
	}
	return strings.Join(commands, ", ")
}
