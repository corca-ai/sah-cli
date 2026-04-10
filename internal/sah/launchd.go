package sah

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
)

func InstallLaunchAgent(paths Paths, executable string) error {
	if err := os.MkdirAll(paths.LaunchAgentsDir, 0o755); err != nil {
		return fmt.Errorf("create launch agents dir: %w", err)
	}
	if err := os.MkdirAll(paths.LogsDir, 0o755); err != nil {
		return fmt.Errorf("create logs dir: %w", err)
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
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
`, DefaultLaunchdLabel, executable, DefaultLaunchdCommand, paths.LaunchAgentStdout, paths.LaunchAgentStderr)

	if err := os.WriteFile(paths.LaunchAgentPlist, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("write launch agent plist: %w", err)
	}

	_ = StopLaunchAgent()
	if err := BootstrapLaunchAgent(paths); err != nil {
		return err
	}
	return StartLaunchAgent()
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
		return false, "", nil
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
