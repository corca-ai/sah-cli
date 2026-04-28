package sah

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

const defaultSystemdPATH = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

func InstallSystemdUserService(paths Paths, executable string) error {
	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.MkdirAll(paths.SystemdUserDir, 0o755); err != nil {
		return fmt.Errorf("create systemd user dir: %w", err)
	}
	if err := os.MkdirAll(paths.LogsDir, 0o755); err != nil {
		return fmt.Errorf("create logs dir: %w", err)
	}

	unit := renderSystemdUserUnit(
		executable,
		DefaultLaunchdCommand,
		paths.ConfigDir,
		systemdServiceEnvironment(),
	)
	if err := os.WriteFile(paths.SystemdUnitFile, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("write systemd unit: %w", err)
	}

	if _, err := runSystemctlUser("daemon-reload"); err != nil {
		return fmt.Errorf("reload systemd user units: %w", err)
	}
	if _, err := runSystemctlUser("enable", DefaultSystemdUnit); err != nil {
		return fmt.Errorf("enable systemd user service: %w", err)
	}
	if _, err := runSystemctlUser("restart", DefaultSystemdUnit); err != nil {
		return fmt.Errorf("restart systemd user service: %w", err)
	}
	return nil
}

func renderSystemdUserUnit(
	executable string,
	command string,
	workingDirectory string,
	environment map[string]string,
) string {
	var envLines strings.Builder
	keys := make([]string, 0, len(environment))
	for key, value := range environment {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		envLines.WriteString("Environment=")
		envLines.WriteString(systemdQuote(key + "=" + environment[key]))
		envLines.WriteString("\n")
	}

	return fmt.Sprintf(`[Unit]
Description=SCIENCE@home background worker

[Service]
Type=simple
WorkingDirectory=%s
ExecStart=%s %s %s
Restart=always
RestartSec=5
%s
[Install]
WantedBy=default.target
`,
		systemdQuote(workingDirectory),
		systemdQuote(executable),
		systemdQuote(command),
		systemdQuote("--daemon"),
		envLines.String(),
	)
}

func systemdServiceEnvironment() map[string]string {
	environment := map[string]string{
		"PATH": defaultSystemdPATH,
	}
	for _, key := range []string{
		"PATH",
		"HOME",
		"SHELL",
		"LANG",
		"LC_ALL",
		"LC_CTYPE",
		"XDG_CONFIG_HOME",
		"XDG_DATA_HOME",
		"XDG_CACHE_HOME",
		"XDG_STATE_HOME",
	} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			environment[key] = value
		}
	}
	return environment
}

func systemdQuote(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`)
	return `"` + replacer.Replace(value) + `"`
}

func StartSystemdUserService(paths Paths) error {
	if _, err := os.Stat(paths.SystemdUnitFile); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("systemd user service is not installed")
		}
		return err
	}
	if _, err := runSystemctlUser("start", DefaultSystemdUnit); err != nil {
		return fmt.Errorf("start systemd user service: %w", err)
	}
	return nil
}

func StopSystemdUserService(paths Paths) error {
	if _, err := os.Stat(paths.SystemdUnitFile); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("systemd user service is not installed")
		}
		return err
	}
	if _, err := runSystemctlUser("stop", DefaultSystemdUnit); err != nil {
		return fmt.Errorf("stop systemd user service: %w", err)
	}
	return nil
}

func UninstallSystemdUserService(paths Paths) error {
	if _, err := runSystemctlUser("disable", "--now", DefaultSystemdUnit); err != nil && !isSystemdUnitMissingError(err) {
		return fmt.Errorf("disable systemd user service: %w", err)
	}
	if err := os.Remove(paths.SystemdUnitFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove systemd unit: %w", err)
	}
	if _, err := runSystemctlUser("daemon-reload"); err != nil {
		return fmt.Errorf("reload systemd user units: %w", err)
	}
	return nil
}

func SystemdUserServiceStatus(paths Paths) (bool, string, error) {
	if _, err := os.Stat(paths.SystemdUnitFile); err != nil {
		if os.IsNotExist(err) {
			return false, "not installed", nil
		}
		return false, "", err
	}

	output, err := runSystemctlUser("is-active", DefaultSystemdUnit)
	state := strings.TrimSpace(output)
	if err == nil {
		if state == "" {
			state = "active"
		}
		return true, state, nil
	}
	if state == "" {
		state = strings.TrimSpace(statusMessageFromCommandError(err))
	}
	switch state {
	case "inactive", "failed", "activating", "deactivating":
		return false, state, nil
	default:
		if isSystemdUnitMissingError(err) {
			return false, "not installed", nil
		}
		return false, "", fmt.Errorf("query systemd user service: %w", err)
	}
}

func isSystemdUnitMissingError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "could not be found") ||
		strings.Contains(message, "not loaded") ||
		strings.Contains(message, "does not exist")
}

func statusMessageFromCommandError(err error) string {
	if err == nil {
		return ""
	}
	parts := strings.SplitN(err.Error(), ": ", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}

func runSystemctlUser(args ...string) (string, error) {
	command := exec.Command("systemctl", append([]string{"--user"}, args...)...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		return strings.TrimSpace(stdout.String()), fmt.Errorf("%w: %s", err, message)
	}
	return strings.TrimSpace(stdout.String()), nil
}
