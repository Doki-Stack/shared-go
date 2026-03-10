package httpclient

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/doki-stack/shared-go/breaker"
	"github.com/doki-stack/shared-go/middleware"
	"github.com/sony/gobreaker/v2"
)

func TestClient_SuccessNoRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	client := New()
	resp, err := client.Get(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestClient_RetriesOn503(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(
		WithRetries(5),
		WithBackoff(func(int) time.Duration { return 1 * time.Millisecond }),
	)
	resp, err := client.Get(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestClient_NoRetryOn400(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := New(WithRetries(3))
	resp, err := client.Get(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestClient_OrgIDPropagation(t *testing.T) {
	var capturedOrgID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedOrgID = r.Header.Get("X-Org-Id")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New()
	ctx := middleware.ContextWithOrgID(context.Background(), "org-123")
	resp, err := client.Get(ctx, server.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if capturedOrgID != "org-123" {
		t.Errorf("X-Org-Id = %q, want %q", capturedOrgID, "org-123")
	}
}

func TestClient_ResponseSizeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 1025)) // 1KB + 1 byte
	}))
	defer server.Close()

	client := New(WithMaxResponseSize(1024))
	resp, err := client.Get(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	_, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err == nil {
		t.Fatal("expected error when response exceeds max size")
	}
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Errorf("err = %v, want ErrResponseTooLarge", err)
	}
}

func TestClient_CircuitBreakerOpen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cb := breaker.New("test", breaker.WithReadyToTrip(func(c gobreaker.Counts) bool {
		return c.ConsecutiveFailures >= 1
	}))

	// Trip the breaker
	_, _ = cb.Execute(func() (interface{}, error) {
		return nil, errors.New("fail")
	})

	if cb.State() != breaker.StateOpen {
		t.Fatalf("breaker state = %v, want open", cb.State())
	}

	client := New(WithCircuitBreaker(cb))
	_, err := client.Get(context.Background(), server.URL)
	if err == nil {
		t.Fatal("expected error when circuit breaker is open")
	}
	if !errors.Is(err, gobreaker.ErrOpenState) {
		t.Errorf("err = %v, want ErrOpenState", err)
	}
}

func TestClient_Post(t *testing.T) {
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New()
	body := []byte("hello")
	resp, err := client.Post(context.Background(), server.URL, nil)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if len(capturedBody) != 0 {
		t.Errorf("body = %q, want empty", capturedBody)
	}

	resp2, err := client.Post(context.Background(), server.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer resp2.Body.Close()
	if string(capturedBody) != string(body) {
		t.Errorf("body = %q, want %q", capturedBody, body)
	}
}

func TestClient_Put(t *testing.T) {
	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New()
	body := []byte("put-data")
	resp, err := client.Put(context.Background(), server.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	defer resp.Body.Close()
	if string(capturedBody) != string(body) {
		t.Errorf("body = %q, want %q", capturedBody, body)
	}
}

func TestClient_Delete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New()
	resp, err := client.Delete(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}
