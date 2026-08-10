//go:build windows

package procutil

import (
	"os/exec"
	"syscall"
)

// HideCommandWindow prevents short-lived helper processes from flashing a console window on Windows.
func HideCommandWindow(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
