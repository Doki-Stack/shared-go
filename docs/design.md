# shared-go — High-Level Design

## Overview

`shared-go` is the foundational Go module for the Doki Stack platform. It provides standardized utilities that all Go microservices import to ensure consistent error handling, logging, observability, and request processing across the platform.

## Architecture

```
shared-go/
├── envelope/       # Standard error envelope factory
├── logger/         # Structured zap logger with trace context
├── otel/           # OpenTelemetry SDK initialization
├── middleware/      # chi middleware (org_id, auth, logging, recovery)
├── breaker/        # Circuit breaker wrapper around gobreaker
├── ratelimit/      # Token bucket rate limiter
├── health/         # Health check HTTP handlers
├── httpclient/     # Retrying HTTP client with tracing
└── config/         # Environment variable and Vault config loader
```

## Key Design Decisions

1. **Zap over zerolog** — Zap provides better performance for structured logging and is widely adopted in the Go ecosystem.
2. **chi middleware pattern** — All middleware follows the `func(next http.Handler) http.Handler` pattern for composability.
3. **org_id is mandatory** — The org_id middleware rejects any request without a valid `X-Org-Id` header or path parameter.
4. **OTel auto-configuration** — The OTel init function reads exporter endpoints from environment variables, matching the infrastructure setup (Tempo for traces, Prometheus for metrics).

## Dependencies

- No dependency on any other Doki Stack repository
- All Go services depend on this module

## Consumers

| Service | What It Uses |
|---------|-------------|
| `api-server` | All packages |
| `mcp-policy` | All packages |
| `mcp-memory` (EE) | All packages |
| `mcp-registry` (EE) | All packages |
| All EE Go services | All packages |
