package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReadiness_AllHealthy(t *testing.T) {
	handler := ReadinessHandler(
		NewCheck("check1", func(ctx context.Context) error { return nil }),
		NewCheck("check2", func(ctx context.Context) error { return nil }),
	)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var status Status
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&status))
	require.Equal(t, "healthy", status.Status)
	require.Equal(t, "ok", status.Checks["check1"])
	require.Equal(t, "ok", status.Checks["check2"])
}

func TestReadiness_OneUnhealthy(t *testing.T) {
	handler := ReadinessHandler(
		NewCheck("db", func(ctx context.Context) error { return nil }),
		NewCheck("qdrant", func(ctx context.Context) error {
			return context.DeadlineExceeded
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var status Status
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&status))
	require.Equal(t, "unhealthy", status.Status)
	require.Equal(t, "ok", status.Checks["db"])
	require.NotEmpty(t, status.Checks["qdrant"])
}

func TestLiveness_AlwaysHealthy(t *testing.T) {
	handler := LivenessHandler()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var status Status
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&status))
	require.Equal(t, "healthy", status.Status)
}

func TestReadiness_CheckTimeout(t *testing.T) {
	handler := ReadinessHandler(
		NewCheck("slow", func(ctx context.Context) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Second):
				return nil
			}
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestReadiness_NoChecks(t *testing.T) {
	handler := ReadinessHandler()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var status Status
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&status))
	require.Equal(t, "healthy", status.Status)
	require.Empty(t, status.Checks)
}

func TestHandler_MountsBothEndpoints(t *testing.T) {
	handler := Handler(
		NewCheck("ok", func(ctx context.Context) error { return nil }),
	)

	reqHealth := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recHealth := httptest.NewRecorder()
	handler.ServeHTTP(recHealth, reqHealth)
	require.Equal(t, http.StatusOK, recHealth.Code)

	reqReady := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	recReady := httptest.NewRecorder()
	handler.ServeHTTP(recReady, reqReady)
	require.Equal(t, http.StatusOK, recReady.Code)
}
