// Package health provides real HTTP health probing for deployed applications.
package health

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// HTTPProber checks if a deployed app is accessible via HTTP GET.
// It retries up to 3 times with exponential backoff (2s, 4s, 8s).
// Status codes 200-399 are considered healthy.
type HTTPProber struct {
	Client *http.Client
}

// Probe sends HTTP GET to url+path and returns nil if the response is 200-399.
// Retries up to 3 times with backoff. Respects context cancellation.
func (p *HTTPProber) Probe(ctx context.Context, url string, path string) error {
	endpoint := url + path
	backoffs := []time.Duration{0, 2 * time.Second, 4 * time.Second}
	var lastErr error

	for attempt, wait := range backoffs {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("health probe cancelled: %w", ctx.Err())
			case <-time.After(wait):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return fmt.Errorf("health probe: invalid request: %w", err)
		}

		resp, err := p.Client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("health probe attempt %d: %w", attempt+1, err)
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			return nil
		}
		lastErr = fmt.Errorf("health probe attempt %d: status %d", attempt+1, resp.StatusCode)
	}

	return fmt.Errorf("health probe failed after 3 attempts: %w", lastErr)
}
