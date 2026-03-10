# shared-go Implementation Plan — Logger Package

## 1. Overview

The `logger` package provides a structured JSON logger built on `zap`, with automatic trace context injection and secret redaction. Log output is designed for Loki/Promtail ingestion and follows the Doki Stack logging standard.

**Package name:** `logger`

## 2. Files

| File | Purpose |
|------|---------|
| `logger.go` | Logger type, constructor, methods |
| `redact.go` | Regex-based field redaction, zap Core wrapper |
| `logger_test.go` | Unit tests |

## 3. Types

### 3.1 Logger Struct

```go
package logger

import "go.uber.org/zap"

// Logger wraps zap.Logger with service name and optional context fields.
type Logger struct {
	zap     *zap.Logger
	service string
}
```

## 4. Constructor

```go
// Option configures the Logger.
type Option func(*loggerConfig)

type loggerConfig struct {
	level          zap.AtomicLevel
	development    bool
	fields         map[string]interface{}
	redactPatterns []*regexp.Regexp
	output         zapcore.WriteSyncer // default: os.Stdout; use zapcore.AddSync(io.Writer) for tests
}

func WithLevel(level string) Option {
	return func(c *loggerConfig) {
		var l zap.AtomicLevel
		switch strings.ToLower(level) {
		case "debug":
			l = zap.NewAtomicLevelAt(zap.DebugLevel)
		case "info":
			l = zap.NewAtomicLevelAt(zap.InfoLevel)
		case "warn":
			l = zap.NewAtomicLevelAt(zap.WarnLevel)
		case "error":
			l = zap.NewAtomicLevelAt(zap.ErrorLevel)
		default:
			l = zap.NewAtomicLevelAt(zap.InfoLevel)
		}
		c.level = l
	}
}

func WithDevelopment(dev bool) Option {
	return func(c *loggerConfig) {
		c.development = dev
	}
}

func WithFields(fields map[string]string) Option {
	return func(c *loggerConfig) {
		if c.fields == nil {
			c.fields = make(map[string]interface{})
		}
		for k, v := range fields {
			c.fields[k] = v
		}
	}
}

func WithRedactPatterns(patterns []string) Option {
	return func(c *loggerConfig) {
		c.redactPatterns = compilePatterns(patterns)
	}
}

func WithOutput(w zapcore.WriteSyncer) Option {
	return func(c *loggerConfig) {
		c.output = w
	}
}

// New creates a Logger with the given service name and options.
// Default: production config, JSON encoder, InfoLevel.
func New(serviceName string, opts ...Option) (*Logger, error) {
	cfg := &loggerConfig{
		level:       zap.NewAtomicLevelAt(zap.InfoLevel),
		development: false,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		MessageKey:     "message",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	encoder := zapcore.NewJSONEncoder(encoderConfig)
	out := cfg.output
	if out == nil {
		out = zapcore.AddSync(os.Stdout)
	}
	core := zapcore.NewCore(encoder, out, cfg.level)

	// Wrap with redaction core if patterns provided
	if len(cfg.redactPatterns) > 0 {
		core = newRedactCore(core, cfg.redactPatterns)
	}

	zapLogger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))
	if len(cfg.fields) > 0 {
		fields := make([]zap.Field, 0, len(cfg.fields))
		for k, v := range cfg.fields {
			fields = append(fields, zap.Any(k, v))
		}
		zapLogger = zapLogger.With(fields...)
	}

	return &Logger{zap: zapLogger.With(zap.String("service", serviceName)), service: serviceName}, nil
}
```

**Imports for logger.go:** `go.uber.org/zap`, `go.uber.org/zap/zapcore`, `os`, `regexp`, `strings`

## 5. Methods

