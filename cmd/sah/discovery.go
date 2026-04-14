package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/corca-ai/sah-cli/internal/sah"
)

type journeyStage string

const (
	stageSignedOut     journeyStage = "signed_out"
	stageNeedsAgent    journeyStage = "needs_agent"
	stageReadyToRun    journeyStage = "ready_to_run"
	stageDaemonStopped journeyStage = "daemon_stopped"
	stageDaemonRunning journeyStage = "daemon_running"
)

type cliState struct {
	BaseURL          string
	AuthConfigured   bool
	AgentStatuses    []sah.AgentStatus
	DetectedAgents   []string
	HasDetectedAgent bool
	DaemonSupported  bool
	DaemonInstalled  bool
	DaemonRunning    bool
	DaemonStatus     string
	ReleaseStatus    *sah.ClientReleaseStatus
	LoadError        error
	ServiceError     error
	ReleaseError     error
	Stage            journeyStage
}

type commandSuggestion struct {
	Command string
	Why     string
}

type commandGuide struct {
	Topic    string
	Usage    string
	Summary  string
	Details  []string
	Examples []string
}

const (
	cliIntroTitle   = "SCIENCE@home CLI"
	cliIntroSummary = "`sah` links this machine to SCIENCE@home. It signs you in, claims coding tasks, runs a local agent CLI, and submits the result for credit."
	maxSuggestions  = 3
)

var topLevelGuides = []commandGuide{
	{
		Topic:   "help",
		Usage:   "sah help [command]",
		Summary: "Show the command guide or help for one command",
	},
	{
		Topic:   "auth",
		Usage:   "sah auth <login|logout|status>",
		Summary: "Sign in, sign out, or inspect local auth state",
	},
	{
		Topic:   "run",
		Usage:   "sah run [flags]",
		Summary: "Claim tasks and work in the foreground",
	},
	{
		Topic:   "daemon",
		Usage:   "sah daemon <install|start|stop|status|uninstall>",
		Summary: "Manage the background worker service",
	},
	{
		Topic:   "me",
		Usage:   "sah me",
		Summary: "Show your SCIENCE@home account summary",
	},
	{
		Topic:   "contributions",
		Usage:   "sah contributions",
		Summary: "Show recent submissions and reviews",
	},
	{
		Topic:   "leaderboard",
		Usage:   "sah leaderboard",
		Summary: "Show public rankings",
	},
	{
		Topic:   "agents",
		Usage:   "sah agents",
		Summary: "Show which supported local agent CLIs this machine can use",
	},
	{
		Topic:   "version",
		Usage:   "sah version",
		Summary: "Print the build version",
	},
	{
		Topic:   "upgrade",
		Usage:   "sah upgrade",
		Summary: "Upgrade the CLI when a newer release is available",
	},
}

