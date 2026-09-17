//go:build windows

package hooks

import (
	"context"
	"os/exec"
	"strconv"
)

// shell runs the command through cmd and, on cancellation, kills the whole
// process tree, since ending cmd alone leaves its children running.
func shell(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "cmd", "/C", command)
	cmd.Cancel = func() error {
		return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	}

	return cmd
}
