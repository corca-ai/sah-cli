package sah

import (
	"fmt"
	"strings"
	"testing"
)

func TestRenderLaunchAgentPlistIncludesEnvironmentAndWorkingDirectory(t *testing.T) {
	plist := renderLaunchAgentPlist(
		"/opt/homebrew/bin/sah",
		DefaultLaunchdCommand,
		"/tmp/sah-stdout.log",
		"/tmp/sah-stderr.log",
		"/Users/tester/Library/Application Support/sah",
		map[string]string{
			"HOME": "/Users/tester",
			"PATH": "/opt/homebrew/bin:/usr/bin:/bin",
		},
	)

	assertContains(t, plist, "<key>EnvironmentVariables</key>")
	assertContains(t, plist, "<key>HOME</key>")
	assertContains(t, plist, "<string>/Users/tester</string>")
	assertContains(t, plist, "<key>PATH</key>")
	assertContains(t, plist, "<string>/opt/homebrew/bin:/usr/bin:/bin</string>")
	assertContains(t, plist, "<key>WorkingDirectory</key>")
	assertContains(t, plist, "<string>/Users/tester/Library/Application Support/sah</string>")
}

func TestRenderLaunchAgentPlistEscapesXML(t *testing.T) {
	plist := renderLaunchAgentPlist(
		"/Applications/Science & Home/sah",
		DefaultLaunchdCommand,
		"/tmp/stdout&1.log",
		"/tmp/stderr<1>.log",
		"/Users/alice&bob/Library/Application Support/sah",
		map[string]string{
			"HOME": "/Users/alice&bob",
			"PATH": "/opt/homebrew/bin:/usr/bin:/bin",
		},
	)

	assertContains(t, plist, "Science &amp; Home")
	assertContains(t, plist, "/tmp/stdout&amp;1.log")
	assertContains(t, plist, "/tmp/stderr&lt;1&gt;.log")
	assertContains(t, plist, "/Users/alice&amp;bob")
	assertContains(t, plist, "/Users/alice&amp;bob/Library/Application Support/sah")
}

func TestLaunchAgentEnvironmentFallsBackToDefaultPath(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("HOME", "/Users/tester")

	environment := launchAgentEnvironment()
	if environment["PATH"] != defaultLaunchdPATH {
		t.Fatalf("expected fallback PATH %q, got %q", defaultLaunchdPATH, environment["PATH"])
	}
	if environment["HOME"] != "/Users/tester" {
		t.Fatalf("expected HOME to be preserved, got %q", environment["HOME"])
	}
}

func TestLaunchAgentStatusWithRunnerTreatsMissingServiceAsNotLoaded(t *testing.T) {
	loaded, output, err := launchAgentStatusWithRunner(func(args ...string) (string, error) {
		return "", fmt.Errorf("exit status 113: Could not find service")
	})
	if err != nil {
		t.Fatalf("launchAgentStatusWithRunner returned error: %v", err)
	}
	if loaded {
		t.Fatal("expected service to be treated as not loaded")
	}
	if output != "" {
		t.Fatalf("expected empty output, got %q", output)
	}
}

func TestIsRetryableBootstrapError(t *testing.T) {
	if !isRetryableBootstrapError(fmt.Errorf("exit status 5: Bootstrap failed: 5: Input/output error")) {
		t.Fatal("expected input/output bootstrap error to be retryable")
	}
	if isRetryableBootstrapError(fmt.Errorf("exit status 3: permission denied")) {
		t.Fatal("expected unrelated bootstrap error to not be retryable")
	}
}

func TestBootstrapLaunchAgentWithRunnerRetriesInputOutputError(t *testing.T) {
	attempts := 0
	err := bootstrapLaunchAgentWithRunner(
		Paths{LaunchAgentPlist: "/tmp/ai.borca.sah.plist"},
		func(args ...string) (string, error) {
			attempts++
			if attempts < 3 {
				return "", fmt.Errorf("exit status 5: Bootstrap failed: 5: Input/output error")
			}
			return "", nil
		},
		3,
		0,
	)
	if err != nil {
		t.Fatalf("bootstrapLaunchAgentWithRunner returned error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestWaitForLaunchAgentLoadedStateEventuallySucceeds(t *testing.T) {
	states := []bool{true, true, false}
	index := 0
	err := waitForLaunchAgentLoadedStateWithCheck(func() (bool, string, error) {
		loaded := states[index]
		if index < len(states)-1 {
			index++
		}
		return loaded, "", nil
	}, false, 3, 0)
	if err != nil {
		t.Fatalf("waitForLaunchAgentLoadedState returned error: %v", err)
	}
}

func assertContains(t *testing.T, haystack string, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected output to contain %q", needle)
	}
}
