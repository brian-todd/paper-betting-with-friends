// Package logging configures the process-wide slog logger.
package logging

import (
	"log/slog"
	"os"
)

// Setup installs a slog logger as the default for the process and returns it.
//
// Production emits JSON so hosted log collectors can index the attributes;
// development emits human-readable text. Call this before constructing any
// service that captures slog.Default.
func Setup(env string) *slog.Logger {
	level := slog.LevelInfo
	if env == "development" {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if env == "production" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}
