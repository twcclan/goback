package common

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"
)

var (
	jsonMode bool
	stdout   io.Writer = os.Stdout
	exit               = os.Exit
)

// SetOutput sets up a command's output: logs as text on stderr, or with
// json set as JSON lines on stderr, with results and errors as JSON on
// stdout. Output of the log package goes through the same handler.
func SetOutput(json bool) {
	jsonMode = json

	// slog takes over the log package, and keeps its callers only if the
	// log package was asked for them when it does
	log.SetFlags(log.Lshortfile)

	opts := &slog.HandlerOptions{AddSource: true}
	if json {
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, opts)))
		return
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, opts)))
}

// JSON reports whether the command was asked for JSON output.
func JSON() bool {
	return jsonMode
}

// Result reports what a command produced: v as JSON on stdout in JSON
// mode, otherwise whatever text prints.
func Result(v any, text func()) {
	if !jsonMode {
		text()
		return
	}

	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")

	if err := enc.Encode(v); err != nil {
		Fatal(err)
	}
}

// Fail reports that a command failed: in JSON mode as an error object on
// stdout, otherwise as an error in the log.
func Fail(msg string) {
	fail(msg, 2)
}

func fail(msg string, skip int) {
	if jsonMode {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(struct {
			Error string `json:"error"`
		}{msg})

		return
	}

	var pcs [1]uintptr
	runtime.Callers(skip+1, pcs[:])

	record := slog.NewRecord(time.Now(), slog.LevelError, msg, pcs[0])
	_ = slog.Default().Handler().Handle(context.Background(), record)
}

// Fatal fails the command with the values' message, as log.Fatal would,
// and exits 1.
func Fatal(v ...any) {
	fail(fmt.Sprint(v...), 2)
	exit(1)
}

// Fatalf fails the command with the formatted message and exits 1.
func Fatalf(format string, v ...any) {
	fail(fmt.Sprintf(format, v...), 2)
	exit(1)
}

// Fatalln fails the command as log.Fatalln would and exits 1.
func Fatalln(v ...any) {
	fail(strings.TrimSuffix(fmt.Sprintln(v...), "\n"), 2)
	exit(1)
}
