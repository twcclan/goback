// Package hooks runs the shell commands a backup is wrapped in.
package hooks

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Runner runs a backup's pre and post hooks through the system shell.
// Post runs under its own deadline even when the context is cancelled, so
// a quiesced server is released after an interrupted walk.
type Runner struct {
	Pre  string
	Post string
	// Timeout bounds each hook; 0 means none.
	Timeout time.Duration
	// Stdout and Stderr receive the hooks' output; nil means the process's.
	Stdout io.Writer
	Stderr io.Writer
	Logf   func(format string, args ...interface{})
}

// RunPre runs the pre hook and returns what it printed to stdout.
func (r *Runner) RunPre(ctx context.Context) (string, error) {
	return r.run(ctx, "pre", r.Pre)
}

// RunPost runs the post hook, ignoring the context's cancellation.
func (r *Runner) RunPost(ctx context.Context) error {
	_, err := r.run(context.WithoutCancel(ctx), "post", r.Post)

	return err
}

func (r *Runner) run(ctx context.Context, name, command string) (string, error) {
	if command == "" {
		return "", nil
	}

	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	if r.Logf != nil {
		r.Logf("Running %s hook: %s", name, command)
	}

	var out bytes.Buffer

	cmd := shell(ctx, command)
	cmd.Stdout = io.MultiWriter(&out, r.writer(r.Stdout, os.Stdout))
	cmd.Stderr = r.writer(r.Stderr, os.Stderr)
	cmd.WaitDelay = 5 * time.Second

	err := cmd.Run()
	if ctx.Err() != nil {
		return out.String(), fmt.Errorf("%s hook: %w", name, ctx.Err())
	}

	if err != nil {
		return out.String(), fmt.Errorf("%s hook: %w", name, err)
	}

	return out.String(), nil
}

// Redirect returns the directory a pre hook asked the walk to read instead
// of the configured root, given as a GOBACK_ROOT=<dir> line on its stdout.
// The last such line wins; "" means no redirect.
func Redirect(output string) string {
	dir := ""
	for _, line := range strings.Split(output, "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "GOBACK_ROOT="); ok {
			dir = strings.TrimSpace(value)
		}
	}

	return dir
}

func (r *Runner) writer(w io.Writer, fallback io.Writer) io.Writer {
	if w != nil {
		return w
	}

	return fallback
}
