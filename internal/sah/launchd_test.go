package sah

import (
	"strings"
	"testing"
)

func TestRenderLaunchAgentPlistIncludesEnvironmentAndWorkingDirectory(t *testing.T) {
	plist := renderLaunchAgentPlist(
		"/opt/homebrew/bin/sah",
		DefaultLaunchdCommand,
		"/tmp/sah-stdout.log",
		"/tmp/sah-stderr.log",
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
	assertContains(t, plist, "<string>/Users/")
}

func TestRenderLaunchAgentPlistEscapesXML(t *testing.T) {
	plist := renderLaunchAgentPlist(
		"/Applications/Science & Home/sah",
		DefaultLaunchdCommand,
		"/tmp/stdout&1.log",
		"/tmp/stderr<1>.log",
		map[string]string{
			"HOME": "/Users/alice&bob",
			"PATH": "/opt/homebrew/bin:/usr/bin:/bin",
		},
	)

	assertContains(t, plist, "Science &amp; Home")
	assertContains(t, plist, "/tmp/stdout&amp;1.log")
	assertContains(t, plist, "/tmp/stderr&lt;1&gt;.log")
	assertContains(t, plist, "/Users/alice&amp;bob")
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

func assertContains(t *testing.T, haystack string, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected output to contain %q", needle)
	}
}
