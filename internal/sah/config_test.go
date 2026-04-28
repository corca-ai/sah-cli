package sah

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolvePathsForDarwinUsesLibraryDirectories(t *testing.T) {
	paths := resolvePaths("darwin", "/Users/tester/Library/Application Support", "/Users/tester", func(string) string {
		return ""
	})

	if got := paths.ConfigDir; got != "/Users/tester/Library/Application Support/sah" {
		t.Fatalf("unexpected config dir: %q", got)
	}
	if got := paths.HTTPCacheDir; got != "/Users/tester/Library/Application Support/sah/http-cache" {
		t.Fatalf("unexpected http cache dir: %q", got)
	}
	if got := paths.LogsDir; got != "/Users/tester/Library/Logs/sah" {
		t.Fatalf("unexpected logs dir: %q", got)
	}
	if got := paths.LaunchAgentPlist; got != "/Users/tester/Library/LaunchAgents/ai.borca.sah.plist" {
		t.Fatalf("unexpected launchd plist: %q", got)
	}
}

func TestResolvePathsForLinuxUsesXDGStateAndSystemdUserDir(t *testing.T) {
	paths := resolvePaths("linux", "/home/tester/.config", "/home/tester", func(key string) string {
		if key == "XDG_STATE_HOME" {
			return "/home/tester/.local/state"
		}
		return ""
	})

	if got := paths.ConfigDir; got != "/home/tester/.config/sah" {
		t.Fatalf("unexpected config dir: %q", got)
	}
	if got := paths.HTTPCacheDir; got != "/home/tester/.config/sah/http-cache" {
		t.Fatalf("unexpected http cache dir: %q", got)
	}
	if got := paths.LogsDir; got != "/home/tester/.local/state/sah" {
		t.Fatalf("unexpected logs dir: %q", got)
	}
	if got := paths.SystemdUnitFile; got != "/home/tester/.config/systemd/user/ai.borca.sah.service" {
		t.Fatalf("unexpected systemd unit file: %q", got)
	}
}

func TestNormalizeConfigDefaultsOAuthClientID(t *testing.T) {
	config := normalizeConfig(Config{})
	if got := config.OAuthClientID; got != DefaultOAuthClientID {
		t.Fatalf("unexpected oauth client id: %q", got)
	}
}

func TestConfigHasAuthAcceptsBearerTokens(t *testing.T) {
	if !(Config{AccessToken: "access-token"}).HasAuth() {
		t.Fatal("expected bearer token config to count as authenticated")
	}
	if !(Config{APIKey: "api-key"}).HasAuth() {
		t.Fatal("expected api-key config to count as authenticated")
	}
	if (Config{}).HasAuth() {
		t.Fatal("did not expect empty config to count as authenticated")
	}
}

func TestConfigParsedTokenExpiry(t *testing.T) {
	expiry := time.Date(2026, 4, 15, 4, 5, 6, 0, time.UTC)
	got := Config{TokenExpiry: expiry.Format(time.RFC3339)}.ParsedTokenExpiry()
	if !got.Equal(expiry) {
		t.Fatalf("unexpected token expiry: %v", got)
	}
}

func TestValidateBaseURL(t *testing.T) {
	for _, value := range []string{
		DefaultBaseURL,
		"http://localhost:8000",
		" https://sah.example/ ",
	} {
		if err := ValidateBaseURL(value); err != nil {
			t.Fatalf("expected base URL %q to be valid, got %v", value, err)
		}
	}

	for _, value := range []string{
		"",
		"localhost:8000",
		"ftp://sah.example",
		"https:///sah",
		"https://user:pass@sah.example",
	} {
		if err := ValidateBaseURL(value); err == nil {
			t.Fatalf("expected base URL %q to be rejected", value)
		}
	}
}

func TestLoadConfigRejectsInvalidBaseURL(t *testing.T) {
	configDir := t.TempDir()
	paths := Paths{
		ConfigDir:  configDir,
		ConfigFile: filepath.Join(configDir, "config.json"),
	}
	if err := os.WriteFile(paths.ConfigFile, []byte(`{"base_url":"localhost:8000"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := LoadConfig(paths); err == nil {
		t.Fatal("expected invalid stored base_url to be rejected")
	}
}

func TestSaveConfigRejectsInvalidBaseURL(t *testing.T) {
	configDir := t.TempDir()
	paths := Paths{
		ConfigDir:  configDir,
		ConfigFile: filepath.Join(configDir, "config.json"),
	}

	err := SaveConfig(paths, Config{BaseURL: "localhost:8000"})
	if err == nil {
		t.Fatal("expected invalid base_url to be rejected")
	}
	if _, statErr := os.Stat(paths.ConfigFile); !os.IsNotExist(statErr) {
		t.Fatalf("expected invalid config not to be written, got stat err=%v", statErr)
	}
}