var guideByTopic = map[string]commandGuide{
	"help": {
		Topic:   "help",
		Usage:   "sah help [command]",
		Summary: "Show the command guide or help for one command",
		Details: []string{
			"Run `sah help` for the full command catalog.",
			"Run `sah help auth`, `sah help daemon`, or another command name for focused help.",
		},
		Examples: []string{
			"sah help",
			"sah help auth",
			"sah help daemon",
		},
	},
	"auth": {
		Topic:   "auth",
		Usage:   "sah auth <login|logout|status>",
		Summary: "Sign in, sign out, or inspect local auth state",
		Details: []string{
			"`sah auth login` opens a browser, completes SCIENCE@home sign-in, and stores the API key locally.",
			"`sah auth status` checks whether this machine has a stored API key and whether the server accepts it.",
			"`sah auth logout` removes the locally stored API key from this machine.",
		},
		Examples: []string{
			"sah auth login",
			"sah auth status",
			"sah auth logout",
		},
	},
	"run": {
		Topic:   "run",
		Usage:   "sah run [flags]",
		Summary: "Claim tasks and work in the foreground",
		Details: []string{
			"Use `sah run --once` to claim one assignment, process it, and exit.",
			"Use `sah run` to stay in a foreground worker loop.",
			"Use `--agent`, `--agents`, or `--rotate-installed` to choose which local agent CLI should handle assignments.",
		},
		Examples: []string{
			"sah run --once",
			"sah run --rotate-installed",
			"sah run --agents codex,claude --models codex=gpt-5.4-mini,claude=sonnet",
		},
	},
	"daemon": {
		Topic:   "daemon",
		Usage:   "sah daemon <install|start|stop|status|uninstall>",
		Summary: "Manage the background worker service",
		Details: []string{
			"`sah daemon install` writes the per-user service definition and starts it immediately.",
			"`sah daemon start` and `sah daemon stop` control the existing background service.",
			"`sah daemon status` shows whether the background worker is installed and running.",
			"`sah daemon uninstall` removes the service definition from this machine.",
		},
		Examples: []string{
			"sah daemon install",
			"sah daemon status",
			"sah daemon stop",
		},
	},
	"me": {
		Topic:   "me",
		Usage:   "sah me",
		Summary: "Show your SCIENCE@home account summary",
		Details: []string{
			"Shows the connected account, credits, pending credits, trust, and rank.",
		},
		Examples: []string{
			"sah me",
		},
	},
	"contributions": {
		Topic:   "contributions",
		Usage:   "sah contributions",
		Summary: "Show recent submissions and reviews",
		Details: []string{
			"Lists recent submission and review history for the connected account.",
		},
		Examples: []string{
			"sah contributions",
			"sah contributions --limit 20",
		},
	},
	"leaderboard": {
		Topic:   "leaderboard",
		Usage:   "sah leaderboard",
		Summary: "Show public rankings",
		Details: []string{
			"Shows weekly, monthly, or all-time public rankings.",
		},
		Examples: []string{
			"sah leaderboard",
			"sah leaderboard --window weekly",
		},
	},
	"agents": {
		Topic:   "agents",
		Usage:   "sah agents",
		Summary: "Show which supported local agent CLIs this machine can use",
		Details: []string{
			"Lists supported agent CLIs (`codex`, `gemini`, `claude`, `qwen`) and whether `sah` can find them on this machine.",
		},
		Examples: []string{
			"sah agents",
		},
	},
	"version": {
		Topic:   "version",
		Usage:   "sah version",
		Summary: "Print the build version",
		Details: []string{
			"Shows the installed `sah` build version.",
		},
		Examples: []string{
			"sah version",
		},
	},
	"upgrade": {
		Topic:   "upgrade",
		Usage:   "sah upgrade",
		Summary: "Upgrade the CLI when a newer release is available",
		Details: []string{
			"Uses the local installation method to upgrade `sah` when possible.",
			"Homebrew installs are upgraded with `brew upgrade corca-ai/tap/sah-cli`.",
			"If this machine uses another install method, the command explains the next manual step.",
		},
		Examples: []string{
			"sah upgrade",
		},
	},
}

func inspectCLIState() cliState {
	paths, config, err := loadConfig()
	if err != nil {
		state := cliState{
			BaseURL:         sah.DefaultBaseURL,
			DaemonSupported: daemonSupported(),
			LoadError:       err,
			Stage:           stageSignedOut,
		}
		state.ReleaseStatus, state.ReleaseError = resolveReleaseStatus(sah.Paths{}, state.BaseURL)
		return state
	}
	return inspectCLIStateWith(paths, config, sah.InstalledAgents())
}

func inspectCLIStateWith(paths sah.Paths, config sah.Config, agentStatuses []sah.AgentStatus) cliState {
	state := cliState{
		BaseURL:         sciHomeURL(config.BaseURL),
		AuthConfigured:  strings.TrimSpace(config.APIKey) != "",
		AgentStatuses:   append([]sah.AgentStatus(nil), agentStatuses...),
		DaemonSupported: daemonSupported(),
	}
	state.DetectedAgents = detectedAgentNames(agentStatuses)
	state.HasDetectedAgent = len(state.DetectedAgents) > 0

	servicePath := strings.TrimSpace(sah.ServiceDefinitionPath(paths))
	if state.DaemonSupported && servicePath != "" {
		if _, err := os.Stat(servicePath); err == nil {
			state.DaemonInstalled = true
		} else if !os.IsNotExist(err) {
			state.ServiceError = err
		}
	}

	switch {
	case !state.DaemonSupported:
		state.DaemonStatus = "unsupported"
	case !state.DaemonInstalled:
		state.DaemonStatus = "not installed"
	default:
		loaded, detail, err := sah.ServiceStatus(paths)
		if err != nil {
			state.ServiceError = err
			state.DaemonStatus = "unknown"
			break
		}
		state.DaemonRunning = loaded
		state.DaemonStatus = strings.TrimSpace(detail)
		if state.DaemonStatus == "" {
			if loaded {
				state.DaemonStatus = "running"
			} else {
				state.DaemonStatus = "stopped"
			}
		}
	}

	state.Stage = deriveJourneyStage(state)
	state.ReleaseStatus, state.ReleaseError = resolveReleaseStatus(paths, state.BaseURL)
	return state
}

