//go:build !windows

package procutil

import "os/exec"

// HideCommandWindow is a no-op on platforms that do not show Windows console windows.
func HideCommandWindow(cmd *exec.Cmd) {}
