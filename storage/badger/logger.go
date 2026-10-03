package badger

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// Logger is a badger.Logger that writes through the default slog logger,
// so badger's lines come out in the program's log format.
var Logger badger.Logger = slogLogger{}

type slogLogger struct{}

func (slogLogger) Errorf(f string, v ...any)   { log(slog.LevelError, f, v) }
func (slogLogger) Warningf(f string, v ...any) { log(slog.LevelWarn, f, v) }
func (slogLogger) Infof(f string, v ...any)    { log(slog.LevelInfo, f, v) }
func (slogLogger) Debugf(f string, v ...any)   { log(slog.LevelDebug, f, v) }

func log(level slog.Level, format string, v []any) {
	ctx := context.Background()
	handler := slog.Default().Handler()

	if !handler.Enabled(ctx, level) {
		return
	}

	// the source is badger's caller of the logger, not this adapter
	var pcs [1]uintptr
	runtime.Callers(4, pcs[:])

	record := slog.NewRecord(time.Now(), level, strings.TrimSpace(fmt.Sprintf(format, v...)), pcs[0])
	record.AddAttrs(slog.String("component", "badger"))
	_ = handler.Handle(ctx, record)
}
