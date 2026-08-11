//go:build !windows

package platformprocess

import "os/exec"

func SuppressConsoleWindow(_ *exec.Cmd) {}
