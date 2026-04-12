package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/corca-ai/sah-cli/internal/sah"
)

var version = "dev"

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
		fmt.Fprintf(os.Stderr, "sah: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: sah <command> [flags]

Commands:
  auth login|logout|status   Authenticate and inspect local auth state
  run                        Run the foreground worker loop
  daemon install|start|stop|status|uninstall
                             Manage the macOS launchd worker
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
	daemonMode := fs.Bool("daemon", false, "Run non-interactively for launchd")
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

	if strings.TrimSpace(config.APIKey) == "" {
		if *daemonMode {
			return fmt.Errorf("daemon mode requires an existing API key; run `sah auth login` first")
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
			return err
		}
	}
	agentPool := sah.ParseAgentList(*agents)
	if len(agentPool) > 0 {
		if _, err := sah.ResolveAgentPool(config, sah.WorkerOptions{
			Agents:      agentPool,
			BinaryPaths: binaryPaths,
		}); err != nil {
			return err
		}
	}
	agentModels, err := sah.ParseAgentModels(*models)
	if err != nil {
		return err
	}

	pollInterval, err := sah.ParsePollInterval(pickString(*interval, config.PollInterval))
	if err != nil {
		return err
	}
	agentTimeout, err := sah.ParseAgentTimeout(pickString(*timeout, config.AgentTimeout))
	if err != nil {
		return err
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
		Output:          os.Stdout,
		ErrorOutput:     os.Stderr,
	}

	picker, err := sah.NewAgentPicker(config, options)
	if err != nil {
		return err
	}
	if !*daemonMode {
		sah.PrintRunPlan(os.Stdout, config, options, picker.Pool())
	}

	return sah.RunWorker(ctx, config, options)
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
	if err := sah.InstallLaunchAgent(paths, executable); err != nil {
		return err
	}

	fmt.Println("Installed and started SCIENCE@home launchd agent.")
	fmt.Printf("Plist: %s\n", paths.LaunchAgentPlist)
	fmt.Printf("Logs: %s and %s\n", paths.LaunchAgentStdout, paths.LaunchAgentStderr)
	fmt.Println("Captured PATH, HOME, and installed agent binary paths for launchd. Re-run `sah daemon install` after changing agent install paths.")
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
	executable, err = preferredLaunchdExecutable(executable)
	if err != nil {
		return "", err
	}
	return executable, nil
}

func preferredLaunchdExecutable(executable string) (string, error) {
	cleaned := filepath.Clean(executable)
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return "", fmt.Errorf("resolve executable symlink: %w", err)
	}

	return selectLaunchdExecutable(resolved, []string{"/opt/homebrew/bin/sah", "/usr/local/bin/sah"})
}

func selectLaunchdExecutable(resolved string, candidates []string) (string, error) {
	if canonical, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = canonical
	}

	for _, candidate := range candidates {
		target, targetErr := filepath.EvalSymlinks(candidate)
		if targetErr != nil {
			continue
		}
		if filepath.Clean(target) == filepath.Clean(resolved) {
			return candidate, nil
		}
	}

	return filepath.Clean(resolved), nil
}

func daemonStartCmd() error {
	paths, _, err := loadConfig()
	if err != nil {
		return err
	}
	if _, err := os.Stat(paths.LaunchAgentPlist); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("launchd agent is not installed")
		}
		return err
	}

	loaded, _, err := sah.LaunchAgentStatus()
	if err != nil {
		return err
	}
	if !loaded {
		if err := sah.BootstrapLaunchAgent(paths); err != nil {
			return err
		}
	}
	if err := sah.StartLaunchAgent(); err != nil {
		return err
	}
	fmt.Println("Started SCIENCE@home launchd agent.")
	return nil
}

func daemonStopCmd() error {
	if err := sah.StopLaunchAgent(); err != nil {
		return err
	}
	fmt.Println("Stopped SCIENCE@home launchd agent.")
	return nil
}

func daemonStatusCmd() error {
	paths, config, err := loadConfig()
	if err != nil {
		return err
	}

	loaded, _, err := sah.LaunchAgentStatus()
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
	if loaded {
		fmt.Println("Launchd: loaded")
	} else {
		fmt.Println("Launchd: not loaded")
	}
	fmt.Printf("Plist: %s\n", paths.LaunchAgentPlist)
	fmt.Printf("Logs: %s and %s\n", paths.LaunchAgentStdout, paths.LaunchAgentStderr)
	return nil
}

func daemonUninstallCmd() error {
	paths, _, err := loadConfig()
	if err != nil {
		return err
	}
	if err := sah.UninstallLaunchAgent(paths); err != nil {
		return err
	}
	fmt.Println("Removed SCIENCE@home launchd agent.")
	return nil
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
	fmt.Printf("Lifetime earned: %d\n", me.LifetimeEarned)
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
	client := sah.NewClient(config.BaseURL, "")
	response, err := client.GetLeaderboard(ctx)
	if err != nil {
		return err
	}

	switch strings.ToLower(strings.TrimSpace(*window)) {
	case "all":
		printLeaderboardSection("Weekly", response.Weekly)
		fmt.Println()
		printLeaderboardSection("Monthly", response.Monthly)
		fmt.Println()
		printLeaderboardSection("All-Time", response.AllTime)
	case "weekly":
		printLeaderboardSection("Weekly", response.Weekly)
	case "monthly":
		printLeaderboardSection("Monthly", response.Monthly)
	case "all-time":
		printLeaderboardSection("All-Time", response.AllTime)
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

func printLeaderboardSection(title string, entries []sah.LeaderboardEntry) {
	fmt.Println(title)
	if len(entries) == 0 {
		fmt.Println("  (none)")
		return
	}

	writer := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "RANK\tNAME\tSCORE")
	for index, entry := range entries {
		_, _ = fmt.Fprintf(writer, "%d\t%s\t%d\n", index+1, entry.Name, entry.Earned)
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
