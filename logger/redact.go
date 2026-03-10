package logger

import (
	"fmt"
	"regexp"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// redactPattern holds a named regex pattern for redaction.
type redactPattern struct {
	Name    string
	Pattern *regexp.Regexp
}

// redactCore wraps a zapcore.Core and redacts field values matching patterns.
type redactCore struct {
	zapcore.Core
	patterns []redactPattern
}

func newRedactCore(core zapcore.Core, patterns []redactPattern) zapcore.Core {
	return &redactCore{Core: core, patterns: patterns}
}

func (c *redactCore) With(fields []zapcore.Field) zapcore.Core {
	return &redactCore{
		Core:     c.Core.With(fields),
		patterns: c.patterns,
	}
}

func (c *redactCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	redacted := make([]zapcore.Field, len(fields))
	for i, f := range fields {
		redacted[i] = redactField(f, c.patterns)
	}
	return c.Core.Write(entry, redacted)
}

// Check implements zapcore.Core. Must delegate to inner core so CheckedEntry gets our core.
func (c *redactCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(ent.Level) {
		return ce.AddCore(ent, c)
	}
	return ce
}

func redactField(f zapcore.Field, patterns []redactPattern) zapcore.Field {
	if f.Type != zapcore.StringType && f.Type != zapcore.StringerType {
		return f
	}
	var s string
	switch f.Type {
	case zapcore.StringType:
		s = f.String
	case zapcore.StringerType:
		if stringer, ok := f.Interface.(fmt.Stringer); ok {
			s = stringer.String()
		} else {
			return f
		}
	default:
		return f
	}
	for _, p := range patterns {
		if p.Pattern != nil && p.Pattern.MatchString(s) {
			name := p.Name
			if name == "" {
				name = p.Pattern.String()
			}
			return zap.String(f.Key, "[REDACTED:"+name+"]")
		}
	}
	return f
}

func compilePatterns(patterns []string) []redactPattern {
	var result []redactPattern
	for i, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			name := p
			if len(p) > 30 {
				name = fmt.Sprintf("pattern_%d", i)
			}
			result = append(result, redactPattern{Name: name, Pattern: re})
		}
	}
	return result
}

// DefaultRedactPatterns returns the standard patterns for secret redaction (risk register T10).
var DefaultRedactPatterns = []string{
	`(?i)bearer\s+[a-zA-Z0-9\-_.]+`,                               // Bearer tokens
	`(?i)api[_-]?key\s*[:=]\s*["']?[a-zA-Z0-9\-_]+["']?`,         // API keys
	`(?i)password\s*[:=]\s*["']?[^\s"']+["']?`,                   // Passwords
	`(?i)secret\s*[:=]\s*["']?[^\s"']+["']?`,                     // Secrets
	`hvs\.[a-zA-Z0-9]+`,                                           // Vault tokens (hvs. prefix)
	`hvb\.[a-zA-Z0-9]+`,                                           // Vault batch tokens
	`[a-zA-Z0-9\-]+://[^:]+:[^@]+@`,                              // Connection strings (user:pass@)
}
