package sah

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

const defaultLaunchdPATH = "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

func InstallLaunchAgent(paths Paths, executable string) error {
	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.MkdirAll(paths.LaunchAgentsDir, 0o755); err != nil {
		return fmt.Errorf("create launch agents dir: %w", err)
	}
	if err := os.MkdirAll(paths.LogsDir, 0o755); err != nil {
		return fmt.Errorf("create logs dir: %w", err)
	}

	plist := renderLaunchAgentPlist(
		executable,
		DefaultLaunchdCommand,
		paths.LaunchAgentStdout,
		paths.LaunchAgentStderr,
		paths.ConfigDir,
		launchAgentEnvironment(),
	)

	if err := os.WriteFile(paths.LaunchAgentPlist, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("write launch agent plist: %w", err)
	}

	_ = StopLaunchAgent()
	if err := BootstrapLaunchAgent(paths); err != nil {
		return err
	}
	return StartLaunchAgent()
}

func renderLaunchAgentPlist(
	executable string,
	command string,
	stdoutPath string,
	stderrPath string,
	workingDirectory string,
	environment map[string]string,
) string {
	environmentBlock := renderPlistEnvironment(environment)

	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>%s</string>
    <string>--daemon</string>
  </array>
%s  <key>WorkingDirectory</key>
  <string>%s</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`,
		plistEscape(DefaultLaunchdLabel),
		plistEscape(executable),
		plistEscape(command),
		environmentBlock,
		plistEscape(resolveLaunchAgentWorkingDirectory(workingDirectory)),
		plistEscape(stdoutPath),
		plistEscape(stderrPath),
	)
}

func launchAgentEnvironment() map[string]string {
	environment := map[string]string{
		"PATH": defaultLaunchdPATH,
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
	} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			environment[key] = value
		}
	}

	return environment
}

func resolveLaunchAgentWorkingDirectory(path string) string {
	if strings.TrimSpace(path) != "" {
		return path
	}

	homeDir, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(homeDir) != "" {
		return homeDir
	}
	return "/"
}

func renderPlistEnvironment(environment map[string]string) string {
	if len(environment) == 0 {
		return ""
	}

	keys := make([]string, 0, len(environment))
	for key, value := range environment {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)

	var builder strings.Builder
	builder.WriteString("  <key>EnvironmentVariables</key>\n")
	builder.WriteString("  <dict>\n")
	for _, key := range keys {
		builder.WriteString("    <key>")
		builder.WriteString(plistEscape(key))
		builder.WriteString("</key>\n")
		builder.WriteString("    <string>")
		builder.WriteString(plistEscape(environment[key]))
		builder.WriteString("</string>\n")
	}
	builder.WriteString("  </dict>\n")
	return builder.String()
}

func plistEscape(value string) string {
	var buffer bytes.Buffer
	if err := xml.EscapeText(&buffer, []byte(value)); err != nil {
		return value
	}
	return buffer.String()
}

func BootstrapLaunchAgent(paths Paths) error {
	_, err := runLaunchctl("bootstrap", launchdDomain(), paths.LaunchAgentPlist)
	if err != nil {
		return fmt.Errorf("bootstrap launch agent: %w", err)
	}
	return nil
}

func StartLaunchAgent() error {
	_, err := runLaunchctl("kickstart", "-k", launchdDomain()+"/"+DefaultLaunchdLabel)
	if err != nil {
		return fmt.Errorf("start launch agent: %w", err)
	}
	return nil
}

func StopLaunchAgent() error {
	_, err := runLaunchctl("bootout", launchdDomain()+"/"+DefaultLaunchdLabel)
	if err != nil {
		return fmt.Errorf("stop launch agent: %w", err)
	}
	return nil
}

func UninstallLaunchAgent(paths Paths) error {
	_ = StopLaunchAgent()
	if err := os.Remove(paths.LaunchAgentPlist); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove launch agent plist: %w", err)
	}
	return nil
}

func LaunchAgentStatus() (bool, string, error) {
	output, err := runLaunchctl("print", launchdDomain()+"/"+DefaultLaunchdLabel)
	if err != nil {
		if strings.Contains(err.Error(), "Could not find service") || strings.Contains(err.Error(), "service not found") {
			err = nil
			return false, "", err
		}
		return false, "", err
	}
	return true, output, nil
}

func launchdDomain() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func runLaunchctl(args ...string) (string, error) {
	command := exec.Command("launchctl", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err != nil {
		message := stderr.String()
		if message == "" {
			message = stdout.String()
		}
		return "", fmt.Errorf("%w: %s", err, message)
	}
	return stdout.String(), nil
}
