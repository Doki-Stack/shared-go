# shared-go Implementation Plan — Module and Project Setup

## 1. go.mod

Full `go.mod` content with module path, Go version, and required dependencies. Use latest stable versions compatible with Go 1.22+.

```go
module github.com/doki-stack/shared-go

go 1.22

require (
	github.com/go-chi/chi/v5 v5.2.5
	github.com/google/uuid v1.6.0
	github.com/hashicorp/vault/api v1.14.0
	github.com/sony/gobreaker/v2 v2.3.0
	go.opentelemetry.io/otel v1.32.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.32.0
	go.opentelemetry.io/otel/sdk v1.32.0
	go.uber.org/zap v1.27.0
	golang.org/x/time v0.11.0
)
```

**Version rationale:**
- `go-chi/chi/v5 v5.2.5` — Latest stable, Go 1.22+
- `google/uuid v1.6.0` — UUID v7 support, widely used
- `hashicorp/vault/api v1.14.0` — Conservative version for Go 1.22; v1.22+ may require Go 1.23+
- `sony/gobreaker/v2 v2.3.0` — Circuit breaker with rolling window
- `opentelemetry.io/otel v1.32.0` — Compatible with Go 1.22; v1.40+ may require newer Go
- `zap v1.27.0` — Latest stable structured logger
- `golang.org/x/time v0.11.0` — Token bucket rate limiter

**Note:** Run `go mod tidy` after creating the module to resolve transitive dependencies and update `go.sum`.

## 2. Directory Layout

Exact file listing for every package:

```
shared-go/
├── go.mod
├── go.sum
├── Makefile
├── .golangci.yml
├── .pre-commit-config.yaml
├── .github/
│   └── workflows/
│       └── ci.yml
├── envelope/
│   ├── envelope.go
│   ├── codes.go
│   ├── http.go
│   └── envelope_test.go
├── logger/
│   ├── logger.go
│   ├── redact.go
│   └── logger_test.go
├── otel/
│   ├── otel.go
│   └── otel_test.go
├── middleware/
│   ├── orgid.go
│   ├── requestid.go
│   ├── logger.go
│   ├── recovery.go
│   ├── context.go
│   └── middleware_test.go
├── breaker/
│   ├── breaker.go
│   └── breaker_test.go
├── ratelimit/
│   ├── limiter.go
│   ├── keyed.go
│   ├── middleware.go
│   └── ratelimit_test.go
├── httpclient/
│   ├── client.go
│   ├── options.go
│   ├── retry.go
│   └── client_test.go
├── health/
│   ├── handler.go
│   ├── checks.go
│   └── health_test.go
├── config/
│   ├── config.go
│   ├── vault.go
│   └── config_test.go
└── docs/
    ├── design.md
    └── implementation-plan/
        ├── 00-overview.md
        └── 01-module-and-project-setup.md
```

## 3. Makefile

Targets for build, test, lint, fmt, vet, coverage, tidy:

```makefile
.PHONY: build test lint fmt vet coverage tidy clean

# Default target
all: lint test

# Build all packages
build:
	go build ./...

# Run tests
test:
	go test -race -count=1 ./...

# Run tests with coverage
coverage:
	go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Run linter
lint:
	golangci-lint run ./...

# Format code
fmt:
	go fmt ./...
	goimports -w .

# Run go vet
vet:
	go vet ./...

# Tidy module dependencies
tidy:
	go mod tidy
	go mod verify

# Clean build artifacts
clean:
	rm -f coverage.out coverage.html

# Install development tools (optional)
tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	go install golang.org/x/tools/cmd/goimports@latest
```

## 4. .golangci.yml

Linter configuration with govet, errcheck, staticcheck, unused, gosimple, ineffassign, typecheck, revive, gofmt, goimports, misspell:

```yaml
run:
  timeout: 5m
  modules-download-mode: readonly
  go: '1.22'

linters:
  enable:
    - govet
    - errcheck
    - staticcheck
    - unused
    - gosimple
    - ineffassign
    - typecheck
    - revive
    - gofmt
    - goimports
    - misspell

linters-settings:
  govet:
    enable-all: true
    disable:
      - fieldalignment  # Optional: can enable for memory optimization
  errcheck:
    check-type-assertions: true
    check-blank: true
  revive:
    rules:
      - name: exported
        severity: warning
        arguments:
          - ["error", "Error"]
  gofmt:
    simplify: true
  goimports:
    local-prefixes: github.com/doki-stack/shared-go

issues:
  exclude-rules:
    - path: _test\.go
      linters:
        - errcheck
        - gosimple
  max-issues-per-linter: 0
  max-same-issues: 0
```

## 5. .pre-commit-config.yaml

Pre-commit hooks for go-fmt, go-vet, golangci-lint:

```yaml
repos:
  - repo: local
    hooks:
      - id: go-fmt
        name: go fmt
        entry: go fmt ./...
        language: system
        pass_filenames: false

      - id: go-vet
        name: go vet
        entry: go vet ./...
        language: system
        pass_filenames: false

      - id: golangci-lint
        name: golangci-lint
        entry: golangci-lint run ./...
        language: system
        pass_filenames: false
```

**Alternative (using pre-commit Go hooks):**

```yaml
repos:
  - repo: https://github.com/dnephin/pre-commit-golang
    rev: v1.1.0
    hooks:
      - id: go-fmt
      - id: go-vet
      - id: golangci-lint
        args: [--enable-all]
```

## 6. Build Tags

**No CE/EE split in shared-go.** All packages are available to all consumer services. There are no build tags or conditional compilation. Services that need EE-only behavior handle that in their own code, not in shared-go.

## 7. Versioning

| Strategy | Details |
|----------|---------|
| **Scheme** | Semantic versioning (SemVer) |
| **Tags** | `v0.1.0`, `v0.2.0`, … `v1.0.0` at Phase 1 launch |
| **Pre-1.0** | v0.x.x for Phase 0; API may change between minor versions |
| **1.0** | Declared when Phase 1 services ship; API stability guaranteed |

**Consumer usage:**
```bash
go get github.com/doki-stack/shared-go@v0.1.0
```

In consumer `go.mod`:
```go
require github.com/doki-stack/shared-go v0.1.0
```

## 8. Go Workspace Consideration

Consumers use `go get github.com/doki-stack/shared-go@v0.x.x`. shared-go is a **separate module**, not part of a Go workspace. Each consumer:

1. Adds shared-go as a dependency in its own `go.mod`
2. Pins a specific version via `go get` or `go mod edit`
3. Imports packages as `github.com/doki-stack/shared-go/envelope`, etc.

**No `go.work` file** is required for shared-go itself. If the monorepo uses a workspace for local development, consumers can use `replace` directives for local iteration:

```go
replace github.com/doki-stack/shared-go => ../shared-go
```

## 9. CI Workflow (.github/workflows/ci.yml)

```yaml
name: CI

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main, develop]

jobs:
  lint-and-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      - name: Cache Go modules
        uses: actions/cache@v4
        with:
          path: |
            ~/go/pkg/mod
            ~/.cache/go-build
          key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}
          restore-keys: |
            ${{ runner.os }}-go-

      - name: Download dependencies
        run: go mod download

      - name: Verify
        run: go mod verify

      - name: Lint
        uses: golangci/golangci-lint-action@v6
        with:
          version: latest

      - name: Test
        run: go test -race -count=1 ./...
```
