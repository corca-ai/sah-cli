package sah

import (
	"strings"
	"testing"
)

func TestRenderSystemdUserUnitIncludesEnvironmentAndWorkingDirectory(t *testing.T) {
	unit := renderSystemdUserUnit(
		"/usr/local/bin/sah",
		DefaultLaunchdCommand,
		"/home/tester/.config/sah",
		map[string]string{
			"HOME": "/home/tester",
			"PATH": "/usr/local/bin:/usr/bin:/bin",
		},
	)

	for _, snippet := range []string{
		`WorkingDirectory="/home/tester/.config/sah"`,
		`ExecStart="/usr/local/bin/sah" "run" "--daemon"`,
		`Environment="HOME=/home/tester"`,
		`Environment="PATH=/usr/local/bin:/usr/bin:/bin"`,
		`WantedBy=default.target`,
	} {
		if !strings.Contains(unit, snippet) {
			t.Fatalf("expected unit to contain %q, got:\n%s", snippet, unit)
		}
	}
}

func TestSystemdServiceEnvironmentFallsBackToDefaultPath(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("HOME", "/home/tester")

	environment := systemdServiceEnvironment()
	if environment["PATH"] != defaultSystemdPATH {
		t.Fatalf("expected fallback PATH %q, got %q", defaultSystemdPATH, environment["PATH"])
	}
	if environment["HOME"] != "/home/tester" {
		t.Fatalf("expected HOME to be preserved, got %q", environment["HOME"])
	}
}

func TestIsSystemdUnitMissingError(t *testing.T) {
	if !isSystemdUnitMissingError(assertError("Unit ai.borca.sah.service could not be found.")) {
		t.Fatal("expected missing systemd unit error to be detected")
	}
	if isSystemdUnitMissingError(assertError("permission denied")) {
		t.Fatal("expected unrelated systemd error to be ignored")
	}
}

func assertError(message string) error {
	return errorString(message)
}

type errorString string

func (value errorString) Error() string {
	return string(value)
}
