# shared-go

Shared Go module for the Doki Stack platform. Provides foundational utilities used by all Go-based microservices.

## Purpose

This module contains the common patterns and utilities that every Go service in the Doki Stack platform depends on. It enforces consistency across services for error handling, logging, observability, and middleware.

## Technology Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.22+ |
| Logging | go.uber.org/zap |
| Observability | go.opentelemetry.io/otel |
| Circuit Breaker | github.com/sony/gobreaker |
| Rate Limiting | golang.org/x/time/rate |
| HTTP Client | net/http with retry and tracing |

## What's Included

- **Error Envelope** — Standard JSON error format with `error_code`, `message`, `trace_id`, `org_id`, `retryable`
- **Structured Logger** — Zap-based logger with `trace_id`, `span_id`, `org_id`, `service`, `event_type`
- **OTel SDK Init** — TracerProvider and MeterProvider setup for Tempo and Prometheus
- **org_id Middleware** — chi middleware that extracts and validates `org_id` from headers
- **Circuit Breaker** — Configurable circuit breaker using gobreaker
- **Rate Limiter** — Token bucket rate limiter
- **Health Check Handler** — Standard `/healthz` and `/readyz` endpoints
- **HTTP Client** — Retrying HTTP client with timeout, tracing, and circuit breaker

## Package

```
go get github.com/doki-stack/shared-go
```

## Implementation Phase

**Phase 0** — Foundation. This is one of the first repositories built as all Go services depend on it.

## License

Apache License 2.0 — see [LICENSE](LICENSE)
