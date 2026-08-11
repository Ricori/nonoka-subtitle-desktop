//go:build windows

package platformprocess

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

func SuppressConsoleWindow(command *exec.Cmd) {
	if command.SysProcAttr == nil {
		command.SysProcAttr = &syscall.SysProcAttr{}
	}
	command.SysProcAttr.HideWindow = true
	command.SysProcAttr.CreationFlags |= createNoWindow
}
