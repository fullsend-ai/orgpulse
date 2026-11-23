package logging

import (
	"context"
	"io"
	"log/slog"
	"strings"
)

type contextKey string

const loggerKey contextKey = "logger"
const requestIDKey contextKey = "request_id"

// Setup initializes the global logger with configurable level and format.
func Setup(level string, format string, output io.Writer) *slog.Logger {
	lvl := slog.LevelInfo
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "info":
		lvl = slog.LevelInfo
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}

	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(output, &slog.HandlerOptions{
			Level: lvl,
		})
	} else {
		handler = slog.NewTextHandler(output, &slog.HandlerOptions{
			Level: lvl,
		})
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

// WithLogger returns a copy of context with the logger embedded.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// FromContext returns the logger from context, or the default logger if not found.
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}

// WithRequestID returns a copy of context with the request ID embedded.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestIDFromContext returns the request ID from context, or empty string if not found.
func RequestIDFromContext(ctx context.Context) string {
	if requestID, ok := ctx.Value(requestIDKey).(string); ok {
		return requestID
	}
	return ""
}

// RequestID is a convenience alias for RequestIDFromContext.
func RequestID(ctx context.Context) string {
	return RequestIDFromContext(ctx)
}
