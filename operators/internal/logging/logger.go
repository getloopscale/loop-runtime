package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// NewLogger creates a JSON-structured slog.Logger with the given component name.
// Log level is configurable via the LOG_LEVEL environment variable (default: "info").
func NewLogger(component string, w io.Writer) *slog.Logger {
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
