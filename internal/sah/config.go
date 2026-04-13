package sah

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	DefaultBaseURL        = "https://sah.borca.ai"
	DefaultAgent          = "codex"
	DefaultPollInterval   = 15 * time.Minute
	DefaultAgentTimeout   = 10 * time.Minute
	DefaultLaunchdLabel   = "ai.borca.sah"
	DefaultSystemdUnit    = DefaultLaunchdLabel + ".service"
	DefaultLaunchdCommand = "run"
)

type Config struct {
	BaseURL          string            `json:"base_url"`
	APIKey           string            `json:"api_key,omitempty"`
	DefaultAgent     string            `json:"default_agent,omitempty"`
	AgentPool        []string          `json:"agent_pool,omitempty"`
	RotateInstalled  bool              `json:"rotate_installed,omitempty"`
	AgentBinaryPaths map[string]string `json:"agent_binary_paths,omitempty"`
	PollInterval     string            `json:"poll_interval,omitempty"`
	AgentModel       string            `json:"agent_model,omitempty"`
	AgentModels      map[string]string `json:"agent_models,omitempty"`
	AgentTimeout     string            `json:"agent_timeout,omitempty"`
}

type Paths struct {
	ConfigDir         string
	ConfigFile        string
	LogsDir           string
	LaunchAgentsDir   string
	LaunchAgentPlist  string
	LaunchAgentStdout string
	LaunchAgentStderr string
	SystemdUserDir    string
	SystemdUnitFile   string
	DaemonStdoutLog   string
	DaemonStderrLog   string
}

func DefaultConfig() Config {
	return Config{
		BaseURL:      DefaultBaseURL,
		DefaultAgent: DefaultAgent,
		PollInterval: DefaultPollInterval.String(),
		AgentTimeout: DefaultAgentTimeout.String(),
	}
}

func ResolvePaths() (Paths, error) {
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve user config dir: %w", err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve home dir: %w", err)
	}

	return resolvePaths(runtime.GOOS, configRoot, homeDir, os.Getenv), nil
}

func resolvePaths(
	goos string,
	configRoot string,
	homeDir string,
	getenv func(string) string,
) Paths {
	configDir := filepath.Join(configRoot, "sah")
	logsDir := resolveLogsDir(goos, configRoot, homeDir, getenv)

	paths := Paths{
		ConfigDir:       configDir,
		ConfigFile:      filepath.Join(configDir, "config.json"),
		LogsDir:         logsDir,
		DaemonStdoutLog: filepath.Join(logsDir, "daemon.stdout.log"),
		DaemonStderrLog: filepath.Join(logsDir, "daemon.stderr.log"),
	}

	switch goos {
	case "darwin":
		launchAgentsDir := filepath.Join(homeDir, "Library", "LaunchAgents")
		paths.LaunchAgentsDir = launchAgentsDir
		paths.LaunchAgentPlist = filepath.Join(launchAgentsDir, DefaultLaunchdLabel+".plist")
		paths.LaunchAgentStdout = filepath.Join(logsDir, "stdout.log")
		paths.LaunchAgentStderr = filepath.Join(logsDir, "stderr.log")
	case "linux":
		systemdUserDir := filepath.Join(configRoot, "systemd", "user")
		paths.SystemdUserDir = systemdUserDir
		paths.SystemdUnitFile = filepath.Join(systemdUserDir, DefaultSystemdUnit)
	}

	return paths
}

func resolveLogsDir(
	goos string,
	configRoot string,
	homeDir string,
	getenv func(string) string,
) string {
	switch goos {
	case "darwin":
		return filepath.Join(homeDir, "Library", "Logs", "sah")
	case "linux":
		stateRoot := strings.TrimSpace(getenv("XDG_STATE_HOME"))
		if stateRoot == "" {
			stateRoot = filepath.Join(homeDir, ".local", "state")
		}
		return filepath.Join(stateRoot, "sah")
	default:
		return filepath.Join(configRoot, "sah", "logs")
	}
}

func LoadConfig(paths Paths) (Config, error) {
	config := DefaultConfig()
	data, err := os.ReadFile(paths.ConfigFile)
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	return normalizeConfig(config), nil
}

func SaveConfig(paths Paths, config Config) error {
	config = normalizeConfig(config)
	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')

	tempFile, err := os.CreateTemp(paths.ConfigDir, "config-*.json")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}

	tempName := tempFile.Name()
	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempName)
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tempFile.Chmod(0o600); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempName)
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempName)
		return fmt.Errorf("close temp config: %w", err)
	}

	if err := os.Rename(tempName, paths.ConfigFile); err != nil {
		_ = os.Remove(tempName)
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

func normalizeConfig(config Config) Config {
	defaults := DefaultConfig()

	config.BaseURL = normalizeBaseURL(config.BaseURL)
	if config.BaseURL == "" {
		config.BaseURL = defaults.BaseURL
	}
	if strings.TrimSpace(config.DefaultAgent) == "" {
		config.DefaultAgent = defaults.DefaultAgent
	} else {
		config.DefaultAgent = normalizeAgentName(config.DefaultAgent)
	}
	config.AgentPool = normalizeAgentPool(config.AgentPool)
	config.AgentBinaryPaths = normalizeAgentBinaryPaths(config.AgentBinaryPaths)
	config.AgentModels = normalizeAgentModels(config.AgentModels)
	if strings.TrimSpace(config.PollInterval) == "" {
		config.PollInterval = defaults.PollInterval
	}
	if strings.TrimSpace(config.AgentTimeout) == "" {
		config.AgentTimeout = defaults.AgentTimeout
	}
	return config
}

func normalizeBaseURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func normalizeAgentName(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func normalizeAgentPool(pool []string) []string {
	if len(pool) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(pool))
	seen := map[string]struct{}{}
	for _, entry := range pool {
		name := normalizeAgentName(entry)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func normalizeAgentModels(models map[string]string) map[string]string {
	if len(models) == 0 {
		return nil
	}

	normalized := make(map[string]string, len(models))
	for key, value := range models {
		name := normalizeAgentName(key)
		model := strings.TrimSpace(value)
		if name == "" || model == "" {
			continue
		}
		normalized[name] = model
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func normalizeAgentBinaryPaths(paths map[string]string) map[string]string {
	if len(paths) == 0 {
		return nil
	}

	normalized := make(map[string]string, len(paths))
	for key, value := range paths {
		name := normalizeAgentName(key)
		path := filepath.Clean(strings.TrimSpace(value))
		if name == "" || path == "." || path == "" {
			continue
		}
		normalized[name] = path
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func ParsePollInterval(raw string) (time.Duration, error) {
	return parseDurationWithDefault(raw, DefaultPollInterval)
}

func ParseAgentTimeout(raw string) (time.Duration, error) {
	return parseDurationWithDefault(raw, DefaultAgentTimeout)
}

func parseDurationWithDefault(raw string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", value, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("duration must be positive: %s", value)
	}
	return duration, nil
}
