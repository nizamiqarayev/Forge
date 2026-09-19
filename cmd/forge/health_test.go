package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPHealthCheckerWait(t *testing.T) {
	t.Run("healthy immediately", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		checker := httpHealthChecker{client: server.Client(), interval: time.Millisecond}
		if err := checker.Wait(context.Background(), server.URL); err != nil {
			t.Fatalf("Wait() error = %v", err)
		}
	})

	t.Run("retries until healthy", func(t *testing.T) {
		var attempts atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if attempts.Add(1) < 3 {
				http.Error(w, "starting", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		checker := httpHealthChecker{client: server.Client(), interval: time.Millisecond}
		if err := checker.Wait(context.Background(), server.URL); err != nil {
			t.Fatalf("Wait() error = %v", err)
		}
		if got := attempts.Load(); got != 3 {
			t.Errorf("attempts = %d, want 3", got)
		}
	})

	t.Run("stops at deadline", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unhealthy", http.StatusServiceUnavailable)
		}))
		defer server.Close()

		checker := httpHealthChecker{client: server.Client(), interval: time.Millisecond}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		err := checker.Wait(ctx, server.URL)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Wait() error = %v, want context deadline exceeded", err)
		}
	})
}
