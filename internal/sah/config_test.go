package sah

import "testing"

func TestResolvePathsForDarwinUsesLibraryDirectories(t *testing.T) {
	paths := resolvePaths("darwin", "/Users/tester/Library/Application Support", "/Users/tester", func(string) string {
		return ""
	})

	if got := paths.ConfigDir; got != "/Users/tester/Library/Application Support/sah" {
		t.Fatalf("unexpected config dir: %q", got)
	}
	if got := paths.ReleaseCacheFile; got != "/Users/tester/Library/Application Support/sah/client-release.json" {
		t.Fatalf("unexpected release cache file: %q", got)
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
	if got := paths.ReleaseCacheFile; got != "/home/tester/.config/sah/client-release.json" {
		t.Fatalf("unexpected release cache file: %q", got)
	}
	if got := paths.LogsDir; got != "/home/tester/.local/state/sah" {
		t.Fatalf("unexpected logs dir: %q", got)
	}
	if got := paths.SystemdUnitFile; got != "/home/tester/.config/systemd/user/ai.borca.sah.service" {
		t.Fatalf("unexpected systemd unit file: %q", got)
	}
}