```go
// WithContext returns a new Logger with trace_id, span_id, org_id from context.
// Reads from: trace.SpanFromContext (trace_id, span_id), context key "org_id".
func (l *Logger) WithContext(ctx context.Context) *Logger {
	if ctx == nil {
		return l
	}
	fields := make([]zap.Field, 0, 3)
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		fields = append(fields, zap.String("trace_id", span.SpanContext().TraceID().String()))
		fields = append(fields, zap.String("span_id", span.SpanContext().SpanID().String()))
	}
	if orgID := ctx.Value("org_id"); orgID != nil {
		if s, ok := orgID.(string); ok {
			fields = append(fields, zap.String("org_id", s))
		}
	}
	if len(fields) == 0 {
		return l
	}
	return &Logger{zap: l.zap.With(fields...), service: l.service}
}

// WithField returns a new Logger with an additional field.
func (l *Logger) WithField(key string, value interface{}) *Logger {
	return &Logger{zap: l.zap.With(zap.Any(key, value)), service: l.service}
}

// WithFields returns a new Logger with additional fields.
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	zapFields := make([]zap.Field, 0, len(fields))
	for k, v := range fields {
		zapFields = append(zapFields, zap.Any(k, v))
	}
	return &Logger{zap: l.zap.With(zapFields...), service: l.service}
}

// Info logs at InfoLevel. Use event_type for Loki filtering.
func (l *Logger) Info(msg string, fields ...zap.Field) {
	l.zap.Info(msg, fields...)
}

// Error logs at ErrorLevel.
func (l *Logger) Error(msg string, fields ...zap.Field) {
	l.zap.Error(msg, fields...)
}

// Warn logs at WarnLevel.
func (l *Logger) Warn(msg string, fields ...zap.Field) {
	l.zap.Warn(msg, fields...)
}

// Debug logs at DebugLevel.
func (l *Logger) Debug(msg string, fields ...zap.Field) {
	l.zap.Debug(msg, fields...)
}

// Fatal logs at FatalLevel and then calls os.Exit(1).
func (l *Logger) Fatal(msg string, fields ...zap.Field) {
	l.zap.Fatal(msg, fields...)
}

// Sync flushes any buffered log entries.
func (l *Logger) Sync() error {
	return l.zap.Sync()
}

// Zap returns the underlying zap.Logger for escape-hatch access.
func (l *Logger) Zap() *zap.Logger {
	return l.zap
}
```

**Imports:** `context`, `go.opentelemetry.io/otel/trace`, `go.uber.org/zap`

## 6. Log Output Format

Output must match Loki/Promtail expectations. Example:

```json
{
  "timestamp": "2025-01-01T00:00:00.000Z",
  "level": "info",
  "trace_id": "abc123",
  "span_id": "def456",
  "org_id": "a0000000-0000-0000-0000-000000000001",
  "service": "api-server",
  "message": "request completed",
  "event_type": "http_response",
  "caller": "handler.go:42"
}
```

**Field mapping:**

| Field | Source | Notes |
|-------|--------|-------|
| `timestamp` | zapcore.ISO8601TimeEncoder | ISO 8601 format |
| `level` | zap level | lowercase: info, error, warn, debug |
| `trace_id` | OTel span or context | From WithContext |
| `span_id` | OTel span | From WithContext |
| `org_id` | context key "org_id" | From WithContext |
| `service` | constructor arg | Always present |
| `message` | log method arg | Always present |
| `event_type` | caller-provided field | Use `zap.String("event_type", "http_response")` |
| `caller` | zap.AddCaller | Short format: file:line |

**Usage example for event_type:**

```go
log.Info("request completed", zap.String("event_type", "http_response"), zap.Int("status", 200))
```

## 7. Secret Redaction (redact.go)

### 7.1 Design

- Regex-based field value redaction
- Applied as a zap `Core` wrapper that intercepts fields before encoding
- Redacted value format: `[REDACTED:pattern_name]`
- Configurable patterns via `WithRedactPatterns([]string)`

### 7.2 Default Patterns

```go
// DefaultRedactPatterns returns the standard patterns for secret redaction (risk register T10).
var DefaultRedactPatterns = []string{
	`(?i)bearer\s+[a-zA-Z0-9\-_.]+`,           // Bearer tokens
	`(?i)api[_-]?key\s*[:=]\s*["']?[a-zA-Z0-9\-_]+["']?`, // API keys
	`(?i)password\s*[:=]\s*["']?[^\s"']+["']?`,            // Passwords
	`(?i)secret\s*[:=]\s*["']?[^\s"']+["']?`,             // Secrets
	`hvs\.[a-zA-Z0-9]+`,                                   // Vault tokens (hvs. prefix)
	`hvb\.[a-zA-Z0-9]+`,                                   // Vault batch tokens
	`[a-zA-Z0-9\-]+://[^:]+:[^@]+@`,                      // Connection strings (user:pass@)
}
```

### 7.3 RedactCore Implementation

```go
package logger

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"regexp"
)

// redactCore wraps a zapcore.Core and redacts field values matching patterns.
type redactCore struct {
	zapcore.Core
	patterns []*regexp.Regexp
}

func newRedactCore(core zapcore.Core, patterns []*regexp.Regexp) zapcore.Core {
	return &redactCore{Core: core, patterns: patterns}
}

func (c *redactCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	redacted := make([]zapcore.Field, len(fields))
	for i, f := range fields {
		redacted[i] = redactField(f, c.patterns)
	}
	return c.Core.Write(entry, redacted)
}

func redactField(f zapcore.Field, patterns []*regexp.Regexp) zapcore.Field {
	if f.Type != zapcore.StringType && f.Type != zapcore.StringerType {
		return f
	}
	var s string
	switch f.Type {
	case zapcore.StringType:
		s = f.String
	case zapcore.StringerType:
		s = f.Interface.(fmt.Stringer).String()
	default:
		return f
	}
	for _, p := range patterns {
		if p.MatchString(s) {
			return zap.String(f.Key, "[REDACTED:"+p.String()+"]")
		}
	}
	return f
}

