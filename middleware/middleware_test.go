package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/doki-stack/shared-go/envelope"
	"github.com/doki-stack/shared-go/logger"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

func TestOrgID_ValidHeader(t *testing.T) {
	orgID := "a0000000-0000-0000-0000-000000000001"
	var capturedCtx context.Context
	handler := OrgID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Org-Id", orgID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, orgID, OrgIDFromContext(capturedCtx))
	// Verify logger can read it via string key
	require.Equal(t, orgID, capturedCtx.Value("org_id"))
}

func TestOrgID_ValidURLParam(t *testing.T) {
	orgID := "a0000000-0000-0000-0000-000000000001"
	var capturedCtx context.Context
	r := chi.NewRouter()
	r.Route("/orgs/{org_id}", func(r chi.Router) {
		r.Use(OrgID)
		r.Get("/foo", func(w http.ResponseWriter, r *http.Request) {
			capturedCtx = r.Context()
			w.WriteHeader(http.StatusOK)
		})
	})
	req := httptest.NewRequest("GET", "/orgs/"+orgID+"/foo", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, orgID, OrgIDFromContext(capturedCtx))
}

func TestOrgID_HeaderOverridesParam(t *testing.T) {
	headerOrgID := "a0000000-0000-0000-0000-000000000001"
	paramOrgID := "b0000000-0000-0000-0000-000000000002"
	var capturedCtx context.Context
	r := chi.NewRouter()
	r.Route("/orgs/{org_id}", func(r chi.Router) {
		r.Use(OrgID)
		r.Get("/foo", func(w http.ResponseWriter, r *http.Request) {
			capturedCtx = r.Context()
			w.WriteHeader(http.StatusOK)
		})
	})
	req := httptest.NewRequest("GET", "/orgs/"+paramOrgID+"/foo", nil)
	req.Header.Set("X-Org-Id", headerOrgID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, headerOrgID, OrgIDFromContext(capturedCtx))
}

func TestOrgID_Missing(t *testing.T) {
	handler := OrgID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	var env envelope.Envelope
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&env))
	require.Equal(t, envelope.BadRequest, env.ErrorCode)
}

func TestOrgID_InvalidUUID(t *testing.T) {
	handler := OrgID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Org-Id", "not-a-uuid")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	var env envelope.Envelope
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&env))
	require.Equal(t, envelope.BadRequest, env.ErrorCode)
}

func TestRequestID_WithHeader(t *testing.T) {
	requestID := "abc-123"
	var capturedID string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-Id", requestID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, requestID, capturedID)
	require.Equal(t, requestID, rec.Header().Get("X-Request-Id"))
}

func TestRequestID_GeneratesWhenMissing(t *testing.T) {
	var capturedID string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.NotEmpty(t, capturedID)
	require.Equal(t, capturedID, rec.Header().Get("X-Request-Id"))
	_, err := uuid.Parse(capturedID)
	require.NoError(t, err)
}

func TestLogger_200Response(t *testing.T) {
	log, err := logger.New("test", logger.WithOutput(zapcore.AddSync(&bytes.Buffer{})))
	require.NoError(t, err)
	handler := Logger(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestLogger_404Response(t *testing.T) {
	log, err := logger.New("test", logger.WithOutput(zapcore.AddSync(&bytes.Buffer{})))
	require.NoError(t, err)
	handler := Logger(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	req := httptest.NewRequest("GET", "/missing", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestLogger_500Response(t *testing.T) {
	log, err := logger.New("test", logger.WithOutput(zapcore.AddSync(&bytes.Buffer{})))
	require.NoError(t, err)
	handler := Logger(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	req := httptest.NewRequest("GET", "/error", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestRecovery_PanicReturns500(t *testing.T) {
	log, err := logger.New("test", logger.WithOutput(zapcore.AddSync(&bytes.Buffer{})))
	require.NoError(t, err)
	handler := Recovery(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	}))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	var env envelope.Envelope
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&env))
	require.Equal(t, envelope.InternalError, env.ErrorCode)
}

func TestRecovery_NoPanic(t *testing.T) {
	log, err := logger.New("test", logger.WithOutput(zapcore.AddSync(&bytes.Buffer{})))
	require.NoError(t, err)
	handler := Recovery(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestContextHelpers_RoundTrip(t *testing.T) {
	ctx := context.Background()
	orgID := "a0000000-0000-0000-0000-000000000001"
	ctx = ContextWithOrgID(ctx, orgID)
	assert.Equal(t, orgID, OrgIDFromContext(ctx))

	requestID := "req-123"
	ctx = ContextWithRequestID(ctx, requestID)
	assert.Equal(t, requestID, RequestIDFromContext(ctx))
	assert.Equal(t, orgID, OrgIDFromContext(ctx))
}

func TestContextHelpers_NilContext(t *testing.T) {
	assert.Empty(t, OrgIDFromContext(nil))
	assert.Empty(t, RequestIDFromContext(nil))
}

func TestContextHelpers_EmptyContext(t *testing.T) {
	ctx := context.Background()
	assert.Empty(t, OrgIDFromContext(ctx))
	assert.Empty(t, RequestIDFromContext(ctx))
}
