# shared-go Implementation Plan — Config Package

## 1. Overview

The `config` package provides environment variable and optional Vault secret loading for service configuration. It uses struct tags to map env vars, supports defaults, required fields, and type conversion. Secrets are loaded from Vault in production—never from env vars or config files (risk register T10).

**Package name:** `config`

**Critical:** config.Load NEVER logs secret values. Sensitive fields use `sensitive:"true"` tag for masking in debug output.

## 2. Files

| File | Purpose |
|------|---------|
| `config.go` | Load function, struct tag parsing, type conversion |
| `vault.go` | VaultConfig, loadFromVault |
| `config_test.go` | Unit tests |

## 3. Core API (config.go)

```go
package config

// Load populates cfg from environment variables (and optionally Vault).
// cfg must be a pointer to a struct.
// Uses struct tags: env:"VAR_NAME" default:"value" required:"true"
// Returns error if required field is missing or type conversion fails.
func Load(cfg interface{}, opts ...Option) error
```

### 3.1 Struct Tags

| Tag | Purpose |
|-----|---------|
| `env:"VAR_NAME"` | Environment variable name |
| `default:"value"` | Default value if env var not set |
| `required:"true"` | Error if not set (after env and Vault) |
| `sensitive:"true"` | Mask value in any debug output |
| `validate:"url"` | Optional: validate URL format |

### 3.2 Type Support

| Go Type | Env Format | Example |
|---------|------------|---------|
| string | Any | `"http://localhost:8080"` |
| int, int64 | Integer | `"8080"` |
| float64 | Float | `"1.5"` |
| bool | true/false, 1/0 | `"true"`, `"1"` |
| time.Duration | Go duration | `"30s"`, `"5m"` |
| []string | Comma-separated | `"a,b,c"` |

## 4. Options

```go
// Option configures Load behavior.
type Option func(*loadConfig)

// WithPrefix sets an env var prefix (e.g., "API" → API_PORT, API_HOST).
func WithPrefix(prefix string) Option

// WithVault enables Vault secret loading. Vault overrides env vars.
func WithVault(vc VaultConfig) Option

// WithDotenv loads a .env file (dev only; NOT for production).
func WithDotenv(path string) Option
```

## 5. Example Config Struct

```go
type Config struct {
	Port           int           `env:"PORT" default:"8080"`
	DatabaseURL    string        `env:"DATABASE_URL" required:"true"`
	QdrantURL      string        `env:"QDRANT_URL" default:"http://qdrant.doki-data:6333"`
	DragonflyURL   string        `env:"DRAGONFLY_URL" default:"dragonfly.doki-data:6379"`
	RabbitMQURL    string        `env:"RABBITMQ_URL" default:"amqp://guest:guest@rabbitmq.doki-data:5672/"`
	OtelEndpoint   string        `env:"OTEL_EXPORTER_OTLP_ENDPOINT" default:"tempo.monitoring:4317"`
	ReadTimeout    time.Duration `env:"READ_TIMEOUT" default:"30s"`
	WriteTimeout   time.Duration `env:"WRITE_TIMEOUT" default:"30s"`
	ShutdownGrace  time.Duration `env:"SHUTDOWN_GRACE" default:"10s"`
	CircuitBreakerMaxFailures int `env:"CIRCUIT_BREAKER_MAX_FAILURES" default:"5"`
	CircuitBreakerTimeout     time.Duration `env:"CIRCUIT_BREAKER_TIMEOUT" default:"60s"`
	RateLimitRequests int     `env:"RATE_LIMIT_REQUESTS" default:"100"`
	RateLimitWindow   time.Duration `env:"RATE_LIMIT_WINDOW" default:"1m"`
	// Auth0 (from Vault in prod)
	Auth0Domain     string `env:"AUTH0_DOMAIN"`      // Vault
	Auth0Audience   string `env:"AUTH0_AUDIENCE"`    // Vault
	Auth0ClientID   string `env:"AUTH0_CLIENT_ID" sensitive:"true"` // Vault
	Auth0ClientSecret string `env:"AUTH0_CLIENT_SECRET" sensitive:"true"` // Vault
}
```

## 6. Vault Integration (vault.go)

```go
// VaultConfig configures Vault secret loading.
type VaultConfig struct {
	Address string // VAULT_ADDR
	Token   string // VAULT_TOKEN (dev only; prod uses K8s auth)
	Path    string // secret/data/services/{service-name}
}

// loadFromVault reads KV v2 secrets from the specified path and merges into cfg.
// Uses github.com/hashicorp/vault/api.
func loadFromVault(vc VaultConfig, cfg interface{}) error
```