func compilePatterns(patterns []string) []*regexp.Regexp {
	var result []*regexp.Regexp
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			result = append(result, re)
		}
	}
	return result
}
```

**Note:** The `redactField` logic should iterate patterns and use a pattern name (e.g., from a named struct) for the redaction message. Simplified version above uses regex string; consider a `[]struct{Name, Pattern}` for clearer `[REDACTED:bearer_token]` output.

### 7.4 Improved Pattern Structure (Recommended)

```go
type redactPattern struct {
	Name    string
	Pattern *regexp.Regexp
}

func redactField(f zapcore.Field, patterns []redactPattern) zapcore.Field {
	// ... same logic, but use pattern.Name in output
	return zap.String(f.Key, "[REDACTED:"+pattern.Name+"]")
}
```

## 8. Context Integration

| Context Source | Field | Extraction |
|----------------|-------|------------|
| `trace.SpanFromContext(ctx)` | trace_id | `span.SpanContext().TraceID().String()` |
| `trace.SpanFromContext(ctx)` | span_id | `span.SpanContext().SpanID().String()` |
| `ctx.Value("org_id")` | org_id | String from context (middleware sets this) |

These fields are automatically added when `WithContext(ctx)` is used. Middleware must set `org_id` in context; OTel middleware sets trace context.

## 9. Full API Surface Summary

```go
// Constructor
func New(serviceName string, opts ...Option) (*Logger, error)

// Options
func WithLevel(level string) Option
func WithDevelopment(dev bool) Option
func WithFields(fields map[string]string) Option
func WithRedactPatterns(patterns []string) Option
func WithOutput(w zapcore.WriteSyncer) Option

// Methods
func (l *Logger) WithContext(ctx context.Context) *Logger
func (l *Logger) WithField(key string, value interface{}) *Logger
func (l *Logger) WithFields(fields map[string]interface{}) *Logger
func (l *Logger) Info(msg string, fields ...zap.Field)
func (l *Logger) Error(msg string, fields ...zap.Field)
func (l *Logger) Warn(msg string, fields ...zap.Field)
func (l *Logger) Debug(msg string, fields ...zap.Field)
func (l *Logger) Fatal(msg string, fields ...zap.Field)
func (l *Logger) Sync() error
func (l *Logger) Zap() *zap.Logger
```

## 10. Test Plan (logger_test.go)

| Test Case | Description |
|-----------|-------------|
| **New** | Create logger with service name; verify no error |
| **New with WithLevel** | Set DebugLevel; log at debug; verify output |
| **New with WithDevelopment** | Development mode; verify config differs |
| **New with WithFields** | Add initial fields; verify in output |
| **WithContext** | Create context with org_id; use WithContext; verify org_id in output |
| **WithContext OTel** | Use OTel span in context; verify trace_id, span_id |
| **WithField** | Add field; verify in next log line |
| **Level filtering** | Set InfoLevel; Debug should not appear |
| **Redaction** | Log string with "Bearer sk-xxx"; verify [REDACTED:...] in output |
| **Redaction patterns** | Custom pattern; verify match and redaction |
| **JSON format** | Parse log output; verify timestamp, level, service, message, caller |
| **Sync** | Call Sync; verify no error |

### Example Test Snippets

```go
func TestNew(t *testing.T) {
	log, err := New("test-service")
	require.NoError(t, err)
	require.NotNil(t, log)
	require.Equal(t, "test-service", log.service)
}

func TestWithContext(t *testing.T) {
	log, _ := New("test")
	ctx := context.WithValue(context.Background(), "org_id", "org-123")
	logWithCtx := log.WithContext(ctx)
	// Capture output and verify org_id in JSON
}

func TestRedaction(t *testing.T) {
	var buf bytes.Buffer
	log, _ := New("test", WithOutput(zapcore.AddSync(&buf)), WithRedactPatterns(DefaultRedactPatterns))
	log.Info("test", zap.String("auth", "Bearer sk-12345"))
	var out map[string]interface{}
	json.Unmarshal(buf.Bytes(), &out)
	assert.Contains(t, out["auth"], "[REDACTED")
}
```

## 11. Dependencies

| Dependency | Purpose |
|------------|---------|
| `go.uber.org/zap` | Core logger |
| `go.uber.org/zap/zapcore` | Encoder, Core, levels |
| `go.opentelemetry.io/otel/trace` | SpanFromContext |
| `context` | WithContext |
| `regexp` | Redaction patterns |
| `os` | Stdout for default output |

**No internal shared-go dependencies.** Logger is a foundation layer.
