package logger

import (
	"context"
	"os"
	"strings"

	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger wraps zap.Logger with service name and optional context fields.
type Logger struct {
	zap     *zap.Logger
	service string
}

// loggerConfig holds options for New.
type loggerConfig struct {
	level          zap.AtomicLevel
	development    bool
	fields         map[string]interface{}
	redactPatterns []redactPattern
	output         zapcore.WriteSyncer
}

// Option configures the Logger.
type Option func(*loggerConfig)

// WithLevel sets the log level (debug, info, warn, error).
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

// WithDevelopment enables development mode (human-readable output).
func WithDevelopment(dev bool) Option {
	return func(c *loggerConfig) {
		c.development = dev
	}
}

// WithFields adds initial fields to the logger.
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

// WithRedactPatterns sets regex patterns for field value redaction.
func WithRedactPatterns(patterns []string) Option {
	return func(c *loggerConfig) {
		c.redactPatterns = compilePatterns(patterns)
	}
}

// WithOutput sets the output writer (default: os.Stdout).
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

	var encoder zapcore.Encoder
	if cfg.development {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	out := cfg.output
	if out == nil {
		out = zapcore.AddSync(os.Stdout)
	}
	core := zapcore.NewCore(encoder, out, cfg.level)

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

	return &Logger{
		zap:     zapLogger.With(zap.String("service", serviceName)),
		service: serviceName,
	}, nil
}

// WithContext returns a new Logger with trace_id, span_id, org_id from context.
// Reads from: trace.SpanFromContext (trace_id, span_id), context key org_id.
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

// Info logs at InfoLevel.
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
