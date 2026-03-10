# shared-go Implementation Plan — Overview

## 1. Module Identity

| Attribute | Value |
|-----------|-------|
| **Module path** | `github.com/doki-stack/shared-go` |
| **Go version** | 1.22+ |
| **License** | Apache 2.0 |
| **Repository** | Separate repo with own `go.mod` (not in monorepo per ADR-001 note) |

## 2. Purpose

shared-go is the **foundational Go library** for the Doki Stack platform. All 9 Go microservices depend on it for:

- Standardized error handling and HTTP responses
- Structured logging with trace context and secret redaction
- OpenTelemetry instrumentation
- HTTP middleware (org_id, auth, logging, recovery)
- Resilience (circuit breaker, rate limiting)
- Health checks with fail-closed support
- Retrying HTTP client with tracing
- Configuration loading (env + optional Vault)

**Critical constraint:** shared-go has **no dependency** on other Doki Stack repos. It is a standalone, reusable module.

## 3. Directory Structure

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

## 4. Package Summary Table

| Package | Primary Exports | Description |
|---------|-----------------|-------------|
| `envelope` | `Error`, `Envelope`, `WriteError`, `ErrorCode` | Standard error envelope factory; JSON error responses with trace_id, org_id, retryable |
| `logger` | `NewLogger`, `FromContext`, `WithTrace`, `Redact` | Structured zap logger with trace context and secret redaction |
| `otel` | `InitTracer`, `InitMeter`, `Shutdown` | OpenTelemetry SDK initialization (traces, metrics) |
| `middleware` | `OrgID`, `RequestID`, `Logger`, `Recovery`, `GetOrgID` | chi middleware for org_id, request ID, logging, panic recovery |
| `breaker` | `Wrap`, `NewSettings` | Circuit breaker wrapper around gobreaker |
| `ratelimit` | `Limiter`, `KeyedLimiter`, `Middleware` | Token bucket rate limiter with per-key support |
| `health` | `Handler`, `Checker`, `RegisterCheck`, `FailClosed` | Health check HTTP handlers with fail-closed pattern |
| `httpclient` | `NewClient`, `WithRetry`, `WithTracing` | Retrying HTTP client with OpenTelemetry tracing |
| `config` | `Load`, `LoadFromVault`, `Env` | Environment variable and optional Vault config loader |

## 5. Dependency Map

| Package | External Dependencies | Internal Dependencies |
|---------|----------------------|------------------------|
| `config` | `github.com/hashicorp/vault/api` (optional), `github.com/google/uuid` | — |
| `envelope` | `github.com/google/uuid` | — |
| `logger` | `go.uber.org/zap` | `config` (for service name) |
| `otel` | `go.opentelemetry.io/otel`, `otel/sdk`, `otel/exporters/otlp` | `logger` |
| `middleware` | `github.com/go-chi/chi/v5` | `logger`, `otel`, `envelope` |
| `breaker` | `github.com/sony/gobreaker/v2` | `logger` |
| `ratelimit` | `golang.org/x/time/rate`, `github.com/go-chi/chi/v5` | `logger` |
| `httpclient` | `net/http`, `go.opentelemetry.io/otel` | `logger`, `otel`, `breaker` |
| `health` | `net/http` | `logger` |

## 6. Consumer Matrix

| Service | envelope | logger | otel | middleware | breaker | ratelimit | health | httpclient | config |
|---------|:--------:|:------:|:----:|:----------:|:------:|:---------:|:------:|:----------:|:------:|
| **api-server** (CE Phase 1) | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| **mcp-policy** (CE Phase 1) | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| **mcp-memory** (EE Phase 3) | ✓ | ✓ | ✓ | ✓ | ✓ | — | ✓ | — | ✓ |
| **mcp-registry** (EE Phase 4) | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| **ee-multi-tenancy** (EE Phase 4) | ✓ | ✓ | ✓ | ✓ | — | — | ✓ | ✓ | ✓ |
| **ee-notifications** (EE Phase 4) | ✓ | ✓ | ✓ | ✓ | — | ✓ | ✓ | ✓ | ✓ |
| **ee-compliance** (EE Phase 4) | ✓ | ✓ | ✓ | ✓ | — | — | ✓ | — | ✓ |
| **ee-governance** (EE Phase 4) | ✓ | ✓ | ✓ | ✓ | — | — | ✓ | ✓ | ✓ |
| **ee-dashboards** (EE Phase 4) | ✓ | ✓ | ✓ | ✓ | — | — | ✓ | ✓ | ✓ |

**Notable usage:**
- **mcp-policy**: `breaker` for Qdrant calls, `httpclient` for Ollama
- **mcp-registry**: `breaker` for MCP proxy, `ratelimit` for MCP rate limits

## 7. Phase Mapping

| Phase | Scope | Timeline |
|-------|-------|----------|
| **Phase 0** | shared-go foundation | Weeks 1–6 |
| **Phase 1** | CE services (api-server, mcp-policy) — **depends on Phase 0 complete** | After Phase 0 |
| **Phase 3** | EE services (mcp-memory, ee-governance, ee-compliance) | After Phase 1 |
| **Phase 4** | EE services (mcp-registry, ee-multi-tenancy, ee-notifications, ee-dashboards) | After Phase 3 |

**Critical:** Phase 0 must be **complete** before any Phase 1 service implementation begins.

## 8. Implementation Order

Packages are implemented in dependency layers:

| Layer | Packages | Rationale |
|-------|----------|-----------|
| **1** | `config` | No dependencies on other shared-go packages |
| **2** | `envelope`, `logger` | Depend only on `config` (logger uses config for service name) |
| **3** | `otel` | Depends on `logger` |
| **4** | `middleware` | Depends on `logger`, `otel`, `envelope` |
| **5** | `breaker`, `ratelimit` | Depend on `logger` |
| **6** | `httpclient` | Depends on `logger`, `otel`, `breaker`; uses middleware context types |
| **7** | `health` | Depends on `logger` |

**Suggested implementation sequence:**
1. config
2. envelope
3. logger
4. otel
5. middleware
6. breaker
7. ratelimit
8. httpclient
9. health

## 9. Effort Estimate

| Activity | Estimate |
|----------|----------|
| config | 0.5 day |
| envelope | 0.5 day |
| logger | 0.5 day |
| otel | 0.5 day |
| middleware | 1 day |
| breaker | 0.5 day |
| ratelimit | 0.5 day |
| httpclient | 0.5 day |
| health | 0.5 day |
| CI, docs, integration | 0.5 day |
| **Total** | **4–5 days** |

## 10. Non-Negotiable Rules

1. **org_id everywhere** — Middleware MUST reject requests without valid `org_id`. All tables, APIs, logs, and cache keys are scoped by org_id.
2. **Fail-closed for health** — Health checks must support fail-closed pattern (ADR-005). When policy/context is unavailable, the system blocks.
3. **Secrets never in logs** — Redaction required (risk register T10). Use `logger.Redact()` for any secret-like values.
4. **No dependency on other Doki repos** — shared-go is standalone. Consumers use `go get github.com/doki-stack/shared-go@v0.x.x`.
5. **Error envelope format** — JSON: `{"error_code": "DOMAIN_CODE", "message": "...", "trace_id": "...", "org_id": "...", "retryable": false}`
6. **Logging format** — Structured JSON with: `timestamp`, `level`, `trace_id`, `span_id`, `org_id`, `service`, `message`, `event_type`
