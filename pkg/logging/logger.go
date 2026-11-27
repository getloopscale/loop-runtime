// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package logging provides structured JSON logging for all LoopRuntime components.
// Uses Go's stdlib slog with JSON handler. Log level is configurable via
// the LOG_LEVEL environment variable (default: "info").
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// NewLogger creates a structured JSON logger for the given component.
// If no writer is provided, defaults to os.Stdout.
func NewLogger(component string, writers ...io.Writer) *slog.Logger {
	var w io.Writer = os.Stdout
	if len(writers) > 0 {
		w = writers[0]
	}

	level := parseLogLevel(os.Getenv("LOG_LEVEL"))

	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
	})

	return slog.New(handler).With("component", component)
}

// WithCorrelationID returns a child logger with the correlation_id field bound.
func WithCorrelationID(logger *slog.Logger, correlationID string) *slog.Logger {
	return logger.With("correlation_id", correlationID)
}

func parseLogLevel(raw string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
