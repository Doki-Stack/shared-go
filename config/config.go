package config

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// loadConfig holds options for Load.
type loadConfig struct {
	prefix  string
	vault   *VaultConfig
	dotenv  string
	envVars map[string]string // populated after dotenv load
}

// Option configures Load behavior.
type Option func(*loadConfig)

// WithPrefix sets an env var prefix (e.g., "API" → API_PORT, API_HOST).
func WithPrefix(prefix string) Option {
	return func(c *loadConfig) {
		c.prefix = prefix
	}
}

// WithVault enables Vault secret loading. Vault overrides env vars.
func WithVault(vc VaultConfig) Option {
	return func(c *loadConfig) {
		c.vault = &vc
	}
}

// WithDotenv loads a .env file (dev only; NOT for production).
func WithDotenv(path string) Option {
	return func(c *loadConfig) {
		c.dotenv = path
	}
}

// Load populates cfg from environment variables (and optionally Vault).
// cfg must be a pointer to a struct.
// Uses struct tags: env:"VAR_NAME" default:"value" required:"true"
// Returns error if required field is missing or type conversion fails.
func Load(cfg interface{}, opts ...Option) error {
	lc := &loadConfig{envVars: make(map[string]string)}
	for _, opt := range opts {
		opt(lc)
	}

	// Load .env file first (sets lc.envVars)
	if lc.dotenv != "" {
		if err := loadDotenv(lc.dotenv, lc.envVars); err != nil {
			return err
		}
	}

	// Build env map: env vars override dotenv
	for _, e := range os.Environ() {
		if idx := strings.Index(e, "="); idx >= 0 {
			key := e[:idx]
			val := e[idx+1:]
			lc.envVars[key] = val
		}
	}

	// Load from env + defaults first
	if err := loadStruct(cfg, lc); err != nil {
		return err
	}

	// Load from Vault if configured (Vault overrides env)
	if lc.vault != nil {
		if err := loadFromVault(*lc.vault, cfg); err != nil {
			return fmt.Errorf("vault: %w", err)
		}
	}

	return nil
}

func loadDotenv(path string, env map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		val = strings.Trim(val, `"'`)
		env[key] = val
	}
	return nil
}

func getValue(lc *loadConfig, envName string) (string, bool) {
	fullName := lc.prefix + envName
	if v, ok := lc.envVars[fullName]; ok && v != "" {
		return v, true
	}
	if v, ok := lc.envVars[envName]; ok && v != "" {
		return v, true
	}
	return "", false
}

func loadStruct(cfg interface{}, lc *loadConfig) error {
	v := reflect.ValueOf(cfg)
	if v.Kind() != reflect.Ptr {
		return fmt.Errorf("config must be a pointer to struct, got %s", v.Kind())
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("config must be a pointer to struct, got pointer to %s", v.Kind())
	}
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if !field.CanSet() {
			continue
		}
		sf := t.Field(i)
		envTag := sf.Tag.Get("env")
		if envTag == "" || envTag == "-" {
			continue
		}
		defaultVal := sf.Tag.Get("default")
		required := sf.Tag.Get("required") == "true"

		val, ok := getValue(lc, envTag)
		if !ok {
			val = defaultVal
		}
		if val == "" && required {
			return fmt.Errorf("required config %s not set", envTag)
		}
		if val == "" && !required {
			continue
		}
		if err := setField(field, val, envTag); err != nil {
			return err
		}
	}
	return nil
}

func setField(field reflect.Value, val, envName string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(val)
		return nil
	case reflect.Int, reflect.Int32:
		n, err := strconv.ParseInt(val, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid int for %s: %w", envName, err)
		}
		field.SetInt(n)
		return nil
	case reflect.Int64:
		if field.Type() == reflect.TypeOf(time.Duration(0)) {
			d, err := time.ParseDuration(val)
			if err != nil {
				return fmt.Errorf("invalid duration for %s: %w", envName, err)
			}
			field.SetInt(int64(d))
			return nil
		}
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid int64 for %s: %w", envName, err)
		}
		field.SetInt(n)
		return nil
	case reflect.Float64:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return fmt.Errorf("invalid float64 for %s: %w", envName, err)
		}
		field.SetFloat(f)
		return nil
	case reflect.Bool:
		b, err := parseBool(val)
		if err != nil {
			return fmt.Errorf("invalid bool for %s: %w", envName, err)
		}
		field.SetBool(b)
		return nil
	case reflect.Slice:
		if field.Type().Elem().Kind() == reflect.String {
			parts := strings.Split(val, ",")
			result := make([]string, 0, len(parts))
			for _, p := range parts {
				result = append(result, strings.TrimSpace(p))
			}
			field.Set(reflect.ValueOf(result))
			return nil
		}
		return fmt.Errorf("unsupported slice type for %s", envName)
	default:
		return fmt.Errorf("unsupported field type %s for %s", field.Kind(), envName)
	}
}

func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid bool value: %q", s)
	}
}
