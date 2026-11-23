package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestSetup_JSONHandler(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup("debug", "json", &buf)

	if slog.Default() != logger {
		t.Errorf("expected default logger to be set")
	}

	logger.Info("test message", "key", "value")
	output := buf.String()

	if !strings.Contains(output, `"level":"INFO"`) {
		t.Errorf("expected JSON format with level, got: %s", output)
	}
	if !strings.Contains(output, `"msg":"test message"`) {
		t.Errorf("expected msg field, got: %s", output)
	}
	if !strings.Contains(output, `"key":"value"`) {
		t.Errorf("expected key=value, got: %s", output)
	}
}

func TestSetup_TextHandler(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup("debug", "text", &buf)

	logger.Info("test message", "key", "value")
	output := buf.String()

	if !strings.Contains(output, "INFO") {
		t.Errorf("expected INFO level in text output, got: %s", output)
	}
	if !strings.Contains(output, "test message") {
		t.Errorf("expected test message, got: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("expected key=value, got: %s", output)
	}
}

func TestSetup_LogLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup("warn", "text", &buf)

	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")

	output := buf.String()

	if strings.Contains(output, "debug message") {
		t.Errorf("expected debug message to be filtered, got: %s", output)
	}
	if strings.Contains(output, "info message") {
		t.Errorf("expected info message to be filtered, got: %s", output)
	}
	if !strings.Contains(output, "warn message") {
		t.Errorf("expected warn message, got: %s", output)
	}
	if !strings.Contains(output, "error message") {
		t.Errorf("expected error message, got: %s", output)
	}
}

func TestSetup_InvalidLogLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup("invalid-level", "text", &buf)

	// Should default to LevelInfo
	logger.Info("info message")
	output := buf.String()

	if !strings.Contains(output, "INFO") {
		t.Errorf("expected INFO level (default), got: %s", output)
	}
	if !strings.Contains(output, "info message") {
		t.Errorf("expected info message, got: %s", output)
	}
}

func TestWithLogger_FromContext(t *testing.T) {
	ctx := context.Background()

	originalLogger := slog.Default()
	testLogger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	ctx = WithLogger(ctx, testLogger)

	retrieved := FromContext(ctx)
	if retrieved != testLogger {
		t.Errorf("expected testLogger from context, got different logger")
	}

	// Context without logger should return default
	ctxNoLogger := context.Background()
	retrievedDefault := FromContext(ctxNoLogger)
	if retrievedDefault != originalLogger {
		t.Errorf("expected default logger when no logger in context")
	}
}

func TestWithRequestID_RequestIDFromContext(t *testing.T) {
	ctx := context.Background()

	testRequestID := "test-request-id-123"
	ctx = WithRequestID(ctx, testRequestID)

	retrieved := RequestIDFromContext(ctx)
	if retrieved != testRequestID {
		t.Errorf("expected %s from context, got %s", testRequestID, retrieved)
	}

	// Context without request ID should return empty string
	ctxNoRequestID := context.Background()
	retrievedEmpty := RequestIDFromContext(ctxNoRequestID)
	if retrievedEmpty != "" {
		t.Errorf("expected empty string when no request ID in context, got %s", retrievedEmpty)
	}
}

func TestRequestID_ConvenienceAlias(t *testing.T) {
	ctx := context.Background()
	testRequestID := "alias-test-id"

	ctx = WithRequestID(ctx, testRequestID)

	// RequestID should be alias for RequestIDFromContext
	result1 := RequestIDFromContext(ctx)
	result2 := RequestID(ctx)

	if result1 != result2 {
		t.Errorf("RequestID and RequestIDFromContext returned different values: %s vs %s", result1, result2)
	}
	if result1 != testRequestID {
		t.Errorf("expected %s, got %s", testRequestID, result1)
	}
}

func TestContextKeysAreDistinct(t *testing.T) {
	// Verify loggerKey and requestIDKey are different
	if loggerKey == requestIDKey {
		t.Errorf("loggerKey and requestIDKey should be different")
	}

	ctx := context.Background()
	testLogger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	testRequestID := "test-id"

	// Add both to context
	ctx = WithLogger(ctx, testLogger)
	ctx = WithRequestID(ctx, testRequestID)

	// Verify both can be retrieved independently
	retrievedLogger := FromContext(ctx)
	retrievedRequestID := RequestIDFromContext(ctx)

	if retrievedLogger != testLogger {
		t.Errorf("logger not retrieved correctly")
	}
	if retrievedRequestID != testRequestID {
		t.Errorf("request ID not retrieved correctly")
	}
}
