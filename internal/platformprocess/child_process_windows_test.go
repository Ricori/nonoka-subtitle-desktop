//go:build windows

package platformprocess

import (
	"os/exec"
	"testing"
)

func TestSuppressConsoleWindow(t *testing.T) {
	command := exec.Command("ffmpeg.exe", "-version")
	SuppressConsoleWindow(command)
	if command.SysProcAttr == nil || !command.SysProcAttr.HideWindow {
		t.Fatal("HideWindow was not enabled")
	}
	if command.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatal("CREATE_NO_WINDOW was not enabled")
	}
}