func detectedAgentNames(statuses []sah.AgentStatus) []string {
	names := make([]string, 0, len(statuses))
	for _, status := range statuses {
		if status.Installed {
			names = append(names, status.Name)
		}
	}
	sort.Strings(names)
	return names
}

func deriveJourneyStage(state cliState) journeyStage {
	switch {
	case !state.AuthConfigured:
		return stageSignedOut
	case state.DaemonInstalled && state.DaemonRunning:
		return stageDaemonRunning
	case state.DaemonInstalled:
		return stageDaemonStopped
	case !state.HasDetectedAgent:
		return stageNeedsAgent
	default:
		return stageReadyToRun
	}
}

func daemonSupported() bool {
	switch runtime.GOOS {
	case "darwin", "linux":
		return true
	default:
		return false
	}
}

func printEntryExperience(writer io.Writer, state cliState) {
	if writer == nil {
		return
	}

	printCLIIntro(writer, cliIntroTitle)
	_, _ = fmt.Fprintln(writer, "For the full command guide, run `sah help`.")
	printStateSummary(writer, state)
	printSuggestionSection(writer, "Try next", suggestionsForContext(state, "", nil))
}

func printHelp(writer io.Writer, topic string, state cliState) {
	if writer == nil {
		return
	}

	normalized := normalizeHelpTopic(topic)
	if guide, ok := guideForTopic(normalized); ok {
		printGuide(writer, guide)
		printStateSummary(writer, state)
		printSuggestionSection(writer, "Try next", suggestionsForContext(state, normalized, nil))
		return
	}

	printCLIIntro(writer, "SCIENCE@home CLI guide")
	printCommandCatalog(writer)
	printStateSummary(writer, state)
	printSuggestionSection(writer, "Try next", suggestionsForContext(state, "help", nil))
}

func printCLIIntro(writer io.Writer, title string) {
	if writer == nil {
		return
	}

	_, _ = fmt.Fprintln(writer, title)
	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, cliIntroSummary)
	_, _ = fmt.Fprintln(writer)
}

func printGuide(writer io.Writer, guide commandGuide) {
	_, _ = fmt.Fprintf(writer, "Help: %s\n\n", guide.Topic)
	_, _ = fmt.Fprintf(writer, "Usage: %s\n", guide.Usage)
	_, _ = fmt.Fprintf(writer, "%s\n", guide.Summary)
	if len(guide.Details) > 0 {
		_, _ = fmt.Fprintln(writer)
		_, _ = fmt.Fprintln(writer, "Details")
		for _, line := range guide.Details {
			_, _ = fmt.Fprintf(writer, "- %s\n", line)
		}
	}
	if len(guide.Examples) > 0 {
		_, _ = fmt.Fprintln(writer)
		_, _ = fmt.Fprintln(writer, "Examples")
		for _, example := range guide.Examples {
			_, _ = fmt.Fprintf(writer, "- `%s`\n", example)
		}
	}
}

func printCommandCatalog(writer io.Writer) {
	_, _ = fmt.Fprintln(writer, "Commands")
	tw := tabwriter.NewWriter(writer, 0, 0, 2, ' ', 0)
	for _, guide := range topLevelGuides {
		_, _ = fmt.Fprintf(tw, "  %s\t%s\n", guide.Usage, guide.Summary)
	}
	_ = tw.Flush()
}

func printUnknownCommand(writer io.Writer, raw string, state cliState) {
	if writer == nil {
		return
	}

	_, _ = fmt.Fprintf(writer, "sah: unknown command %q\n\n", raw)
	_, _ = fmt.Fprintln(writer, "Run `sah help` for the full command guide.")
	_, _ = fmt.Fprintln(writer)
	printCommandCatalog(writer)
	printSuggestionSection(writer, "Try next", suggestionsForContext(state, "", fmt.Errorf("unknown command")))
}

