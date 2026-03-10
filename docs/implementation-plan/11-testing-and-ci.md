# shared-go Implementation Plan — Testing and CI/CD

## 1. Overview

This document defines the testing strategy, CI workflow, release process, pre-commit hooks, and Makefile targets for the shared-go module. All packages must meet the quality bar: unit tests, 80%+ coverage, lint-clean, and no external service dependencies in CI.

## 2. Testing Strategy

### 2.1 Unit Tests

- Every package has `*_test.go` files
- Test exported API surface
- Use Go's standard `testing` package

### 2.2 Table-Driven Tests

Use Go's standard table-driven test pattern:

```go
func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int
		wantErr bool
	}{
		{"valid", "42", 42, false},
		{"invalid", "x", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Parse() = %v, want %v", got, tt.want)
			}
		})
	}
}
```

### 2.3 Test Helpers

- `testutil/` package (if needed) for shared fixtures
- Keep helpers minimal; prefer inline setup in tests

### 2.4 Mocks

- Use interfaces + manual mocks (no framework dependency)
- For HTTP: `net/http/httptest.Server`
- For Vault: mock HTTP server or Vault dev mode

### 2.5 Coverage Target

**80%+** line coverage across all packages.

## 3. Per-Package Test Summary

| Package | Key Test Areas | External Deps |
|---------|----------------|----------------|
| envelope | Construction, JSON, error interface, HTTP helpers | None |
| logger | Creation, levels, context, redaction, JSON format | None |
| otel | Init, shutdown, helpers, env var reading | Mock OTLP receiver |
| middleware | OrgID validation, RequestID, logging, recovery | httptest |
| breaker | State transitions, thresholds, callbacks | None |
| ratelimit | Allow/deny, keyed limits, TTL, middleware | httptest |
| httpclient | Retry, backoff, tracing, org_id propagation, size limit | httptest |
| health | Check pass/fail, liveness/readiness, timeout | httptest, mock checks |
| config | Env loading, defaults, required, Vault, types | Mock Vault |

## 4. CI Workflow

**File:** `.github/workflows/ci.yml`

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.22'

      - name: Tidy
        run: go mod tidy && git diff --exit-code go.mod go.sum

      - name: Lint
        uses: golangci/golangci-lint-action@v6
        with:
          version: latest

      - name: Build
        run: go build ./...

      - name: Test
        run: go test -v -race -coverprofile=coverage.txt -covermode=atomic ./...

      - name: Coverage
        run: go tool cover -func=coverage.txt

  release:
    runs-on: ubuntu-latest
    if: startsWith(github.ref, 'refs/tags/v')
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Validate tag
        run: echo "Release ${GITHUB_REF_NAME}"
```

## 5. Release Strategy

### 5.1 Semantic Versioning

- **v0.1.0** — Initial release (Phase 0 foundation)
- **v0.2.0** — Add package (e.g., httpclient, health)
- **v1.0.0** — Stable at Phase 1 launch

### 5.2 Tag Creation

```bash
git tag v0.1.0
git push origin v0.1.0
```

### 5.3 Go Module Proxy

Tags are picked up automatically by the Go module proxy. Consumers use:

```bash
go get github.com/doki-stack/shared-go@v0.1.0
```

### 5.4 CHANGELOG.md

Maintained manually. Include:

- Added/Changed/Fixed/Removed sections
- Breaking changes highlighted
- Version and date for each release

### 5.5 Breaking Changes

Require **major version bump** (e.g., v1.0.0 → v2.0.0). Update module path if needed: `github.com/doki-stack/shared-go/v2`.

## 6. Pre-Commit Hooks

**File:** `.pre-commit-config.yaml`

```yaml
repos:
  - repo: https://github.com/dnephin/pre-commit-golang
    rev: v0.5.1
    hooks:
      - id: go-fmt
      - id: go-vet
      - id: go-imports
      - id: golangci-lint
      - id: go-build
      - id: go-mod-tidy
```

**Installation:**

```bash
pip install pre-commit
pre-commit install
```

## 7. Makefile

**File:** `Makefile`

```makefile
.PHONY: build test lint fmt vet tidy coverage clean

build:
	go build ./...

test:
	go test -v -race ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -s -w .
	goimports -w .

vet:
	go vet ./...

tidy:
	go mod tidy

coverage:
	go test -coverprofile=coverage.txt -covermode=atomic ./...
	go tool cover -html=coverage.txt -o coverage.html

clean:
	rm -f coverage.txt coverage.html
```

## 8. Development Workflow

1. Create feature branch: `git checkout -b feat/httpclient`
2. Implement package + tests
3. Run `make lint test`
4. Open PR
5. CI validates: lint, build, test with race detector
6. Merge to main
7. Tag release when ready: `git tag v0.x.x && git push origin v0.x.x`

## 9. Integration Testing Notes

- **health/checks.go** — PostgresCheck, QdrantCheck, DragonflyCheck, RabbitMQCheck, VaultCheck are best tested in consumer services' integration tests (require real or containerized services)
- **config/vault.go** — Vault integration tested with mock Vault server or Vault dev mode (`vault server -dev`)
- **httpclient** — Tested with `httptest.Server`; no real HTTP calls in CI
- **No external service dependencies** in shared-go CI — all mocked

## 10. CI Triggers Summary

| Event | Actions |
|-------|---------|
| Push to main | Lint, build, test |
| Pull request to main | Lint, build, test |
| Tag push (v*) | Release job (validate tag) |

## 11. Quality Gates

| Gate | Command | Required |
|------|---------|----------|
| Format | `gofmt -s -l .` | Yes |
| Vet | `go vet ./...` | Yes |
| Lint | `golangci-lint run ./...` | Yes |
| Build | `go build ./...` | Yes |
| Test | `go test -race ./...` | Yes |
| Coverage | 80%+ line coverage | Target |