### 6.1 Vault Behavior

- Reads KV v2 secrets from `Path` (e.g., `secret/data/services/mcp-policy`)
- Maps Vault keys to struct fields by env tag name (case-insensitive)
- Vault secrets override env vars
- Priority order: **Vault > env vars > defaults**
- If Vault is configured but unreachable → error (fail closed for secrets)

### 6.2 Vault Key Mapping

Vault key `auth0_client_id` maps to struct field with `env:"AUTH0_CLIENT_ID"`.

## 7. Validation

| Condition | Error |
|-----------|-------|
| Required field not set | `fmt.Errorf("required config %s not set", envName)` |
| Invalid type conversion | Descriptive error (e.g., "invalid int for PORT") |
| Invalid duration | "invalid duration for READ_TIMEOUT" |
| Vault unreachable | "vault: connection refused" or similar |

## 8. Security Constraints

1. **Never log secrets** — config.Load and any helper NEVER log values of fields with `sensitive:"true"`
2. **Regex redaction** — If debug output includes config, redact known patterns (passwords, tokens)
3. **Vault in production** — DATABASE_URL, AUTH0_CLIENT_SECRET, etc. loaded from Vault
4. **No .env in production** — WithDotenv is for local dev only; production uses env vars or Vault

## 9. Dependencies

| Dependency | Purpose |
|------------|---------|
| `github.com/hashicorp/vault/api` | Vault client |
| `reflect` | Struct field iteration |
| `os` | Environment variables |

**Internal:** None. Config is a foundation layer.

## 10. Test Plan (config_test.go)

| Test Case | Description |
|-----------|-------------|
| **Load from env** | Set env vars, Load populates struct correctly |
| **Default values** | Unset env vars → defaults applied |
| **Required missing** | Required field not set → error |
| **Type conversions** | int, bool, duration, []string parsed correctly |
| **Vault integration** | Mock Vault server → secrets override env |
| **Priority** | Vault > env > defaults |
| **Prefix** | WithPrefix("API") → API_PORT used |
| **Invalid struct** | Non-pointer, non-struct → error |
| **Invalid type** | Unsupported field type → error |
| **Sensitive masking** | Debug output does not contain sensitive values |

### Example Test Snippets

```go
func TestLoad_FromEnv(t *testing.T) {
	os.Setenv("PORT", "9090")
	os.Setenv("DATABASE_URL", "postgres://localhost/test")
	defer os.Unsetenv("PORT")
	defer os.Unsetenv("DATABASE_URL")

	var cfg struct {
		Port        int    `env:"PORT" default:"8080"`
		DatabaseURL string `env:"DATABASE_URL" required:"true"`
	}
	err := config.Load(&cfg)
	require.NoError(t, err)
	require.Equal(t, 9090, cfg.Port)
	require.Equal(t, "postgres://localhost/test", cfg.DatabaseURL)
}

func TestLoad_Defaults(t *testing.T) {
	var cfg struct {
		Port int `env:"PORT" default:"8080"`
	}
	err := config.Load(&cfg)
	require.NoError(t, err)
	require.Equal(t, 8080, cfg.Port)
}

func TestLoad_RequiredMissing(t *testing.T) {
	os.Unsetenv("DATABASE_URL")
	var cfg struct {
		DatabaseURL string `env:"DATABASE_URL" required:"true"`
	}
	err := config.Load(&cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "DATABASE_URL")
}

func TestLoad_Duration(t *testing.T) {
	os.Setenv("READ_TIMEOUT", "45s")
	defer os.Unsetenv("READ_TIMEOUT")
	var cfg struct {
		ReadTimeout time.Duration `env:"READ_TIMEOUT" default:"30s"`
	}
	err := config.Load(&cfg)
	require.NoError(t, err)
	require.Equal(t, 45*time.Second, cfg.ReadTimeout)
}

func TestLoad_Prefix(t *testing.T) {
	os.Setenv("API_PORT", "7070")
	defer os.Unsetenv("API_PORT")
	var cfg struct {
		Port int `env:"PORT" default:"8080"`
	}
	err := config.Load(&cfg, config.WithPrefix("API_"))
	require.NoError(t, err)
	require.Equal(t, 7070, cfg.Port)
}
```

## 11. Full API Surface Summary

```go
// Core
func Load(cfg interface{}, opts ...Option) error

// Options
func WithPrefix(prefix string) Option
func WithVault(vc VaultConfig) Option
func WithDotenv(path string) Option

// Vault
type VaultConfig struct { Address, Token, Path string }
```