func printUnknownSubcommand(writer io.Writer, parent string, raw string, state cliState) {
	if writer == nil {
		return
	}

	_, _ = fmt.Fprintf(writer, "sah: unknown %s command %q\n\n", parent, raw)
	printHelp(writer, parent, state)
}

func printCommandFailure(writer io.Writer, state cliState, commandKey string, err error) {
	if writer == nil || err == nil {
		return
	}

	_, _ = fmt.Fprintf(writer, "sah: %v\n", err)
	printSuggestionSection(writer, "Try next", suggestionsForContext(state, commandKey, err))
}

func printCommandSuccessHints(writer io.Writer, state cliState, commandKey string) {
	if writer == nil {
		return
	}
	printSuggestionSection(writer, "Next commands", suggestionsForContext(state, commandKey, nil))
}

func printStateSummary(writer io.Writer, state cliState) {
	if writer == nil {
		return
	}

	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, "Current status")
	if state.LoadError != nil {
		_, _ = fmt.Fprintf(writer, "- Local state: unavailable (%v)\n", state.LoadError)
		return
	}

	_, _ = fmt.Fprintf(writer, "- Sign-in: %s\n", authSummary(state))
	_, _ = fmt.Fprintf(writer, "- Agent CLIs: %s\n", agentSummary(state))
	if state.DaemonSupported {
		_, _ = fmt.Fprintf(writer, "- Background worker: %s\n", daemonSummary(state))
	} else {
		_, _ = fmt.Fprintln(writer, "- Background worker: unsupported on this platform")
	}
	_, _ = fmt.Fprintf(writer, "- Base URL: %s\n", state.BaseURL)
	if summary := releaseSummary(state); summary != "" {
		_, _ = fmt.Fprintf(writer, "- Update: %s\n", summary)
	}
}

func authSummary(state cliState) string {
	if state.AuthConfigured {
		return "connected on this machine"
	}
	return "not connected"
}

func agentSummary(state cliState) string {
	if len(state.DetectedAgents) == 0 {
		return "none detected"
	}
	return strings.Join(state.DetectedAgents, ", ")
}

func daemonSummary(state cliState) string {
	switch {
	case !state.DaemonInstalled:
		return "not installed"
	case state.DaemonRunning:
		return fmt.Sprintf("running via %s", sah.ServiceManagerName())
	default:
		if strings.TrimSpace(state.DaemonStatus) != "" {
			return fmt.Sprintf("installed but not running (%s)", state.DaemonStatus)
		}
		return "installed but not running"
	}
}

func releaseSummary(state cliState) string {
	status := state.ReleaseStatus
	if status == nil {
		return ""
	}
	if status.UpdateAvailable {
		target := releaseTargetVersion(status)
		if target == "" {
			return ""
		}
		return fmt.Sprintf("available: %s (current %s)", target, displayVersion(status.CurrentVersion))
	}
	return ""
}

func printSuggestionSection(writer io.Writer, title string, suggestions []commandSuggestion) {
	if writer == nil || len(suggestions) == 0 {
		return
	}

	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintf(writer, "%s\n", title)
	tw := tabwriter.NewWriter(writer, 0, 0, 2, ' ', 0)
	for _, suggestion := range suggestions {
		_, _ = fmt.Fprintf(tw, "- `%s`\t%s\n", suggestion.Command, suggestion.Why)
	}
	_ = tw.Flush()
}

func suggestionsForContext(state cliState, commandKey string, err error) []commandSuggestion {
	list := make([]commandSuggestion, 0, 4)
	seen := map[string]struct{}{}
	add := func(command string, why string) {
		command = strings.TrimSpace(command)
		why = strings.TrimSpace(why)
		if command == "" || why == "" {
			return
		}
		if _, ok := seen[command]; ok {
			return
		}
		seen[command] = struct{}{}
		list = append(list, commandSuggestion{Command: command, Why: why})
	}

	if state.ReleaseStatus != nil {
		if state.ReleaseStatus.UpdateAvailable {
			add("sah upgrade", upgradeWhy(state.ReleaseStatus))
		}
	}

	if handled := addErrorSuggestions(add, state, err); handled {
		return limitSuggestions(list)
	}

	addCommandSuggestions(add, state, commandKey)
	return limitSuggestions(list)
}

