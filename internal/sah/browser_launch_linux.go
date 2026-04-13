//go:build linux

package sah

import "os/exec"

func defaultBrowserCommand(rawURL string) (*exec.Cmd, error) {
	return exec.Command("xdg-open", rawURL), nil
}
