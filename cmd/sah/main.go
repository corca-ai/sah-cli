package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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
const maxContributionsLimit = 100

type reportedError struct {
	err     error
	code    int
	codeSet bool
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

func (err *reportedError) ExitCode() int {
	if err == nil {
		return 1
	}
	if err.codeSet {
		return err.code
	}
	return 1
}

func main() {
	sah.SetCLIVersion(version)
	commandKey, err := runCLI(os.Args[1:])
	if err == nil {
		if shouldPrintCommandSuccessHints(commandKey) {
			printCommandSuccessHints(os.Stdout, inspectCLIState(), commandKey)
		}
		return
	}

	var reported *reportedError
	if errors.As(err, &reported) {
		os.Exit(reported.ExitCode())
	}
	printCommandFailure(os.Stderr, inspectCLIState(), commandKey, err)
	os.Exit(1)
}

func runCLI(args []string) (string, error) {
	if len(args) == 0 {
		printEntryExperience(os.Stdout, inspectCLIState())
		return "", nil
	}
	if isHelpToken(args[0]) {
		printHelp(os.Stdout, strings.Join(args[1:], " "), inspectCLIState())
		return "", nil
	}

	commandKey := canonicalCommandKey(args)
	if commandKey == "version" {
		return commandKey, executeTopLevelCommand(args)
	}
	if requiresSupportedWorkerContract(commandKey) {
		if err := enforceSupportedWorkerContract(); err != nil {
			return commandKey, err
		}
	}
	return commandKey, executeTopLevelCommand(args)
}

func executeTopLevelCommand(args []string) error {
	switch args[0] {
	case "auth":
		return authCmd(args[1:])
	case "run":
		return runCmd(args[1:])
	case "daemon":
		return daemonCmd(args[1:])
	case "me":
		return meCmd(args[1:])
	case "contributions":
		return contributionsCmd(args[1:])
	case "leaderboard":
		return leaderboardCmd(args[1:])
	case "agents":
		return agentsCmd(args[1:])
	case "version", "--version", "-version":
		fmt.Println(version)
		return nil
	default:
		printUnknownCommand(os.Stderr, args[0], inspectCLIState())
		return &reportedError{code: 2, codeSet: true}
	}
}

func authCmd(args []string) error {
	if len(args) == 0 || isHelpToken(args[0]) {
		printHelp(os.Stdout, "auth", inspectCLIState())
		return &reportedError{code: 0, codeSet: true}
	}

	switch args[0] {
	case "login":
		return runAuthLogin(args[1:])
	case "logout":
		return runAuthLogout()
	case "status":
		return runAuthStatus()
	default:
		printUnknownSubcommand(os.Stderr, "auth", args[0], inspectCLIState())
		return &reportedError{code: 2, codeSet: true}
	}
}

func runAuthLogin(args []string) error {
	baseURL, err := parseAuthLoginFlags(args)
	if err != nil {
		return err
	}

	ctx, cancel := signalContext()
	defer cancel()

	paths, config, err := loadConfig()
	if err != nil {
		return err
	}
	if strings.TrimSpace(baseURL) != "" {
		config.BaseURL = baseURL
	}
	if err := loginAndPersist(ctx, paths, &config); err != nil {
		return err
	}
	printAuthLoginSuccess(ctx, paths, config)
	return nil
}

func parseAuthLoginFlags(args []string) (string, error) {
	fs := flag.NewFlagSet("auth login", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	baseURL := fs.String("base-url", "", "SCIENCE@home base URL")
	fs.Bool("no-open", false, "Deprecated compatibility flag; sah always prints the verification URL")
	if err := fs.Parse(args); err != nil {
		return "", handleFlagParseError(err)
	}
	if err := validateBaseURLFlag(*baseURL); err != nil {
		return "", err
	}
	return *baseURL, nil
}

func printAuthLoginSuccess(ctx context.Context, paths sah.Paths, config sah.Config) {
	client := sah.NewConfigClient(paths, &config)
	me, err := client.GetMe(ctx)
	if err != nil {
		fmt.Println("Authenticated successfully.")
		return
	}

	fmt.Printf("Authenticated as %s <%s>.\n", me.PreferredName(), me.Email)
	fmt.Printf("Rank: #%d, credits: %d, pending: %d\n", me.Rank, me.Credits, me.PendingCredits)
}

func runAuthLogout() error {
	paths, config, err := loadConfig()
	if err != nil {
		return err
	}
	config.APIKey = ""
	config.AccessToken = ""
	config.RefreshToken = ""
	config.TokenType = ""
	config.TokenExpiry = ""
	config.OAuthIssuer = ""
	if err := sah.SaveConfig(paths, config); err != nil {
		return err
	}
	fmt.Println("Removed local SCIENCE@home credentials.")
	return nil
}

func runAuthStatus() error {
	paths, config, err := loadConfig()
	if err != nil {
		return err
	}
	fmt.Printf("Base URL: %s\n", config.BaseURL)
	if !config.HasAuth() {
		fmt.Println("Authentication: not logged in")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return printResolvedAuthStatus(ctx, paths, config)
}

func printResolvedAuthStatus(ctx context.Context, paths sah.Paths, config sah.Config) error {
	client := sah.NewConfigClient(paths, &config)
	me, err := client.GetMe(ctx)
	switch {
	case err == nil:
		fmt.Printf("Authentication: logged in as %s <%s>\n", me.PreferredName(), me.Email)
		fmt.Printf("Rank: #%d, credits: %d, pending: %d\n", me.Rank, me.Credits, me.PendingCredits)
		return nil
	case sah.IsAuthenticationFailure(err):
		fmt.Println("Authentication: stored credential exists but was rejected by the server")
		return nil
	default:
		return err
	}
}

func runCmd(args []string) error {
	options, err := parseRunCommandOptions(args)
	if err != nil {
		return err
	}
	if !options.DaemonMode {
		sah.PrintRunBanner(os.Stdout)
	}

	ctx, cancel := signalContext()
	defer cancel()

	session, cleanup, err := prepareRunSession(ctx, options)
	if err != nil {
		return err
	}
	defer cleanup()

	workerOptions, picker, err := buildRunWorkerSession(session, options)
	if err != nil {
		return session.report(err)
	}
	if !options.DaemonMode {
		sah.PrintRunPlan(os.Stdout, session.config, workerOptions, picker.Pool())
	}
	return session.report(sah.RunWorker(ctx, session.paths, session.config, workerOptions))
}

type runCommandOptions struct {
	Agent           string
	Agents          string
	AgentsSpecified bool
	RotateInstalled bool
	Model           string
	Models          string
	Interval        string
	Timeout         string
	TaskType        string
	BaseURL         string
	Once            bool
	DaemonMode      bool
}

type runSession struct {
	paths       sah.Paths
	config      sah.Config
	binaryPaths map[string]string
	report      func(error) error
	output      io.Writer
	errorOutput io.Writer
}

func parseRunCommandOptions(args []string) (runCommandOptions, error) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	options := runCommandOptions{}
	fs.StringVar(&options.Agent, "agent", "", "Agent CLI to use: codex, gemini, claude, qwen")
	fs.StringVar(&options.Agents, "agents", "", "Comma-separated round-robin agent order, e.g. codex,gemini,claude,qwen")
	fs.BoolVar(&options.RotateInstalled, "rotate-installed", false, "Rotate through every supported agent CLI installed on this Mac")
	fs.StringVar(&options.Model, "model", "", "Optional model override passed to the agent CLI")
	fs.StringVar(&options.Models, "models", "", "Per-agent model overrides, e.g. codex=gpt-5.4-mini,gemini=gemini-3-flash-base,claude=sonnet,qwen=<name>")
	fs.StringVar(&options.Interval, "interval", "", "Polling interval")
	fs.StringVar(&options.Timeout, "timeout", "", "Per-assignment agent timeout")
	fs.StringVar(&options.TaskType, "task-type", "", "Optional task type filter")
	fs.StringVar(&options.BaseURL, "base-url", "", "SCIENCE@home base URL")
	fs.BoolVar(&options.Once, "once", false, "Run a single polling cycle and exit")
	fs.BoolVar(&options.DaemonMode, "daemon", false, "Run non-interactively for the background service")

	if err := fs.Parse(args); err != nil {
		return runCommandOptions{}, handleFlagParseError(err)
	}
	options.AgentsSpecified = flagWasProvided(fs, "agents")
	if err := validateAgentsFlag(options.Agents, options.AgentsSpecified); err != nil {
		return runCommandOptions{}, err
	}
	if err := validateAgentFlags(options.Agent, options.Agents, options.RotateInstalled); err != nil {
		return runCommandOptions{}, err
	}
	if err := validateBaseURLFlag(options.BaseURL); err != nil {
		return runCommandOptions{}, err
	}
	return options, nil
}

func prepareRunSession(ctx context.Context, options runCommandOptions) (runSession, func(), error) {
	paths, config, err := loadConfig()
	if err != nil {
		return runSession{}, nil, err
	}
	if strings.TrimSpace(options.BaseURL) != "" {
		config.BaseURL = options.BaseURL
	}

	outputWriter, errorWriter, report, cleanup, err := prepareRunOutputs(paths, options.DaemonMode)
	if err != nil {
		return runSession{}, nil, err
	}
	session := runSession{
		paths:       paths,
		config:      config,
		binaryPaths: nil,
		report:      report,
		output:      outputWriter,
		errorOutput: errorWriter,
	}

	if err := ensureRunAuthentication(ctx, paths, &session.config, options.DaemonMode, report); err != nil {
		cleanup()
		return runSession{}, nil, err
	}
	if options.DaemonMode {
		session.binaryPaths = session.config.AgentBinaryPaths
	}
	return session, cleanup, nil
}

func prepareRunOutputs(
	paths sah.Paths,
	daemonMode bool,
) (io.Writer, io.Writer, func(error) error, func(), error) {
	outputWriter := io.Writer(os.Stdout)
	errorWriter := io.Writer(os.Stderr)
	report := func(runErr error) error {
		return runErr
	}
	cleanup := func() {}
	if !daemonMode {
		return outputWriter, errorWriter, report, cleanup, nil
	}

	daemonLogs, err := sah.OpenDaemonLogs(paths)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	outputWriter = daemonLogs.Stdout
	errorWriter = daemonLogs.Stderr
	report = func(runErr error) error {
		if runErr == nil {
			return nil
		}
		_, _ = fmt.Fprintf(errorWriter, "[%s] sah: %v\n", time.Now().Format(time.RFC3339), runErr)
		return &reportedError{err: runErr}
	}
	cleanup = func() {
		_ = daemonLogs.Close()
	}
	return outputWriter, errorWriter, report, cleanup, nil
}

func ensureRunAuthentication(
	ctx context.Context,
	paths sah.Paths,
	config *sah.Config,
	daemonMode bool,
	report func(error) error,
) error {
	if config.HasAuth() {
		return nil
	}
	if daemonMode {
		return report(fmt.Errorf("daemon mode requires an existing credential; run `sah auth login` first"))
	}
	return loginAndPersist(ctx, paths, config)
}

func buildRunWorkerSession(
	session runSession,
	commandOptions runCommandOptions,
) (sah.WorkerOptions, *sah.AgentPicker, error) {
	agentPool, err := resolveRunAgentPool(session.config, commandOptions, session.binaryPaths)
	if err != nil {
		return sah.WorkerOptions{}, nil, err
	}
	workerOptions, err := buildRunWorkerOptions(session, commandOptions, agentPool)
	if err != nil {
		return sah.WorkerOptions{}, nil, err
	}
	picker, err := sah.NewAgentPicker(session.config, workerOptions)
	if err != nil {
		return sah.WorkerOptions{}, nil, err
	}
	return workerOptions, picker, nil
}

func resolveRunAgentPool(
	config sah.Config,
	commandOptions runCommandOptions,
	binaryPaths map[string]string,
) ([]string, error) {
	if strings.TrimSpace(commandOptions.Agent) != "" {
		if _, err := sah.ResolveAgentWithBinaryPaths(commandOptions.Agent, binaryPaths); err != nil {
			return nil, err
		}
	}

	if err := validateAgentsFlag(commandOptions.Agents, commandOptions.AgentsSpecified); err != nil {
		return nil, err
	}
	agentPool := sah.ParseAgentList(commandOptions.Agents)
	if len(agentPool) == 0 {
		return nil, nil
	}
	if _, err := sah.ResolveAgentPool(config, sah.WorkerOptions{
		Agents:      agentPool,
		BinaryPaths: binaryPaths,
	}); err != nil {
		return nil, err
	}
	return agentPool, nil
}

func buildRunWorkerOptions(
	session runSession,
	commandOptions runCommandOptions,
	agentPool []string,
) (sah.WorkerOptions, error) {
	config := session.config
	agentModels, err := sah.ParseAgentModels(commandOptions.Models)
	if err != nil {
		return sah.WorkerOptions{}, err
	}
	pollInterval, err := sah.ParsePollInterval(pickString(commandOptions.Interval, config.PollInterval))
	if err != nil {
		return sah.WorkerOptions{}, err
	}
	agentTimeout, err := sah.ParseAgentTimeout(pickString(commandOptions.Timeout, config.AgentTimeout))
	if err != nil {
		return sah.WorkerOptions{}, err
	}

	return sah.WorkerOptions{
		Agent:           strings.TrimSpace(commandOptions.Agent),
		Agents:          agentPool,
		RotateInstalled: commandOptions.RotateInstalled,
		BinaryPaths:     session.binaryPaths,
		Model:           pickString(commandOptions.Model, config.AgentModel),
		Models:          sah.MergeAgentModels(config.AgentModels, agentModels),
		Interval:        pollInterval,
		Timeout:         agentTimeout,
		TaskType:        strings.TrimSpace(commandOptions.TaskType),
		Once:            commandOptions.Once,
		Output:          session.output,
		ErrorOutput:     session.errorOutput,
	}, nil
}

func daemonCmd(args []string) error {
	if len(args) == 0 || isHelpToken(args[0]) {
		printHelp(os.Stdout, "daemon", inspectCLIState())
		return &reportedError{code: 0, codeSet: true}
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
		printUnknownSubcommand(os.Stderr, "daemon", args[0], inspectCLIState())
		return &reportedError{code: 2, codeSet: true}
	}
}

type daemonInstallOptions struct {
	agent           string
	agents          string
	agentsSpecified bool
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
	capturedBinaryPaths := sah.CaptureInstalledAgentBinaryPaths()
	if err := applyDaemonInstallOptions(&config, options, capturedBinaryPaths); err != nil {
		return daemonAgentSelectionError(err)
	}
	config.AgentBinaryPaths = capturedBinaryPaths

	daemonPool, err := sah.ResolveAgentPool(config, sah.WorkerOptions{
		BinaryPaths: capturedBinaryPaths,
	})
	if err != nil {
		return daemonAgentSelectionError(err)
	}

	if err := ensureDaemonInstallAuthentication(ctx, paths, &config); err != nil {
		return err
	}
	if err := sah.SaveConfig(paths, config); err != nil {
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
	fmt.Printf("Daemon agent order: %s\n", joinAgentNames(daemonPool))
	fmt.Printf("%s: %s\n", sah.ServiceDefinitionLabel(), sah.ServiceDefinitionPath(paths))
	fmt.Printf("Daemon logs: %s and %s\n", paths.DaemonStdoutLog, paths.DaemonStderrLog)
	if capture := sah.ServiceCaptureValue(paths); capture != "" {
		fmt.Printf("%s: %s\n", sah.ServiceCaptureLabel(), capture)
	}
	fmt.Printf("Captured PATH, HOME, and installed agent binary paths for %s. Re-run `sah daemon install` after changing agent install paths.\n", sah.ServiceManagerName())
	fmt.Println()
	printDaemonWelcome(config.BaseURL)
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
		return daemonInstallOptions{}, handleFlagParseError(err)
	}
	options.agentsSpecified = flagWasProvided(fs, "agents")
	if err := validateAgentsFlag(options.agents, options.agentsSpecified); err != nil {
		return daemonInstallOptions{}, err
	}
	if err := validateAgentFlags(options.agent, options.agents, options.rotateInstalled); err != nil {
		return daemonInstallOptions{}, err
	}
	if err := validateBaseURLFlag(options.baseURL); err != nil {
		return daemonInstallOptions{}, err
	}
	return options, nil
}

func applyDaemonInstallOptions(
	config *sah.Config,
	options daemonInstallOptions,
	binaryPaths map[string]string,
) error {
	if err := applyDaemonBaseURL(config, options); err != nil {
		return err
	}
	if err := applyDaemonAgentSelection(config, options, binaryPaths); err != nil {
		return err
	}
	if err := applyDaemonModelOptions(config, options); err != nil {
		return err
	}
	return applyDaemonTimingOptions(config, options)
}

func ensureDaemonInstallAuthentication(ctx context.Context, paths sah.Paths, config *sah.Config) error {
	if config == nil || !config.HasAuth() {
		return fmt.Errorf("daemon install requires an existing credential; run `sah auth login` first")
	}
	client := sah.NewConfigClient(paths, config)
	if _, err := client.GetMe(ctx); err != nil {
		if sah.IsAuthenticationFailure(err) {
			return fmt.Errorf("stored credential rejected; run `sah auth login` again")
		}
		return err
	}
	return nil
}

func applyDaemonBaseURL(config *sah.Config, options daemonInstallOptions) error {
	if strings.TrimSpace(options.baseURL) != "" {
		if err := sah.ValidateBaseURL(options.baseURL); err != nil {
			return fmt.Errorf("--base-url: %w", err)
		}
		config.BaseURL = options.baseURL
	}
	return nil
}

func applyDaemonAgentSelection(
	config *sah.Config,
	options daemonInstallOptions,
	binaryPaths map[string]string,
) error {
	selectionSpecified, err := applyExplicitDaemonSelection(config, options, binaryPaths)
	if err != nil {
		return err
	}
	if selectionSpecified || !shouldAutoRotateInstalledAgents(*config) {
		return nil
	}
	return setDaemonRotateInstalled(config, binaryPaths)
}

func applyExplicitDaemonSelection(
	config *sah.Config,
	options daemonInstallOptions,
	binaryPaths map[string]string,
) (bool, error) {
	if strings.TrimSpace(options.agent) != "" {
		if err := setDaemonPinnedAgent(config, options.agent, binaryPaths); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := validateAgentsFlag(options.agents, options.agentsSpecified); err != nil {
		return false, err
	}
	if pool := sah.ParseAgentList(options.agents); len(pool) > 0 {
		if err := setDaemonAgentPool(config, pool, binaryPaths); err != nil {
			return false, err
		}
		return true, nil
	}
	if options.rotateInstalled {
		return true, setDaemonRotateInstalled(config, binaryPaths)
	}
	return false, nil
}

func setDaemonPinnedAgent(config *sah.Config, agent string, binaryPaths map[string]string) error {
	if _, err := sah.ResolveAgentWithBinaryPaths(agent, binaryPaths); err != nil {
		return err
	}
	config.DefaultAgent = agent
	config.AgentPool = nil
	config.RotateInstalled = false
	return nil
}

func setDaemonAgentPool(config *sah.Config, pool []string, binaryPaths map[string]string) error {
	if _, err := sah.ResolveAgentPool(*config, sah.WorkerOptions{
		Agents:      pool,
		BinaryPaths: binaryPaths,
	}); err != nil {
		return err
	}
	config.AgentPool = pool
	config.RotateInstalled = false
	return nil
}

func setDaemonRotateInstalled(config *sah.Config, binaryPaths map[string]string) error {
	if _, err := sah.ResolveAgentPool(*config, sah.WorkerOptions{
		RotateInstalled: true,
		BinaryPaths:     binaryPaths,
	}); err != nil {
		return err
	}
	config.AgentPool = nil
	config.RotateInstalled = true
	return nil
}

func applyDaemonModelOptions(config *sah.Config, options daemonInstallOptions) error {
	if strings.TrimSpace(options.model) != "" {
		config.AgentModel = options.model
	}
	parsedModels, err := sah.ParseAgentModels(options.models)
	if err != nil {
		return err
	}
	if parsedModels != nil {
		config.AgentModels = parsedModels
	}
	return nil
}

func applyDaemonTimingOptions(config *sah.Config, options daemonInstallOptions) error {
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

func shouldAutoRotateInstalledAgents(config sah.Config) bool {
	if len(config.AgentPool) > 0 || config.RotateInstalled {
		return false
	}
	if strings.TrimSpace(config.AgentModel) != "" {
		return false
	}
	defaultAgent := strings.ToLower(strings.TrimSpace(config.DefaultAgent))
	return defaultAgent == "" || defaultAgent == sah.DefaultAgent
}

func daemonAgentSelectionError(err error) error {
	return fmt.Errorf("cannot install daemon: %w", err)
}

func joinAgentNames(pool []sah.AgentSpec) string {
	names := make([]string, 0, len(pool))
	for _, agent := range pool {
		names = append(names, agent.Name)
	}
	return strings.Join(names, ", ")
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
		return handleFlagParseError(err)
	}
	if err := validateBaseURLFlag(*baseURL); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	paths, config, err := loadConfig()
	if err != nil {
		return err
	}
	if strings.TrimSpace(*baseURL) != "" {
		config.BaseURL = *baseURL
	}
	client, err := authedClient(paths, config)
	if err != nil {
		return err
	}
	me, err := client.GetMe(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("Name: %s\n", me.PreferredName())
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
		return handleFlagParseError(err)
	}
	if err := validateContributionsLimit(*limit); err != nil {
		return err
	}
	if err := validateBaseURLFlag(*baseURL); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	paths, config, err := loadConfig()
	if err != nil {
		return err
	}
	if strings.TrimSpace(*baseURL) != "" {
		config.BaseURL = *baseURL
	}
	client, err := authedClient(paths, config)
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

func validateContributionsLimit(limit int) error {
	if limit < 1 || limit > maxContributionsLimit {
		return fmt.Errorf("--limit must be between 1 and %d", maxContributionsLimit)
	}
	return nil
}

func leaderboardCmd(args []string) error {
	options, err := parseLeaderboardOptions(args)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	paths, config, err := loadConfig()
	if err != nil {
		return err
	}
	if strings.TrimSpace(options.baseURL) != "" {
		config.BaseURL = options.baseURL
	}
	hadAuth := config.HasAuth()
	client := sah.NewCachedConfigClient(paths, &config)
	response, err := client.GetLeaderboard(ctx)
	if err != nil && hadAuth && sah.IsAuthenticationFailure(err) {
		response, err = sah.NewCachedClient(paths, config.BaseURL, "").GetLeaderboard(ctx)
	}
	if err != nil {
		return err
	}

	switch options.window {
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
		return fmt.Errorf("unsupported window %q", options.window)
	}
	return nil
}

type leaderboardOptions struct {
	window  string
	baseURL string
}

func parseLeaderboardOptions(args []string) (leaderboardOptions, error) {
	fs := flag.NewFlagSet("leaderboard", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	window := fs.String("window", "all", "all, weekly, monthly, all-time")
	baseURL := fs.String("base-url", "", "SCIENCE@home base URL")
	if err := fs.Parse(args); err != nil {
		return leaderboardOptions{}, handleFlagParseError(err)
	}
	normalizedWindow, err := normalizeLeaderboardWindow(*window)
	if err != nil {
		return leaderboardOptions{}, err
	}
	if err := validateBaseURLFlag(*baseURL); err != nil {
		return leaderboardOptions{}, err
	}
	return leaderboardOptions{window: normalizedWindow, baseURL: *baseURL}, nil
}

func normalizeLeaderboardWindow(raw string) (string, error) {
	window := strings.ToLower(strings.TrimSpace(raw))
	switch window {
	case "all", "weekly", "monthly", "all-time":
		return window, nil
	default:
		return "", fmt.Errorf("unsupported window %q", raw)
	}
}

func agentsCmd(args []string) error {
	fs := flag.NewFlagSet("agents", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return handleFlagParseError(err)
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
) error {
	response, err := sah.Login(ctx, sah.LoginOptions{
		BaseURL: config.BaseURL,
		Paths:   paths,
		Output:  os.Stdout,
	})
	if err != nil {
		return err
	}

	config.APIKey = ""
	config.AccessToken = response.AccessToken
	config.RefreshToken = response.RefreshToken
	config.TokenType = response.TokenType
	if config.TokenType == "" && config.AccessToken != "" {
		config.TokenType = "Bearer"
	}
	if response.ExpiresIn > 0 {
		config.TokenExpiry = time.Now().UTC().Add(time.Duration(response.ExpiresIn) * time.Second).Format(time.RFC3339)
	} else {
		config.TokenExpiry = ""
	}
	config.OAuthClientID = sah.DefaultOAuthClientID
	return sah.SaveConfig(paths, *config)
}

func authedClient(paths sah.Paths, config sah.Config) (*sah.Client, error) {
	if !config.HasAuth() {
		return nil, fmt.Errorf("not authenticated; run `sah auth login` first")
	}
	return sah.NewCachedConfigClient(paths, &config), nil
}

func sciHomeURL(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		return sah.DefaultBaseURL
	}
	return trimmed
}

func printDaemonWelcome(baseURL string) {
	homeURL := sciHomeURL(baseURL)
	fmt.Println("Welcome to SCIENCE@home. This machine is now linked and the background worker is running.")
	fmt.Printf("The home dashboard at %s will switch over after your first task is claimed.\n", homeURL)
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

func validateAgentsFlag(raw string, specified bool) error {
	if !specified && strings.TrimSpace(raw) == "" {
		return nil
	}
	if len(sah.ParseAgentList(raw)) == 0 {
		return fmt.Errorf("--agents must include at least one agent")
	}
	return nil
}

func validateBaseURLFlag(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if err := sah.ValidateBaseURL(raw); err != nil {
		return fmt.Errorf("--base-url: %w", err)
	}
	return nil
}

func flagWasProvided(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(flag *flag.Flag) {
		if flag.Name == name {
			found = true
		}
	})
	return found
}

func handleFlagParseError(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return &reportedError{code: 0, codeSet: true}
	}
	return err
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func requiresSupportedWorkerContract(commandKey string) bool {
	switch commandKey {
	case "run", "daemon install", "daemon start":
		return true
	default:
		return false
	}
}

func enforceSupportedWorkerContract() error {
	paths, config, err := loadConfig()
	if err != nil {
		return err
	}
	release, releaseErr := resolveClientRelease(paths, config.BaseURL)
	if releaseErr == nil && release != nil {
		if violation := sah.ResolveWorkerContractViolation(release); violation != nil {
			return violation
		}
	}
	return nil
}
