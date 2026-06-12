package provider

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func withFastRetries(t *testing.T) {
	t.Helper()
	prevAttempts, prevBase, prevMax := retryMaxAttempts, retryBaseDelay, retryMaxDelay
	retryMaxAttempts = 4
	retryBaseDelay = 1 * time.Millisecond
	retryMaxDelay = 5 * time.Millisecond
	t.Cleanup(func() {
		retryMaxAttempts = prevAttempts
		retryBaseDelay = prevBase
		retryMaxDelay = prevMax
	})
}

func TestDoWithRetry_SucceedsOnFirstAttempt(t *testing.T) {
	withFastRetries(t)
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := doWithRetry(server.Client(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 call, got %d", got)
	}
}

func TestDoWithRetry_RetriesOn5xxThenSucceeds(t *testing.T) {
	withFastRetries(t)
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := doWithRetry(server.Client(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after retries, got %d", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 calls, got %d", got)
	}
}

func TestDoWithRetry_RetriesOn503ThenSucceeds(t *testing.T) {
	withFastRetries(t)
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := doWithRetry(server.Client(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after retries, got %d", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 calls, got %d", got)
	}
}

func TestDoWithRetry_ReturnsFinalResponseAfterMaxAttempts(t *testing.T) {
	withFastRetries(t)
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := doWithRetry(server.Client(), req)
	if err != nil {
		t.Fatalf("expected response (not error) after max attempts, got err: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); int(got) != retryMaxAttempts {
		t.Fatalf("expected %d calls, got %d", retryMaxAttempts, got)
	}
}

func TestDoWithRetry_DoesNotRetryOn4xx(t *testing.T) {
	withFastRetries(t)
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := doWithRetry(server.Client(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 call (no retry on 4xx), got %d", got)
	}
}

func TestDoWithRetry_RetriesOn429(t *testing.T) {
	withFastRetries(t)
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "GET", server.URL, nil)
	resp, err := doWithRetry(server.Client(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after retry, got %d", resp.StatusCode)
	}
}

func TestDoWithRetry_ReplaysBodyOnRetry(t *testing.T) {
	withFastRetries(t)
	var calls int32
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if n < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "POST", server.URL, bytes.NewReader([]byte("payload")))
	resp, err := doWithRetry(server.Client(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body.Close()
	if len(bodies) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(bodies))
	}
	for i, b := range bodies {
		if b != "payload" {
			t.Errorf("attempt %d: expected body 'payload', got %q", i, b)
		}
	}
}

func TestDoWithRetry_RespectsContextCancellation(t *testing.T) {
	withFastRetries(t)
	retryBaseDelay = 50 * time.Millisecond
	retryMaxDelay = 100 * time.Millisecond

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	_, err := doWithRetry(server.Client(), req)
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if got := atomic.LoadInt32(&calls); int(got) >= retryMaxAttempts {
		t.Fatalf("expected to stop before max attempts due to context cancellation, got %d calls", got)
	}
}

func TestIsRetryableStatus(t *testing.T) {
	cases := []struct {
		code     int
		expected bool
	}{
		{200, false},
		{301, false},
		{400, false},
		{401, false},
		{404, false},
		{408, true},
		{429, true},
		{500, true},
		{502, true},
		{503, true},
		{504, true},
		{599, true},
	}
	for _, tc := range cases {
		if got := isRetryableStatus(tc.code); got != tc.expected {
			t.Errorf("isRetryableStatus(%d): expected %v, got %v", tc.code, tc.expected, got)
		}
	}
}