func addErrorSuggestions(add func(command string, why string), state cliState, err error) bool {
	switch {
	case isNotAuthenticatedError(err):
		add("sah auth login", "Sign in from your browser and link this machine.")
		add("sah help auth", "See the authentication commands and what each one does.")
		add("sah agents", "Inspect which supported local agent CLIs `sah` can use here.")
		return true
	case sah.IsWorkerContractError(err):
		add("sah upgrade", "Install a CLI release that supports the current SCIENCE@home worker contract.")
		add("sah help upgrade", "See how CLI upgrades are handled on this machine.")
		add("sah auth status", "Confirm this machine is still connected after the upgrade.")
		return true
	case sah.IsNoSupportedAgentCLI(err):
		add("sah agents", "See which supported local agent CLIs are installed and which ones are missing.")
		if state.AuthConfigured {
			add("sah auth status", "Confirm this machine is already connected before installing a worker.")
		} else {
			add("sah auth login", "Sign in first so this machine is ready once an agent CLI is available.")
		}
		add("sah help run", "See how foreground and background execution work.")
		return true
	default:
		return false
	}
}

func addCommandSuggestions(add func(command string, why string), state cliState, commandKey string) {
	switch commandKey {
	case "auth login":
		addPostLoginSuggestions(add, state)
	case "auth logout":
		add("sah auth login", "Reconnect this machine to SCIENCE@home.")
		add("sah help auth", "Review the authentication commands.")
	case "auth", "auth status", "daemon", "daemon status", "help", "version", "":
		addPhaseSuggestions(add, state)
	case "run":
		addRunSuggestions(add, state)
	case "daemon install", "daemon start":
		add("sah daemon status", "Verify that the background service is installed and running.")
		add("sah contributions", "See new submissions and reviews after the worker starts running.")
		add("sah me", "Check credits, pending credits, trust, and rank.")
	case "daemon stop":
		add("sah daemon start", "Start the background worker again.")
		add("sah daemon status", "Check whether the service is still installed.")
	case "me":
		add("sah contributions", "See the recent work behind the numbers in your account summary.")
		add("sah leaderboard", "Compare your progress against the public rankings.")
		if state.DaemonSupported {
			add("sah daemon status", "Check whether the background worker is running.")
		}
	case "contributions":
		add("sah me", "See your overall credits, trust, and rank.")
		add("sah leaderboard", "Compare your progress against the public rankings.")
		if state.DaemonSupported {
			add("sah daemon status", "Check whether the background worker is still running.")
		}
	case "leaderboard":
		add("sah me", "Check your own credits, trust, and rank.")
		add("sah contributions", "See the recent work behind your score.")
	case "agents":
		addAgentSuggestions(add, state)
	case "upgrade":
		if state.DaemonInstalled {
			add("sah daemon status", "Verify the background worker after upgrading the CLI.")
		}
		add("sah version", "Confirm the new CLI version in a fresh invocation.")
	default:
		addPhaseSuggestions(add, state)
	}
}

func addPostLoginSuggestions(add func(command string, why string), state cliState) {
	if state.HasDetectedAgent {
		add("sah daemon install", "Install and start the background worker on this machine.")
		add("sah run --once", "Claim one task now in the foreground and exit.")
	} else {
		add("sah agents", "Inspect which supported local agent CLIs are available before running work.")
	}
	add("sah me", "Check the connected account, credits, and trust.")
}

func addRunSuggestions(add func(command string, why string), state cliState) {
	if state.DaemonInstalled {
		add("sah contributions", "See the submissions and reviews attached to this machine's work.")
		add("sah daemon status", "Confirm the background worker configuration and service state.")
	} else {
		add("sah daemon install", "Move from foreground runs to always-on background work.")
		add("sah contributions", "See what was submitted recently.")
	}
	add("sah me", "Check your current credits and rank.")
}

func addAgentSuggestions(add func(command string, why string), state cliState) {
	switch {
	case !state.AuthConfigured:
		add("sah auth login", "Sign in once this machine has the agent CLI setup you want.")
	case state.HasDetectedAgent && !state.DaemonInstalled:
		add("sah daemon install", "Start background work with the detected agent CLIs.")
		add("sah run --once", "Claim one task now in the foreground.")
	default:
		addPhaseSuggestions(add, state)
	}
}

