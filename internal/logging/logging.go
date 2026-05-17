// Package logging builds the project slog logger (text in dev, JSON in prod).
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New returns a logger writing to stderr. format: "text"|"json". level:
// "debug"|"info"|"warn"|"error" (default info).
func New(level, format string) *slog.Logger { return NewTo(os.Stderr, level, format) }

// NewTo is New with an explicit writer (for tests).
func NewTo(w io.Writer, level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}
	var h slog.Handler
	if strings.EqualFold(format, "json") {
		h = slog.NewJSONHandler(w, opts)
	} else {
		h = slog.NewTextHandler(w, opts)
	}
	return slog.New(h)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
