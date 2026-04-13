//go:build !darwin && !linux

package sah

import (
	"fmt"
	"os/exec"
	"runtime"
)

func defaultBrowserCommand(rawURL string) (*exec.Cmd, error) {
	return nil, fmt.Errorf("automatic browser open is unsupported on %s", runtime.GOOS)
}
