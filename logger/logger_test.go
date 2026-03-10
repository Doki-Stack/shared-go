package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestNew(t *testing.T) {
	log, err := New("test-service")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if log == nil {
		t.Fatal("Logger should not be nil")
	}
	if log.service != "test-service" {
		t.Errorf("service = %q, want test-service", log.service)
	}
}

func TestNewWithLevel(t *testing.T) {
	var buf bytes.Buffer
	log, err := New("test", WithLevel("debug"), WithOutput(zapcore.AddSync(&buf)))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	log.Debug("debug message")
	if buf.Len() == 0 {
		t.Error("Debug message should be logged when level is debug")
	}
}

func TestNewWithDevelopment(t *testing.T) {
	var buf bytes.Buffer
	log, err := New("test", WithDevelopment(true), WithOutput(zapcore.AddSync(&buf)))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	log.Info("test")
	if buf.Len() == 0 {
		t.Error("Expected output")
	}
	// Development mode uses console encoder (not JSON)
	if !bytes.Contains(buf.Bytes(), []byte("test")) {
		t.Error("Expected message in output")
	}
}

func TestNewWithFields(t *testing.T) {
	var buf bytes.Buffer
	log, err := New("test", WithFields(map[string]string{"env": "test"}), WithOutput(zapcore.AddSync(&buf)))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	log.Info("test")
	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out["env"] != "test" {
		t.Errorf("env = %v, want test", out["env"])
	}
}

func TestWithContext(t *testing.T) {
	var buf bytes.Buffer
	log, err := New("test", WithOutput(zapcore.AddSync(&buf)))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	ctx := context.WithValue(context.Background(), "org_id", "org-123")
	logWithCtx := log.WithContext(ctx)
	logWithCtx.Info("test")
	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out["org_id"] != "org-123" {
		t.Errorf("org_id = %v, want org-123", out["org_id"])
	}
}

func TestWithContextOTel(t *testing.T) {
	var buf bytes.Buffer
	log, err := New("test", WithOutput(zapcore.AddSync(&buf)))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	// Create a span context
	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)
	logWithCtx := log.WithContext(ctx)
	logWithCtx.Info("test")
	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace_id = %v", out["trace_id"])
	}
	if out["span_id"] != "00f067aa0ba902b7" {
		t.Errorf("span_id = %v", out["span_id"])
	}
}

func TestWithField(t *testing.T) {
	var buf bytes.Buffer
	log, err := New("test", WithOutput(zapcore.AddSync(&buf)))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	log.WithField("key", "value").Info("test")
	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out["key"] != "value" {
		t.Errorf("key = %v, want value", out["key"])
	}
}

func TestLevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	log, err := New("test", WithLevel("info"), WithOutput(zapcore.AddSync(&buf)))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	log.Debug("should not appear")
	if buf.Len() != 0 {
		t.Error("Debug should not appear at info level")
	}
	log.Info("should appear")
	if buf.Len() == 0 {
		t.Error("Info should appear")
	}
}

func TestRedaction(t *testing.T) {
	var buf bytes.Buffer
	// Use a simple pattern that definitely matches
	patterns := []string{`Bearer\s+.+`}
	log, err := New("test", WithOutput(zapcore.AddSync(&buf)), WithRedactPatterns(patterns))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	log.Info("test", zap.String("auth", "Bearer sk-12345"))
	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	authVal, ok := out["auth"].(string)
	if !ok {
		t.Fatalf("auth field not found or wrong type")
	}
	if !containsRedacted(authVal) {
		t.Errorf("auth should be redacted, got %q", authVal)
	}
}

func TestRedactionDefaultPatterns(t *testing.T) {
	var buf bytes.Buffer
	log, err := New("test", WithOutput(zapcore.AddSync(&buf)), WithRedactPatterns(DefaultRedactPatterns))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	log.Info("test", zap.String("auth", "Bearer sk-12345"))
	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	authVal, ok := out["auth"].(string)
	if !ok {
		t.Fatalf("auth field not found or wrong type")
	}
	if !containsRedacted(authVal) {
		t.Errorf("auth should be redacted with default patterns, got %q", authVal)
	}
}

func containsRedacted(s string) bool {
	return strings.Contains(s, "REDACTED")
}

func TestJSONFormat(t *testing.T) {
	var buf bytes.Buffer
	log, err := New("test", WithOutput(zapcore.AddSync(&buf)))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	log.Info("test message")
	var out map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out["timestamp"] == nil {
		t.Error("Expected timestamp")
	}
	if out["level"] != "info" {
		t.Errorf("level = %v, want info", out["level"])
	}
	if out["service"] != "test" {
		t.Errorf("service = %v, want test", out["service"])
	}
	if out["message"] != "test message" {
		t.Errorf("message = %v, want test message", out["message"])
	}
	if out["caller"] == nil {
		t.Error("Expected caller")
	}
}

func TestSync(t *testing.T) {
	// Use a buffer instead of stdout - Sync on /dev/stdout can fail on some systems
	var buf bytes.Buffer
	log, err := New("test", WithOutput(zapcore.AddSync(&buf)))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if err := log.Sync(); err != nil {
		t.Errorf("Sync failed: %v", err)
	}
}

func TestZap(t *testing.T) {
	log, err := New("test")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	z := log.Zap()
	if z == nil {
		t.Error("Zap() should return non-nil")
	}
}
