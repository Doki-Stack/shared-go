# shared-go

Shared Go module for the Doki Stack platform. Provides foundational utilities used by all Go-based microservices.

## Packages

| Package | Import Path | Description |
|---------|-------------|-------------|
| config | `github.com/doki-stack/shared-go/config` | Environment loading, defaults, required vars, Vault integration |
| envelope | `github.com/doki-stack/shared-go/envelope` | Standard JSON error format with `error_code`, `message`, `trace_id`, `org_id`, `retryable` |
| logger | `github.com/doki-stack/shared-go/logger` | Zap-based structured logger with trace context and secret redaction |
| otel | `github.com/doki-stack/shared-go/otel` | OpenTelemetry init (TracerProvider, MeterProvider) for Tempo and Prometheus |
| middleware | `github.com/doki-stack/shared-go/middleware` | Chi middleware: org_id validation, request ID, logging, panic recovery |
| breaker | `github.com/doki-stack/shared-go/breaker` | Circuit breaker using gobreaker |
| ratelimit | `github.com/doki-stack/shared-go/ratelimit` | Token bucket rate limiter and middleware |
| health | `github.com/doki-stack/shared-go/health` | `/healthz` and `/readyz` endpoints with pluggable checks |
| httpclient | `github.com/doki-stack/shared-go/httpclient` | Retrying HTTP client with tracing, circuit breaker, org_id propagation |

## Quick Start

```go
package main

import (
    "context"
    "net/http"
    "time"

    "github.com/doki-stack/shared-go/health"
    "github.com/doki-stack/shared-go/httpclient"
    "github.com/doki-stack/shared-go/logger"
    "github.com/doki-stack/shared-go/middleware"
    "github.com/doki-stack/shared-go/otel"
    "github.com/go-chi/chi/v5"
)

func main() {
    ctx := context.Background()

    log, _ := logger.New("my-service")
    _ = log

    shutdown, _ := otel.Init(ctx, "my-service")
    defer shutdown(ctx)

    client := httpclient.New(
        httpclient.WithTimeout(30*time.Second),
        httpclient.WithRetries(3),
    )
    resp, err := client.Get(middleware.ContextWithOrgID(ctx, "org-123"), "https://api.example.com/data")
    if err == nil {
        defer resp.Body.Close()
    }

    r := chi.NewRouter()
    r.Use(middleware.OrgID)
    r.Mount("/health", health.Handler())
    http.ListenAndServe(":8080", r)
}
```

## Install

```bash
go get github.com/doki-stack/shared-go
```

## License

Apache License 2.0 — see [LICENSE](LICENSE)
