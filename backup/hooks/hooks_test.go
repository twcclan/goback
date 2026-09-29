package hooks

import (
	"bytes"
	"context"
	"log/slog"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func commands() (echo, fail, hang string) {
	if runtime.GOOS == "windows" {
		return "echo hello", "exit /b 3", "ping -n 60 127.0.0.1 > nul"
	}

	return "echo hello", "exit 3", "sleep 60"
}

func TestRunPreCapturesOutput(t *testing.T) {
	echo, _, _ := commands()

	var stdout, logged bytes.Buffer
	r := &Runner{Pre: echo, Stdout: &stdout, Logger: slog.New(slog.NewTextHandler(&logged, nil))}

	out, err := r.RunPre(context.Background())
	require.NoError(t, err)
	require.Contains(t, out, "hello")
	require.Contains(t, stdout.String(), "hello", "output is echoed as well as captured")
	require.Contains(t, logged.String(), "hook=pre")
}

func TestRunPreReportsFailure(t *testing.T) {
	_, fail, _ := commands()
	r := &Runner{Pre: fail}

	_, err := r.RunPre(context.Background())
	require.Error(t, err)

	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 3, exit.ExitCode())
}

func TestTimeoutKillsHook(t *testing.T) {
	_, _, hang := commands()
	r := &Runner{Pre: hang, Timeout: 300 * time.Millisecond}

	start := time.Now()
	_, err := r.RunPre(context.Background())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 20*time.Second)
}

func TestPostRunsAfterCancellation(t *testing.T) {
	echo, _, _ := commands()

	var stdout bytes.Buffer
	r := &Runner{Post: echo, Stdout: &stdout, Timeout: time.Minute}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.NoError(t, r.RunPost(ctx))
	require.Contains(t, stdout.String(), "hello")
}

func TestRedirect(t *testing.T) {
	require.Equal(t, "", Redirect(""))
	require.Equal(t, "", Redirect("saving off\nsaved\n"))
	require.Equal(t, "/snap/world", Redirect("saving off\n  GOBACK_ROOT=/snap/world  \n"))
	require.Equal(t, "/snap/b", Redirect("GOBACK_ROOT=/snap/a\r\nGOBACK_ROOT=/snap/b\r\n"))
}

func TestEmptyHooksAreNoOps(t *testing.T) {
	r := &Runner{}

	out, err := r.RunPre(context.Background())
	require.NoError(t, err)
	require.Empty(t, out)
	require.NoError(t, r.RunPost(context.Background()))
}