func addPhaseSuggestions(add func(command string, why string), state cliState) {
	switch state.Stage {
	case stageSignedOut:
		add("sah auth login", "Sign in from your browser and link this machine.")
		add("sah agents", "Inspect which supported local agent CLIs `sah` can use here.")
		add("sah help", "See the full command catalog and examples.")
	case stageNeedsAgent:
		add("sah agents", "Inspect which supported local agent CLIs are available on this machine.")
		add("sah me", "Confirm the connected account before enabling work.")
		add("sah help run", "See how foreground and background execution work.")
	case stageReadyToRun:
		add("sah daemon install", "Install and start the background worker.")
		add("sah run --once", "Claim one task now in the foreground and exit.")
		add("sah me", "Check credits, pending credits, trust, and rank.")
	case stageDaemonStopped:
		add("sah daemon start", "Start the background worker again.")
		add("sah daemon status", "Check whether the installed service is healthy.")
		add("sah contributions", "See recent submissions and reviews.")
	case stageDaemonRunning:
		add("sah contributions", "See recent submissions and reviews.")
		add("sah me", "Check credits, pending credits, trust, and rank.")
		add("sah daemon status", "Verify the background service state.")
	}
}

func limitSuggestions(suggestions []commandSuggestion) []commandSuggestion {
	if len(suggestions) <= maxSuggestions {
		return suggestions
	}
	return suggestions[:maxSuggestions]
}

func normalizeHelpTopic(topic string) string {
	trimmed := strings.ToLower(strings.TrimSpace(topic))
	if trimmed == "" {
		return ""
	}
	if _, ok := guideByTopic[trimmed]; ok {
		return trimmed
	}

	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return ""
	}
	if _, ok := guideByTopic[parts[0]]; ok {
		return parts[0]
	}
	return ""
}

func guideForTopic(topic string) (commandGuide, bool) {
	if topic == "" {
		return commandGuide{}, false
	}
	guide, ok := guideByTopic[topic]
	return guide, ok
}

func canonicalCommandKey(args []string) string {
	if len(args) == 0 {
		return ""
	}

	command := strings.ToLower(strings.TrimSpace(args[0]))
	switch command {
	case "version", "--version", "-version":
		return "version"
	case "upgrade":
		return "upgrade"
	case "auth", "daemon":
		if len(args) > 1 {
			subcommand := strings.ToLower(strings.TrimSpace(args[1]))
			if subcommand != "" && !strings.HasPrefix(subcommand, "-") {
				return command + " " + subcommand
			}
		}
		return command
	default:
		return command
	}
}

func isHelpToken(raw string) bool {
	switch strings.TrimSpace(raw) {
	case "help", "--help", "-h", "-help":
		return true
	default:
		return false
	}
}

func isNotAuthenticatedError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "not authenticated") ||
		strings.Contains(message, "api key rejected") ||
		strings.Contains(message, "run `sah auth login` first")
}

func resolveReleaseStatus(paths sah.Paths, baseURL string) (*sah.ClientReleaseStatus, error) {
	release, err := resolveClientRelease(paths, baseURL)
	if release == nil && err != nil {
		return nil, err
	}
	return sah.ResolveClientReleaseStatus(release), err
}

func resolveClientRelease(paths sah.Paths, baseURL string) (*sah.ClientReleaseResponse, error) {
	var cachedRelease *sah.ClientReleaseResponse
	if strings.TrimSpace(paths.ReleaseCacheFile) != "" {
		release, fresh, err := sah.CachedClientRelease(paths, sah.DefaultClientReleaseCacheTTL)
		cachedRelease = release
		if err == nil && fresh {
			return cachedRelease, nil
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	release, refreshErr := sah.RefreshClientRelease(ctx, paths, baseURL)
	if refreshErr == nil {
		return release, nil
	}
	if cachedRelease != nil {
		return cachedRelease, nil
	}
	return nil, refreshErr
}

func displayVersion(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}

func upgradeWhy(status *sah.ClientReleaseStatus) string {
	target := releaseTargetVersion(status)
	if target == "" {
		return "Install the latest available SCIENCE@home CLI release."
	}
	return fmt.Sprintf("Install the recommended CLI release %s.", target)
}

func releaseTargetVersion(status *sah.ClientReleaseStatus) string {
	if status == nil {
		return ""
	}

	target := strings.TrimSpace(status.RecommendedVersion)
	if target != "" {
		return target
	}
	return strings.TrimSpace(status.LatestVersion)
}
