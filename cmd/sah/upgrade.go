package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/corca-ai/sah-cli/internal/sah"
)

type installMethodKind string

const installMethodHomebrew installMethodKind = "homebrew"

type installMethod struct {
	Kind           installMethodKind
	Command        []string
	DisplayCommand string
}

func detectInstallMethod() (installMethod, error) {
	executable, err := os.Executable()
	if err != nil {
		return installMethod{}, fmt.Errorf("resolve executable path: %w", err)
	}

	cleaned := filepath.Clean(executable)
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		resolved = cleaned
	}
	lowerResolved := strings.ToLower(filepath.Clean(resolved))
	lowerCleaned := strings.ToLower(filepath.Clean(cleaned))

	if isHomebrewExecutable(lowerResolved) || isHomebrewShim(lowerCleaned) {
		return installMethod{
			Kind:           installMethodHomebrew,
			Command:        []string{"brew", "upgrade", "corca-ai/tap/sah-cli"},
			DisplayCommand: "brew upgrade corca-ai/tap/sah-cli",
		}, nil
	}

	return installMethod{}, nil
}

func isHomebrewExecutable(path string) bool {
	return strings.Contains(path, "/cellar/sah-cli/")
}

func isHomebrewShim(path string) bool {
	for _, candidate := range []string{"/opt/homebrew/bin/sah", "/usr/local/bin/sah"} {
		if path == strings.ToLower(filepath.Clean(candidate)) {
			return true
		}
	}
	return false
}

func daemonInstalled(paths sah.Paths) bool {
	servicePath := strings.TrimSpace(sah.ServiceDefinitionPath(paths))
	if servicePath == "" {
		return false
	}
	_, err := os.Stat(servicePath)
	return err == nil
}
