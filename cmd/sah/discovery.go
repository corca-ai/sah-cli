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

const (
	cliIntroTitle   = "SCIENCE@home CLI"
	cliIntroSummary = "`sah` links this machine to SCIENCE@home. It signs you in, claims coding tasks, runs a local agent CLI, and submits the result for credit."
	maxSuggestions  = 3
)

var localKernelActions = []sah.CommandAction{
	{
		Command:     "auth login",
		Title:       "Sign in",
		Description: "Pair this machine with SCIENCE@home and store a local credential.",
	},
	{
		Command:     "auth status",
		Title:       "Authentication status",
		Description: "Check whether this machine still has a working SCIENCE@home credential.",
	},
	{
		Command:     "auth logout",
		Title:       "Sign out",
		Description: "Remove the locally stored SCIENCE@home credential from this machine.",
	},
	{
		Command:     "run",
		Title:       "Foreground worker",
		Description: "Claim assignments and work in the foreground on this machine.",
	},
	{
		Command:     "daemon install",
		Title:       "Install background worker",
		Description: "Install and start the background worker service on this machine.",
	},
	{
		Command:     "daemon status",
		Title:       "Background worker status",
		Description: "Inspect whether the background worker service is installed and running.",
	},
	{
		Command:     "daemon start",
		Title:       "Start background worker",
		Description: "Start the installed background worker service again.",
	},
	{
		Command:     "daemon stop",
		Title:       "Stop background worker",
		Description: "Stop the installed background worker service without uninstalling it.",
	},
	{
		Command:     "daemon uninstall",
		Title:       "Uninstall background worker",
		Description: "Remove the installed background worker service from this machine.",
	},
	{
		Command:     "agents",
		Title:       "Local agent CLIs",
		Description: "Inspect which supported coding agent CLIs are available on this machine.",
	},
	{
		Command:     "version",
		Title:       "CLI version",
		Description: "Print the installed sah CLI build version on this machine.",
	},
}

var fallbackCatalogActions = []sah.CommandAction{
	{
		Command:     "me",
		Title:       "My account",
		Description: "View your SCIENCE@home credits, trust, and rank.",
	},
	{
		Command:     "contributions",
		Title:       "Recent contributions",
		Description: "View your recent submissions and reviews.",
	},
	{
		Command:     "leaderboard",
		Title:       "Leaderboard",
		Description: "View the public weekly, monthly, and all-time rankings.",
	},
}

var serverNavigationResolver = resolveServerNavigation

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
		AuthConfigured:  config.HasAuth(),
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
		} else {
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

	document, navigation := serverNavigationResolver(state, "", nil)
	title := cliIntroTitle
	summary := cliIntroSummary
	if document != nil {
		if strings.TrimSpace(document.Title) != "" {
			title = document.Title
		}
		if strings.TrimSpace(document.Description) != "" {
			summary = document.Description
		}
	}

	printCLIIntro(writer, title, summary)
	_, _ = fmt.Fprintln(writer, "For the full command guide, run `sah help`.")
	_, _ = fmt.Fprintln(writer)
	printStateSummary(writer, state)
	printSuggestionSection(writer, "Try next", navigation)
}

func printHelp(writer io.Writer, topic string, state cliState) {
	if writer == nil {
		return
	}

	document, navigation := serverNavigationResolver(state, normalizeHelpTopic(topic), nil)
	title := "SCIENCE@home CLI guide"
	summary := cliIntroSummary
	normalizedTopic := normalizeHelpTopic(topic)
	if document != nil {
		if strings.TrimSpace(document.Title) != "" {
			title = document.Title
		}
		if strings.TrimSpace(document.Description) != "" {
			summary = document.Description
		}
	}

	printCLIIntro(writer, title, summary)
	if normalizedTopic != "" {
		_, _ = fmt.Fprintf(writer, "Help: %s\n\n", normalizedTopic)
	}
	printCommandCatalog(writer, filterActions(topic, documentActions(document)))
	printStateSummary(writer, state)
	printSuggestionSection(writer, "Try next", navigation)
}

func printCLIIntro(writer io.Writer, title string, summary string) {
	_, _ = fmt.Fprintln(writer, title)
	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, summary)
	_, _ = fmt.Fprintln(writer)
}

func printCommandCatalog(writer io.Writer, actions []sah.CommandAction) {
	if len(actions) == 0 {
		actions = localKernelActions
	}

	_, _ = fmt.Fprintln(writer, "Commands")
	tw := tabwriter.NewWriter(writer, 0, 0, 2, ' ', 0)
	for _, action := range actions {
		command := strings.TrimSpace(action.Command)
		if command == "" {
			continue
		}
		_, _ = fmt.Fprintf(tw, "  sah %s\t%s\n", command, strings.TrimSpace(action.Description))
	}
	_ = tw.Flush()
}

