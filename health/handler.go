package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// Status is the JSON response for health endpoints.
type Status struct {
	Status string            `json:"status"` // "healthy" or "unhealthy"
	Checks map[string]string `json:"checks"` // check name → "ok" or error message
}

const (
	healthzPath = "/healthz"
	readyzPath  = "/readyz"
)

// Handler returns an http.Handler that mounts /healthz (liveness) and /readyz (readiness)
// on a chi sub-router. Readiness runs all checks.
func Handler(checks ...Check) http.Handler {
	r := chi.NewRouter()
	r.Get(healthzPath, LivenessHandler().ServeHTTP)
	r.Get(readyzPath, ReadinessHandler(checks...).ServeHTTP)
	return r
}

// LivenessHandler returns an http.Handler for /healthz that always returns 200.
// Proves the process is alive; no dependency checks.
func LivenessHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := Status{Status: "healthy"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(status)
	})
}

// ReadinessHandler returns an http.Handler for /readyz that runs all checks.
// Returns 200 if all pass, 503 if any fail.
func ReadinessHandler(checks ...Check) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := runChecks(r.Context(), checks)
		w.Header().Set("Content-Type", "application/json")
		if status.Status == "healthy" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(status)
	})
}

func runChecks(ctx context.Context, checks []Check) Status {
	status := Status{
		Status: "healthy",
		Checks: make(map[string]string, len(checks)),
	}
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, check := range checks {
		wg.Add(1)
		go func(c Check) {
			defer wg.Done()
			checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			err := c.Check(checkCtx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				status.Status = "unhealthy"
				status.Checks[c.Name()] = err.Error()
			} else {
				status.Checks[c.Name()] = "ok"
			}
		}(check)
	}

	wg.Wait()
	return status
}
