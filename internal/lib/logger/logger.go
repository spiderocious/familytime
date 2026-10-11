// Package logger is the only sanctioned output. Structured, levelled, and
// stamped with the request id carried in the context.
package logger

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"familytime/backend/internal/lib/reqctx"
)

var base = slog.New(slog.NewJSONHandler(os.Stdout, nil))

func Init(level string) {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	base = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}

// With returns a logger stamped with the request id, when there is one.
func With(ctx context.Context) *slog.Logger {
	if id := reqctx.RequestID(ctx); id != "" {
		return base.With("request_id", id)
	}
	return base
}

func Info(msg string, args ...any)  { base.Info(msg, args...) }
func Warn(msg string, args ...any)  { base.Warn(msg, args...) }
func Error(msg string, args ...any) { base.Error(msg, args...) }
func Debug(msg string, args ...any) { base.Debug(msg, args...) }
