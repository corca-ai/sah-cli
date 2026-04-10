package sah

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultBaseURL        = "https://sah.borca.ai"
	DefaultAgent          = "codex"
	DefaultPollInterval   = 15 * time.Minute
	DefaultAgentTimeout   = 10 * time.Minute
	DefaultLaunchdLabel   = "ai.borca.sah"
	DefaultLaunchdCommand = "run"
)

type Config struct {
	BaseURL      string `json:"base_url"`
	APIKey       string `json:"api_key,omitempty"`
	DefaultAgent string `json:"default_agent,omitempty"`
	PollInterval string `json:"poll_interval,omitempty"`
	AgentModel   string `json:"agent_model,omitempty"`
	AgentTimeout string `json:"agent_timeout,omitempty"`
}

type Paths struct {
	ConfigDir         string
	ConfigFile        string
	LogsDir           string
	LaunchAgentsDir   string
	LaunchAgentPlist  string
	LaunchAgentStdout string
	LaunchAgentStderr string
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

	configDir := filepath.Join(configRoot, "sah")
	logsDir := filepath.Join(homeDir, "Library", "Logs", "sah")
	launchAgentsDir := filepath.Join(homeDir, "Library", "LaunchAgents")
	return Paths{
		ConfigDir:         configDir,
		ConfigFile:        filepath.Join(configDir, "config.json"),
		LogsDir:           logsDir,
		LaunchAgentsDir:   launchAgentsDir,
		LaunchAgentPlist:  filepath.Join(launchAgentsDir, DefaultLaunchdLabel+".plist"),
		LaunchAgentStdout: filepath.Join(logsDir, "stdout.log"),
		LaunchAgentStderr: filepath.Join(logsDir, "stderr.log"),
	}, nil
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
		tempFile.Close()
		_ = os.Remove(tempName)
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tempFile.Chmod(0o600); err != nil {
		tempFile.Close()
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
	}
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
