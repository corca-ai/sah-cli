//go:build darwin

package sah

import "os/exec"

func defaultBrowserCommand(rawURL string) (*exec.Cmd, error) {
	return exec.Command("open", rawURL), nil
}
