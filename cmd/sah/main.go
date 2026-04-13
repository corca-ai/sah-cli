package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/corca-ai/sah-cli/internal/sah"
)

var version = "dev"

const leaderboardVisibleRows = 15

type reportedError struct {
	err error
}

func (err *reportedError) Error() string {
	if err == nil || err.err == nil {
		return ""
	}
	return err.err.Error()
}

func (err *reportedError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.err
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "help", "--help", "-h", "-help":
		usage()
	case "auth":
		err = authCmd(os.Args[2:])
	case "run":
		err = runCmd(os.Args[2:])
	case "daemon":
		err = daemonCmd(os.Args[2:])
	case "me":
		err = meCmd(os.Args[2:])
	case "contributions":
		err = contributionsCmd(os.Args[2:])
	case "leaderboard":
		err = leaderboardCmd(os.Args[2:])
	case "agents":
		err = agentsCmd(os.Args[2:])
	case "version", "--version", "-version":
		fmt.Println(version)
	default:
		unknownCmd(os.Args[1:])
	}

	if err != nil {
		var reported *reportedError
		if !errors.As(err, &reported) {
			fmt.Fprintf(os.Stderr, "sah: %v\n", err)
		}
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: sah <command> [flags]

Commands:
  auth login|logout|status   Authenticate and inspect local auth state
  run                        Run the foreground worker loop
  daemon install|start|stop|status|uninstall
                             Manage the background worker service
  me                         Show your SCIENCE@home account summary
  contributions              Show recent submissions and reviews
  leaderboard                Show public rankings
  agents                     Show supported local agent CLIs
  version                    Print the build version

Examples:
  sah auth login
  sah run --rotate-installed
  sah run --agents codex,gemini,claude,qwen --models codex=gpt-5.4-mini,gemini=gemini-3-flash-base,claude=sonnet
  sah daemon install --agents codex,claude,qwen --interval 30m
  sah me
`)
}

func unknownCmd(args []string) {
	fmt.Fprintf(os.Stderr, "sah: unknown command %q\n\n", args[0])
	usage()
	os.Exit(2)
}

func authCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: sah auth login|logout|status")
	}

	switch args[0] {
	case "login":
		fs := flag.NewFlagSet("auth login", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		baseURL := fs.String("base-url", "", "SCIENCE@home base URL")
		noOpen := fs.Bool("no-open", false, "Print the auth URL without opening a browser")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}

		ctx, cancel := signalContext()
		defer cancel()

		paths, config, err := loadConfig()
		if err != nil {
			return err
		}
		if strings.TrimSpace(*baseURL) != "" {
			config.BaseURL = *baseURL
		}

		if err := loginAndPersist(ctx, paths, &config, !*noOpen); err != nil {
			return err
		}

		client := sah.NewClient(config.BaseURL, config.APIKey)
		me, err := client.GetMe(ctx)
		if err == nil {
			fmt.Printf("Authenticated as %s <%s>.\n", me.Name, me.Email)
			fmt.Printf("Rank: #%d, credits: %d, pending: %d\n", me.Rank, me.Credits, me.PendingCredits)
			return nil
		}

		fmt.Println("Authenticated successfully.")
		return nil
	case "logout":
		paths, config, err := loadConfig()
		if err != nil {
			return err
		}
		config.APIKey = ""
		if err := sah.SaveConfig(paths, config); err != nil {
			return err
		}
		fmt.Println("Removed local SCIENCE@home API key.")
		return nil
	case "status":
		_, config, err := loadConfig()
		if err != nil {
			return err
		}
		fmt.Printf("Base URL: %s\n", config.BaseURL)
		if strings.TrimSpace(config.APIKey) == "" {
			fmt.Println("Authentication: not logged in")
			return nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		client := sah.NewClient(config.BaseURL, config.APIKey)
		me, err := client.GetMe(ctx)
		switch {
		case err == nil:
			fmt.Printf("Authentication: logged in as %s <%s>\n", me.Name, me.Email)
			fmt.Printf("Rank: #%d, credits: %d, pending: %d\n", me.Rank, me.Credits, me.PendingCredits)
			return nil
		case sah.IsStatus(err, http.StatusUnauthorized), sah.IsStatus(err, http.StatusForbidden):
			fmt.Println("Authentication: stored API key exists but was rejected by the server")
			return nil
		default:
			return err
		}
	default:
		return fmt.Errorf("usage: sah auth login|logout|status")
	}
}

func runCmd(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	agent := fs.String("agent", "", "Agent CLI to use: codex, gemini, claude, qwen")
	agents := fs.String("agents", "", "Comma-separated round-robin agent order, e.g. codex,gemini,claude,qwen")
	rotateInstalled := fs.Bool("rotate-installed", false, "Rotate through every supported agent CLI installed on this Mac")
	model := fs.String("model", "", "Optional model override passed to the agent CLI")
	models := fs.String("models", "", "Per-agent model overrides, e.g. codex=gpt-5.4-mini,gemini=gemini-3-flash-base,claude=sonnet,qwen=<name>")
	interval := fs.String("interval", "", "Polling interval")
	timeout := fs.String("timeout", "", "Per-assignment agent timeout")
	taskType := fs.String("task-type", "", "Optional task type filter")
	baseURL := fs.String("base-url", "", "SCIENCE@home base URL")
	once := fs.Bool("once", false, "Run a single polling cycle and exit")
	daemonMode := fs.Bool("daemon", false, "Run non-interactively for the background service")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := validateAgentFlags(*agent, *agents, *rotateInstalled); err != nil {
		return err
	}
	if !*daemonMode {
		sah.PrintRunBanner(os.Stdout)
	}

	ctx, cancel := signalContext()
	defer cancel()

	paths, config, err := loadConfig()
	if err != nil {
		return err
	}
	if strings.TrimSpace(*baseURL) != "" {
		config.BaseURL = *baseURL
	}

	outputWriter := io.Writer(os.Stdout)
	errorWriter := io.Writer(os.Stderr)
	report := func(runErr error) error {
		return runErr
	}
	if *daemonMode {
		daemonLogs, err := sah.OpenDaemonLogs(paths)
		if err != nil {
			return err
		}
		defer func() {
			_ = daemonLogs.Close()
		}()

		outputWriter = daemonLogs.Stdout
		errorWriter = daemonLogs.Stderr
		report = func(runErr error) error {
			if runErr == nil {
				return nil
			}
			_, _ = fmt.Fprintf(errorWriter, "[%s] sah: %v\n", time.Now().Format(time.RFC3339), runErr)
			return &reportedError{err: runErr}
		}
	}

	if strings.TrimSpace(config.APIKey) == "" {
		if *daemonMode {
			return report(fmt.Errorf("daemon mode requires an existing API key; run `sah auth login` first"))
		}
		if err := loginAndPersist(ctx, paths, &config, true); err != nil {
			return err
		}
	}

	var binaryPaths map[string]string
	if *daemonMode {
		binaryPaths = config.AgentBinaryPaths
	}

	if strings.TrimSpace(*agent) != "" {
		if _, err := sah.ResolveAgentWithBinaryPaths(*agent, binaryPaths); err != nil {
			return report(err)
		}
	}
	agentPool := sah.ParseAgentList(*agents)
	if len(agentPool) > 0 {
		if _, err := sah.ResolveAgentPool(config, sah.WorkerOptions{
			Agents:      agentPool,
			BinaryPaths: binaryPaths,
		}); err != nil {
			return report(err)
		}
	}
	agentModels, err := sah.ParseAgentModels(*models)
	if err != nil {
		return report(err)
	}

	pollInterval, err := sah.ParsePollInterval(pickString(*interval, config.PollInterval))
	if err != nil {
		return report(err)
	}
	agentTimeout, err := sah.ParseAgentTimeout(pickString(*timeout, config.AgentTimeout))
	if err != nil {
		return report(err)
	}

	options := sah.WorkerOptions{
		Agent:           pickString(*agent, config.DefaultAgent),
		Agents:          agentPool,
		RotateInstalled: *rotateInstalled,
		BinaryPaths:     binaryPaths,
		Model:           pickString(*model, config.AgentModel),
		Models:          sah.MergeAgentModels(config.AgentModels, agentModels),
		Interval:        pollInterval,
		Timeout:         agentTimeout,
		TaskType:        strings.TrimSpace(*taskType),
		Once:            *once,
		Output:          outputWriter,
		ErrorOutput:     errorWriter,
	}

	picker, err := sah.NewAgentPicker(config, options)
	if err != nil {
		return report(err)
	}
	if !*daemonMode {
		sah.PrintRunPlan(os.Stdout, config, options, picker.Pool())
	}

	return report(sah.RunWorker(ctx, config, options))
}

func daemonCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: sah daemon install|start|stop|status|uninstall")
	}

	switch args[0] {
	case "install":
		return daemonInstallCmd(args[1:])
	case "start":
		return daemonStartCmd()
	case "stop":
		return daemonStopCmd()
	case "status":
		return daemonStatusCmd()
	case "uninstall":
		return daemonUninstallCmd()
	default:
		return fmt.Errorf("usage: sah daemon install|start|stop|status|uninstall")
	}
}

type daemonInstallOptions struct {
	agent           string
	agents          string
	rotateInstalled bool
	model           string
	models          string
	interval        string
	timeout         string
	baseURL         string
}

func daemonInstallCmd(args []string) error {
	options, err := parseDaemonInstallOptions(args)
	if err != nil {
		return err
	}

	ctx, cancel := signalContext()
	defer cancel()

	paths, config, err := loadConfig()
	if err != nil {
		return err
	}
	if err := applyDaemonInstallOptions(&config, options); err != nil {
		return err
	}
	config.AgentBinaryPaths = sah.CaptureInstalledAgentBinaryPaths()

	if strings.TrimSpace(config.APIKey) == "" {
		if err := loginAndPersist(ctx, paths, &config, true); err != nil {
			return err
		}
	} else if err := sah.SaveConfig(paths, config); err != nil {
		return err
	}

	executable, err := resolveExecutable()
	if err != nil {
		return err
	}
	if err := sah.InstallService(paths, executable); err != nil {
		return err
	}

	fmt.Printf("Installed and started SCIENCE@home %s service.\n", sah.ServiceManagerName())
	fmt.Printf("%s: %s\n", sah.ServiceDefinitionLabel(), sah.ServiceDefinitionPath(paths))
	fmt.Printf("Daemon logs: %s and %s\n", paths.DaemonStdoutLog, paths.DaemonStderrLog)
	if capture := sah.ServiceCaptureValue(paths); capture != "" {
		fmt.Printf("%s: %s\n", sah.ServiceCaptureLabel(), capture)
	}
	fmt.Printf("Captured PATH, HOME, and installed agent binary paths for %s. Re-run `sah daemon install` after changing agent install paths.\n", sah.ServiceManagerName())
	return nil
}

func parseDaemonInstallOptions(args []string) (daemonInstallOptions, error) {
	fs := flag.NewFlagSet("daemon install", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	options := daemonInstallOptions{}
	fs.StringVar(&options.agent, "agent", "", "Default agent CLI for the daemon")
	fs.StringVar(&options.agents, "agents", "", "Comma-separated round-robin agent order for the daemon")
	fs.BoolVar(&options.rotateInstalled, "rotate-installed", false, "Rotate through every installed supported agent CLI")
	fs.StringVar(&options.model, "model", "", "Default model override")
	fs.StringVar(&options.models, "models", "", "Per-agent model overrides, e.g. codex=gpt-5.4-mini,gemini=gemini-3-flash-base,claude=sonnet,qwen=<name>")
	fs.StringVar(&options.interval, "interval", "", "Default polling interval")
	fs.StringVar(&options.timeout, "timeout", "", "Default per-assignment timeout")
	fs.StringVar(&options.baseURL, "base-url", "", "SCIENCE@home base URL")

	if err := fs.Parse(args); err != nil {
		return daemonInstallOptions{}, err
	}
	if err := validateAgentFlags(options.agent, options.agents, options.rotateInstalled); err != nil {
		return daemonInstallOptions{}, err
	}
	return options, nil
}

func applyDaemonInstallOptions(config *sah.Config, options daemonInstallOptions) error {
	if strings.TrimSpace(options.baseURL) != "" {
		config.BaseURL = options.baseURL
	}
	if strings.TrimSpace(options.agent) != "" {
		if _, err := sah.ResolveAgent(options.agent); err != nil {
			return err
		}
		config.DefaultAgent = options.agent
		config.AgentPool = nil
		config.RotateInstalled = false
	}
	if pool := sah.ParseAgentList(options.agents); len(pool) > 0 {
		if _, err := sah.ResolveAgentPool(*config, sah.WorkerOptions{
			Agents: pool,
		}); err != nil {
			return err
		}
		config.AgentPool = pool
		config.RotateInstalled = false
	}
	if options.rotateInstalled {
		if _, err := sah.ResolveAgentPool(*config, sah.WorkerOptions{
			RotateInstalled: true,
		}); err != nil {
			return err
		}
		config.AgentPool = nil
		config.RotateInstalled = true
	}
	if strings.TrimSpace(options.model) != "" {
		config.AgentModel = options.model
	}
	if parsedModels, err := sah.ParseAgentModels(options.models); err != nil {
		return err
	} else if parsedModels != nil {
		config.AgentModels = parsedModels
	}
	if strings.TrimSpace(options.interval) != "" {
		if _, err := sah.ParsePollInterval(options.interval); err != nil {
			return err
		}
		config.PollInterval = options.interval
	}
	if strings.TrimSpace(options.timeout) != "" {
		if _, err := sah.ParseAgentTimeout(options.timeout); err != nil {
			return err
		}
		config.AgentTimeout = options.timeout
	}
	return nil
}

func resolveExecutable() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	executable, err = sah.PreferredServiceExecutable(executable)
	if err != nil {
		return "", err
	}
	return executable, nil
}

func selectLaunchdExecutable(resolved string, candidates []string) string {
	if canonical, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = canonical
	}

	for _, candidate := range candidates {
		target, targetErr := filepath.EvalSymlinks(candidate)
		if targetErr != nil {
			continue
		}
		if filepath.Clean(target) == filepath.Clean(resolved) {
			return candidate
		}
	}

	return filepath.Clean(resolved)
}

func daemonStartCmd() error {
	paths, _, err := loadConfig()
	if err != nil {
		return err
	}
	if _, err := os.Stat(sah.ServiceDefinitionPath(paths)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s service is not installed", sah.ServiceManagerName())
		}
		return err
	}

	if err := sah.StartService(paths); err != nil {
		return err
	}
	fmt.Printf("Started SCIENCE@home %s service.\n", sah.ServiceManagerName())
	return nil
}

func daemonStopCmd() error {
	paths, _, err := loadConfig()
	if err != nil {
		return err
	}
	if err := sah.StopService(paths); err != nil {
		return err
	}
	fmt.Printf("Stopped SCIENCE@home %s service.\n", sah.ServiceManagerName())
	return nil
}

func daemonStatusCmd() error {
	paths, config, err := loadConfig()
	if err != nil {
		return err
	}

	loaded, detail, err := sah.ServiceStatus(paths)
	if err != nil {
		return err
	}

	fmt.Printf("Base URL: %s\n", config.BaseURL)
	fmt.Printf("Agents: %s\n", sah.DescribeAgentMode(config, sah.WorkerOptions{}))
	if model := strings.TrimSpace(config.AgentModel); model != "" {
		fmt.Printf("Model: %s\n", model)
	}
	if models := sah.FormatAgentModels(config.AgentModels); models != "" {
		fmt.Printf("Per-agent models: %s\n", models)
	}
	fmt.Printf("Interval: %s\n", config.PollInterval)
	fmt.Printf("%s: %s\n", sah.ServiceStatusLabel(), formatServiceState(loaded, detail))
	fmt.Printf("%s: %s\n", sah.ServiceDefinitionLabel(), sah.ServiceDefinitionPath(paths))
	fmt.Printf("Daemon logs: %s\n", strings.Join([]string{paths.DaemonStdoutLog, paths.DaemonStderrLog}, " and "))
	if capture := sah.ServiceCaptureValue(paths); capture != "" {
		fmt.Printf("%s: %s\n", sah.ServiceCaptureLabel(), capture)
	}
	return nil
}

func daemonUninstallCmd() error {
	paths, _, err := loadConfig()
	if err != nil {
		return err
	}
	if err := sah.UninstallService(paths); err != nil {
		return err
	}
	fmt.Printf("Removed SCIENCE@home %s service.\n", sah.ServiceManagerName())
	return nil
}

func formatServiceState(loaded bool, detail string) string {
	trimmed := strings.TrimSpace(detail)
	if trimmed != "" {
		switch sah.ServiceManagerName() {
		case "launchd":
			if loaded {
				return "loaded"
			}
			return "not loaded"
		default:
			return trimmed
		}
	}
	if loaded {
		switch sah.ServiceManagerName() {
		case "launchd":
			return "loaded"
		default:
			return "active"
		}
	}
	switch sah.ServiceManagerName() {
	case "launchd":
		return "not loaded"
	default:
		return "inactive"
	}
}

func meCmd(args []string) error {
	fs := flag.NewFlagSet("me", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	baseURL := fs.String("base-url", "", "SCIENCE@home base URL")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, config, err := loadConfig()
	if err != nil {
		return err
	}
	if strings.TrimSpace(*baseURL) != "" {
		config.BaseURL = *baseURL
	}
	client, err := authedClient(config)
	if err != nil {
		return err
	}
	me, err := client.GetMe(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("Name: %s\n", me.Name)
	fmt.Printf("Email: %s\n", me.Email)
	fmt.Printf("Rank: #%d\n", me.Rank)
	fmt.Printf("Credits: %d\n", me.Credits)
	fmt.Printf("Leaderboard score: %d\n", me.LeaderboardScore)
	fmt.Printf("Pending credits: %d\n", me.PendingCredits)
	fmt.Printf("Trust: %.4f\n", me.Trust)
	return nil
}

func contributionsCmd(args []string) error {
	fs := flag.NewFlagSet("contributions", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	limit := fs.Int("limit", 10, "How many recent items to fetch per category")
	baseURL := fs.String("base-url", "", "SCIENCE@home base URL")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, config, err := loadConfig()
	if err != nil {
		return err
	}
	if strings.TrimSpace(*baseURL) != "" {
		config.BaseURL = *baseURL
	}
	client, err := authedClient(config)
	if err != nil {
		return err
	}

	response, err := client.GetContributions(ctx, *limit)
	if err != nil {
		return err
	}

	printHistorySection("Submissions", response.Submissions)
	fmt.Println()
	printHistorySection("Reviews", response.Reviews)
	return nil
}

func leaderboardCmd(args []string) error {
	fs := flag.NewFlagSet("leaderboard", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	window := fs.String("window", "all", "all, weekly, monthly, all-time")
	baseURL := fs.String("base-url", "", "SCIENCE@home base URL")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, config, err := loadConfig()
	if err != nil {
		return err
	}
	if strings.TrimSpace(*baseURL) != "" {
		config.BaseURL = *baseURL
	}
	client := sah.NewClient(config.BaseURL, config.APIKey)
	response, err := client.GetLeaderboard(ctx)
	if err != nil && strings.TrimSpace(config.APIKey) != "" &&
		(sah.IsStatus(err, http.StatusUnauthorized) || sah.IsStatus(err, http.StatusForbidden)) {
		response, err = sah.NewClient(config.BaseURL, "").GetLeaderboard(ctx)
	}
	if err != nil {
		return err
	}

	switch strings.ToLower(strings.TrimSpace(*window)) {
	case "all":
		printLeaderboardSection("Weekly", response.Weekly, leaderboardViewerEntry(response.Viewer, "weekly"))
		fmt.Println()
		printLeaderboardSection("Monthly", response.Monthly, leaderboardViewerEntry(response.Viewer, "monthly"))
		fmt.Println()
		printLeaderboardSection("All-Time", response.AllTime, leaderboardViewerEntry(response.Viewer, "all-time"))
	case "weekly":
		printLeaderboardSection("Weekly", response.Weekly, leaderboardViewerEntry(response.Viewer, "weekly"))
	case "monthly":
		printLeaderboardSection("Monthly", response.Monthly, leaderboardViewerEntry(response.Viewer, "monthly"))
	case "all-time":
		printLeaderboardSection("All-Time", response.AllTime, leaderboardViewerEntry(response.Viewer, "all-time"))
	default:
		return fmt.Errorf("unsupported window %q", *window)
	}
	return nil
}

func agentsCmd(args []string) error {
	fs := flag.NewFlagSet("agents", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}

	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "NAME\tINSTALLED\tPATH\tDESCRIPTION\tMODEL FLAG")
	for _, status := range sah.InstalledAgents() {
		installed := "no"
		if status.Installed {
			installed = "yes"
		}
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t--model / --models %s=<name>\n", status.Name, installed, status.Path, status.Description, status.Name)
	}
	return writer.Flush()
}

func loadConfig() (sah.Paths, sah.Config, error) {
	paths, err := sah.ResolvePaths()
	if err != nil {
		return sah.Paths{}, sah.Config{}, err
	}
	config, err := sah.LoadConfig(paths)
	if err != nil {
		return sah.Paths{}, sah.Config{}, err
	}
	return paths, config, nil
}

func loginAndPersist(
	ctx context.Context,
	paths sah.Paths,
	config *sah.Config,
	openBrowser bool,
) error {
	response, err := sah.Login(ctx, sah.LoginOptions{
		BaseURL:     config.BaseURL,
		Output:      os.Stdout,
		OpenBrowser: openBrowser,
	})
	if err != nil {
		return err
	}

	config.APIKey = response.APIKey
	return sah.SaveConfig(paths, *config)
}

func authedClient(config sah.Config) (*sah.Client, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, fmt.Errorf("not authenticated; run `sah auth login` first")
	}
	return sah.NewClient(config.BaseURL, config.APIKey), nil
}

func printHistorySection(title string, entries []sah.HistoryEntry) {
	fmt.Println(title)
	if len(entries) == 0 {
		fmt.Println("  (none)")
		return
	}

	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "WHEN\tTASK\tSTATUS\tCREDITS\tSUBJECT")
	for _, entry := range entries {
		_, _ = fmt.Fprintf(
			writer,
			"%s\t%s\t%s\t%d (%s)\t%s\n",
			entry.CreatedAt.Local().Format("2006-01-02 15:04"),
			entry.TaskType,
			entry.StatusLabel,
			entry.CreditAmount,
			entry.CreditState,
			entry.Subject,
		)
	}
	_ = writer.Flush()
}

type leaderboardDisplayEntry struct {
	Rank   string
	Label  string
	Earned string
}

func leaderboardViewerEntry(viewer *sah.LeaderboardViewer, window string) *sah.LeaderboardEntry {
	if viewer == nil {
		return nil
	}

	switch strings.ToLower(strings.TrimSpace(window)) {
	case "weekly":
		return viewer.Weekly
	case "monthly":
		return viewer.Monthly
	case "all-time":
		return viewer.AllTime
	default:
		return nil
	}
}

func leaderboardEntryLabel(entry sah.LeaderboardEntry) string {
	if label := strings.TrimSpace(entry.PublicLabel); label != "" {
		return label
	}
	if publicID := strings.TrimSpace(entry.PublicID); publicID != "" {
		return publicID
	}
	if entry.ID > 0 {
		return fmt.Sprintf("user-%d", entry.ID)
	}
	return "(unknown)"
}

func leaderboardDisplayEntries(
	entries []sah.LeaderboardEntry,
	viewer *sah.LeaderboardEntry,
) []leaderboardDisplayEntry {
	if len(entries) > leaderboardVisibleRows {
		entries = entries[:leaderboardVisibleRows]
	}

	display := make([]leaderboardDisplayEntry, 0, len(entries)+2)
	for index, entry := range entries {
		rank := entry.Rank
		if rank <= 0 {
			rank = index + 1
		}
		display = append(display, leaderboardDisplayEntry{
			Rank:   strconv.Itoa(rank),
			Label:  leaderboardEntryLabel(entry),
			Earned: strconv.Itoa(entry.Earned),
		})
	}

	if viewer == nil || viewer.Rank <= 0 {
		return display
	}
	for _, entry := range entries {
		if entry.ID == viewer.ID {
			return display
		}
	}

	lastRank := 0
	if len(entries) > 0 {
		lastRank = entries[len(entries)-1].Rank
		if lastRank <= 0 {
			lastRank = len(entries)
		}
	}
	if lastRank > 0 && viewer.Rank > lastRank+1 {
		display = append(display, leaderboardDisplayEntry{
			Rank:   "...",
			Label:  "...",
			Earned: "...",
		})
	}

	display = append(display, leaderboardDisplayEntry{
		Rank:   strconv.Itoa(viewer.Rank),
		Label:  leaderboardEntryLabel(*viewer),
		Earned: strconv.Itoa(viewer.Earned),
	})
	return display
}

func printLeaderboardSection(title string, entries []sah.LeaderboardEntry, viewer *sah.LeaderboardEntry) {
	fmt.Println(title)
	display := leaderboardDisplayEntries(entries, viewer)
	if len(display) == 0 {
		fmt.Println("  (none)")
		return
	}

	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "RANK\tNAME\tSCORE")
	for _, entry := range display {
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\n", entry.Rank, entry.Label, entry.Earned)
	}
	_ = writer.Flush()
}

func pickString(primary string, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return primary
	}
	return fallback
}

func validateAgentFlags(agent string, agents string, rotateInstalled bool) error {
	if rotateInstalled && (strings.TrimSpace(agent) != "" || strings.TrimSpace(agents) != "") {
		return fmt.Errorf("--rotate-installed cannot be combined with --agent or --agents")
	}
	if strings.TrimSpace(agent) != "" && strings.TrimSpace(agents) != "" {
		return fmt.Errorf("--agent cannot be combined with --agents")
	}
	return nil
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