func printUnknownCommand(writer io.Writer, raw string, state cliState) {
	if writer == nil {
		return
	}

	document, navigation := serverNavigationResolver(state, "", fmt.Errorf("unknown command"))
	_, _ = fmt.Fprintf(writer, "sah: unknown command %q\n\n", raw)
	printCommandCatalog(writer, documentActions(document))
	printSuggestionSection(writer, "Try next", navigation)
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
	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, "Try next")
	for _, suggestion := range resolveNavigationSuggestions(state, commandKey, err) {
		_, _ = fmt.Fprintf(writer, "- `%s` %s\n", suggestion.Command, suggestion.Why)
	}
}

func printCommandSuccessHints(writer io.Writer, state cliState, commandKey string) {
	if writer == nil {
		return
	}
	printSuggestionSection(writer, "Next commands", resolveNavigationSuggestions(state, commandKey, nil))
}

func shouldPrintCommandSuccessHints(commandKey string) bool {
	switch strings.TrimSpace(commandKey) {
	case "", "version":
		return false
	default:
		return true
	}
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
	if status == nil || !status.UpdateAvailable {
		return ""
	}
	target := releaseTargetVersion(status)
	if target == "" {
		return ""
	}
	if url := strings.TrimSpace(status.ReleaseNotesURL); url != "" {
		return fmt.Sprintf("available: %s (current %s, %s)", target, displayVersion(status.CurrentVersion), url)
	}
	return fmt.Sprintf("available: %s (current %s)", target, displayVersion(status.CurrentVersion))
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

func resolveServerNavigation(
	state cliState,
	commandKey string,
	err error,
) (*sah.ServiceDocument, []commandSuggestion) {
	paths, _, loadErr := loadConfig()
	baseURL := sciHomeURL(state.BaseURL)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var client *sah.Client
	if loadErr == nil {
		client = sah.NewCachedClient(paths, baseURL, "")
	} else {
		client = sah.NewClient(baseURL, "")
	}
	document, docErr := client.GetServiceDocument(ctx)
	if docErr != nil {
		document = nil
	}

	nav, navErr := client.GetNavigation(ctx, navigationRequestFromState(state, commandKey, err))
	if navErr != nil || nav == nil {
		return document, fallbackSuggestions(state, commandKey, err)
	}
	suggestions := toSuggestions(nav.Actions)
	if len(suggestions) == 0 {
		return document, fallbackSuggestions(state, commandKey, err)
	}
	return document, suggestions
}

func resolveNavigationSuggestions(state cliState, commandKey string, err error) []commandSuggestion {
	_, suggestions := serverNavigationResolver(state, commandKey, err)
	return suggestions
}

func suggestionsForContext(state cliState, commandKey string, err error) []commandSuggestion {
	return resolveNavigationSuggestions(state, commandKey, err)
}

func documentActions(document *sah.ServiceDocument) []sah.CommandAction {
	fallback := mergeCommandActions(localKernelActions, fallbackCatalogActions)
	if document == nil || len(document.Actions) == 0 {
		return fallback
	}
	return mergeCommandActions(document.Actions, fallback)
}

func filterActions(topic string, actions []sah.CommandAction) []sah.CommandAction {
	normalized := normalizeHelpTopic(topic)
	if normalized == "" {
		return actions
	}

	filtered := make([]sah.CommandAction, 0, len(actions))
	for _, action := range actions {
		command := strings.ToLower(strings.TrimSpace(action.Command))
		if command == normalized || strings.HasPrefix(command, normalized+" ") {
			filtered = append(filtered, action)
		}
	}
	if len(filtered) == 0 {
		return actions
	}
	return filtered
}

func mergeCommandActions(primary []sah.CommandAction, fallback []sah.CommandAction) []sah.CommandAction {
	merged := make([]sah.CommandAction, 0, len(primary)+len(fallback))
	seen := make(map[string]struct{}, len(primary)+len(fallback))
	appendUnique := func(actions []sah.CommandAction) {
		for _, action := range actions {
			command := strings.ToLower(strings.TrimSpace(action.Command))
			if command == "" {
				continue
			}
			if _, ok := seen[command]; ok {
				continue
			}
			seen[command] = struct{}{}
			merged = append(merged, action)
		}
	}

	appendUnique(primary)
	appendUnique(fallback)
	return merged
}

func toSuggestions(actions []sah.CommandAction) []commandSuggestion {
	suggestions := make([]commandSuggestion, 0, len(actions))
	for _, action := range actions {
		command := strings.TrimSpace(action.Command)
		why := strings.TrimSpace(action.Description)
		if command == "" || why == "" {
			continue
		}
		suggestions = append(suggestions, commandSuggestion{
			Command: "sah " + command,
			Why:     why,
		})
		if len(suggestions) == maxSuggestions {
			break
		}
	}
	return suggestions
}

func navigationRequestFromState(state cliState, commandKey string, err error) sah.NavigationRequest {
	return sah.NavigationRequest{
		AuthConfigured:  state.AuthConfigured,
		DetectedAgents:  append([]string(nil), state.DetectedAgents...),
		DaemonSupported: state.DaemonSupported,
		DaemonInstalled: state.DaemonInstalled,
		DaemonRunning:   state.DaemonRunning,
		CurrentCommand:  strings.TrimSpace(commandKey),
		LastError:       navigationErrorCode(err),
	}
}

func navigationErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case isNotAuthenticatedError(err):
		return "not_authenticated"
	case sah.IsNoSupportedAgentCLI(err):
		return "no_supported_agent"
	case sah.IsWorkerContractError(err):
		return "worker_contract"
	default:
		return ""
	}
}

