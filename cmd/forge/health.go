package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

type healthChecker interface {
	Wait(ctx context.Context, url string) error
}

type httpHealthChecker struct {
	client   *http.Client
	interval time.Duration
}

func newHTTPHealthChecker() httpHealthChecker {
	return httpHealthChecker{
		client: &http.Client{
			Timeout: 2 * time.Second,
		},
		interval: 500 * time.Millisecond,
	}
}

func (h httpHealthChecker) Wait(ctx context.Context, url string) error {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	var lastErr error
	for {
		lastErr = h.check(ctx, url)
		if lastErr == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf(
				"wait for %s: last check failed: %v: %w",
				url,
				lastErr,
				ctx.Err(),
			)
		case <-ticker.C:
		}
	}
}

func (h httpHealthChecker) check(ctx context.Context, url string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}

	response, err := h.client.Do(request)
	if err != nil {
		return fmt.Errorf("perform health request: %w", err)
	}
	defer response.Body.Close()

	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned %s", response.Status)
	}
	return nil
}
