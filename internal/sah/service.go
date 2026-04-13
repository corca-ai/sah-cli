package sah

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

func ServiceManagerName() string {
	switch runtime.GOOS {
	case "darwin":
		return "launchd"
	case "linux":
		return "systemd --user"
	default:
		return runtime.GOOS
	}
}

func ServiceStatusLabel() string {
	switch runtime.GOOS {
	case "darwin":
		return "Launchd"
	case "linux":
		return "Systemd"
	default:
		return "Service"
	}
}

func ServiceDefinitionLabel() string {
	switch runtime.GOOS {
	case "darwin":
		return "Plist"
	case "linux":
		return "Unit"
	default:
		return "Service"
	}
}

func ServiceDefinitionPath(paths Paths) string {
	switch runtime.GOOS {
	case "darwin":
		return paths.LaunchAgentPlist
	case "linux":
		return paths.SystemdUnitFile
	default:
		return ""
	}
}

func ServiceCaptureLabel() string {
	switch runtime.GOOS {
	case "darwin":
		return "Launchd capture"
	case "linux":
		return "Journal"
	default:
		return ""
	}
}

func ServiceCaptureValue(paths Paths) string {
	switch runtime.GOOS {
	case "darwin":
		return strings.Join([]string{paths.LaunchAgentStdout, paths.LaunchAgentStderr}, " and ")
	case "linux":
		return fmt.Sprintf("journalctl --user -u %s", DefaultSystemdUnit)
	default:
		return ""
	}
}

func InstallService(paths Paths, executable string) error {
	switch runtime.GOOS {
	case "darwin":
		return InstallLaunchAgent(paths, executable)
	case "linux":
		return InstallSystemdUserService(paths, executable)
	default:
		return fmt.Errorf("daemon mode is unsupported on %s", runtime.GOOS)
	}
}

func StartService(paths Paths) error {
	switch runtime.GOOS {
	case "darwin":
		loaded, _, err := LaunchAgentStatus()
		if err != nil {
			return err
		}
		if !loaded {
			if err := BootstrapLaunchAgent(paths); err != nil {
				return err
			}
		}
		return StartLaunchAgent()
	case "linux":
		return StartSystemdUserService(paths)
	default:
		return fmt.Errorf("daemon mode is unsupported on %s", runtime.GOOS)
	}
}

func StopService(paths Paths) error {
	switch runtime.GOOS {
	case "darwin":
		return StopLaunchAgent()
	case "linux":
		return StopSystemdUserService(paths)
	default:
		return fmt.Errorf("daemon mode is unsupported on %s", runtime.GOOS)
	}
}

func UninstallService(paths Paths) error {
	switch runtime.GOOS {
	case "darwin":
		return UninstallLaunchAgent(paths)
	case "linux":
		return UninstallSystemdUserService(paths)
	default:
		return fmt.Errorf("daemon mode is unsupported on %s", runtime.GOOS)
	}
}

func ServiceStatus(paths Paths) (bool, string, error) {
	switch runtime.GOOS {
	case "darwin":
		return LaunchAgentStatus()
	case "linux":
		return SystemdUserServiceStatus(paths)
	default:
		return false, "", fmt.Errorf("daemon mode is unsupported on %s", runtime.GOOS)
	}
}

func PreferredServiceExecutable(executable string) (string, error) {
	cleaned := filepath.Clean(executable)
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return "", fmt.Errorf("resolve executable symlink: %w", err)
	}

	switch runtime.GOOS {
	case "darwin":
		return selectStableExecutable(resolved, []string{"/opt/homebrew/bin/sah", "/usr/local/bin/sah"})
	default:
		return filepath.Clean(resolved), nil
	}
}

func selectStableExecutable(resolved string, candidates []string) (string, error) {
	if canonical, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = canonical
	}

	for _, candidate := range candidates {
		target, targetErr := filepath.EvalSymlinks(candidate)
		if targetErr != nil {
			continue
		}
		if filepath.Clean(target) == filepath.Clean(resolved) {
			return candidate, nil
		}
	}

	return filepath.Clean(resolved), nil
}