func fallbackSuggestions(state cliState, commandKey string, err error) []commandSuggestion {
	request := navigationRequestFromState(state, commandKey, err)
	actions := make([]sah.CommandAction, 0, 3)
	switch request.LastError {
	case "not_authenticated":
		actions = append(actions,
			localKernelActions[0],
			localKernelActions[1],
			localKernelActions[9],
		)
	case "no_supported_agent":
		actions = append(actions,
			localKernelActions[9],
			localKernelActions[1],
			localKernelActions[3],
		)
	default:
		switch state.Stage {
		case stageSignedOut:
			actions = append(actions, localKernelActions[0], localKernelActions[9], localKernelActions[10])
		case stageNeedsAgent:
			actions = append(actions, localKernelActions[9], sah.CommandAction{Command: "me", Description: "Confirm the connected account before enabling work."}, localKernelActions[3])
		case stageReadyToRun:
			if state.DaemonSupported {
				actions = append(actions, localKernelActions[4], localKernelActions[3], sah.CommandAction{Command: "me", Description: "Check your credits, trust, and rank."})
			} else {
				actions = append(actions, localKernelActions[3], sah.CommandAction{Command: "me", Description: "Check your credits, trust, and rank."}, localKernelActions[9])
			}
		case stageDaemonStopped:
			if state.DaemonSupported {
				actions = append(actions, localKernelActions[6], localKernelActions[5], sah.CommandAction{Command: "contributions", Description: "See recent submissions and reviews."})
			} else {
				actions = append(actions, localKernelActions[3], sah.CommandAction{Command: "contributions", Description: "See recent submissions and reviews."}, sah.CommandAction{Command: "me", Description: "Check your credits, trust, and rank."})
			}
		default:
			if state.DaemonSupported {
				actions = append(actions, sah.CommandAction{Command: "contributions", Description: "See recent submissions and reviews."}, sah.CommandAction{Command: "me", Description: "Check your credits, trust, and rank."}, localKernelActions[5])
			} else {
				actions = append(actions, localKernelActions[3], sah.CommandAction{Command: "contributions", Description: "See recent submissions and reviews."}, sah.CommandAction{Command: "me", Description: "Check your credits, trust, and rank."})
			}
		}
	}
	return toSuggestions(actions)
}

func normalizeHelpTopic(topic string) string {
	normalized := strings.ToLower(strings.TrimSpace(topic))
	if normalized == "" {
		return ""
	}
	if strings.Contains(normalized, " ") {
		return strings.Fields(normalized)[0]
	}
	return normalized
}

func canonicalCommandKey(args []string) string {
	if len(args) == 0 {
		return ""
	}

	command := strings.ToLower(strings.TrimSpace(args[0]))
	switch command {
	case "version", "--version", "-version":
		return "version"
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
	return sah.IsAuthenticationFailure(err)
}

func resolveReleaseStatus(paths sah.Paths, baseURL string) (*sah.ClientReleaseStatus, error) {
	release, err := resolveClientRelease(paths, baseURL)
	if release == nil && err != nil {
		return nil, err
	}
	return sah.ResolveClientReleaseStatus(release), err
}

func resolveClientRelease(paths sah.Paths, baseURL string) (*sah.ClientReleaseResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return sah.NewCachedClient(paths, baseURL, "").GetClientRelease(ctx)
}

func displayVersion(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
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
