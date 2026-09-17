//go:build !windows

package hooks

import (
	"context"
	"os/exec"
	"syscall"
)

// shell runs the command through sh in its own process group and, on
// cancellation, kills the group, so a hook's children go with it.
func shell(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}

	return cmd
}
