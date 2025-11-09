// Shoal is a Redfish aggregator service.
// Copyright (C) 2025  Matthew Burns
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package bmc

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"time"

	"shoal/internal/ctxkeys"
	pm "shoal/internal/provisioner/metrics"
)

// Default retry configuration values for critical Redfish operations.
const (
	defaultMaxAttempts = 4
	defaultBaseDelay   = 500 * time.Millisecond
	defaultMaxDelay    = 3 * time.Second
	defaultJitterFrac  = 0.3
)

// retryConfig defines retry/backoff parameters for Redfish calls.
type retryConfig struct {
	maxAttempts int
	baseDelay   time.Duration
	maxDelay    time.Duration
	jitterFrac  float64 // 0.0-1.0 fraction of delay to jitter
	opLabel     string  // metrics/logging operation label
	vendor      string  // optional vendor label for metrics
}

// newDefaultRetryConfig creates a retry config with default values for critical operations.
func newDefaultRetryConfig(opLabel, vendor string) retryConfig {
	return retryConfig{
		maxAttempts: defaultMaxAttempts,
		baseDelay:   defaultBaseDelay,
		maxDelay:    defaultMaxDelay,
		jitterFrac:  defaultJitterFrac,
		opLabel:     opLabel,
		vendor:      vendor,
	}
}

// doWithRetry executes fn with retry/backoff on transient failures.
// It returns the last response (caller is responsible to close body) and error.
func (s *Service) doWithRetry(ctx context.Context, cfg retryConfig, fn func(context.Context) (*http.Response, error)) (*http.Response, error) {
	if cfg.maxAttempts <= 0 {
		cfg.maxAttempts = 3
	}
	if cfg.baseDelay <= 0 {
		cfg.baseDelay = 300 * time.Millisecond
	}
	if cfg.maxDelay <= 0 {
		cfg.maxDelay = 5 * time.Second
	}
	if cfg.jitterFrac <= 0 {
		cfg.jitterFrac = 0.25
	}

	var attempt int
	var lastErr error
	var lastResp *http.Response
	for attempt = 1; attempt <= cfg.maxAttempts; attempt++ {
		start := time.Now()
		resp, err := fn(ctx)
		dur := time.Since(start)

		code := -1
		if resp != nil {
			code = resp.StatusCode
		}

		// Record metrics for this attempt
		pm.ObserveRedfishRequest(cfg.opLabel, cfg.vendor, code, dur)

		// Success path
		if err == nil && resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}

		// Decide if retryable
		if !isRetryable(err, resp) {
			// Not retryable - return immediately without closing body (caller handles it)
			if err != nil {
				return nil, err
			}
			return resp, nil
		}

		// Close response body on retryable failure to avoid leaks before retry
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}

		lastErr = err
		lastResp = resp

		if attempt < cfg.maxAttempts {
			// Backoff with jitter
			exp := attempt - 1
			if exp > 10 {
				exp = 10 // cap exponent to prevent overflow
			}
			backoff := cfg.baseDelay * (1 << exp)
			if backoff > cfg.maxDelay {
				backoff = cfg.maxDelay
			}
			jitter := time.Duration(rand.Float64() * cfg.jitterFrac * float64(backoff) * 2)
			sleep := backoff - time.Duration(cfg.jitterFrac*float64(backoff)) + jitter // +/- around base
			pm.IncRedfishRetry(cfg.opLabel, cfg.vendor)
			cid := ctxkeys.GetCorrelationID(ctx)
			if cid != "" {
				slog.Debug("redfish retry", "op", cfg.opLabel, "attempt", attempt, "sleep", sleep, "vendor", cfg.vendor, "err", errString(err), "statusCode", code, "correlation_id", cid)
			} else {
				slog.Debug("redfish retry", "op", cfg.opLabel, "attempt", attempt, "sleep", sleep, "vendor", cfg.vendor, "err", errString(err), "statusCode", code)
			}

			timer := time.NewTimer(sleep)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
				// continue
			}
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return lastResp, errors.New("redfish request failed after retries")
}

// isRetryable determines if the error/response suggests a transient failure.
func isRetryable(err error, resp *http.Response) bool {
	if err != nil {
		// Timeouts and temporary net errors are retryable
		var nerr net.Error
		if errors.As(err, &nerr) {
			if nerr.Timeout() {
				return true
			}
		}
		// Generic connection resets etc.
		return true
	}
	if resp == nil {
		return true
	}
	// Retry on 5xx and 429
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if resp.StatusCode >= 500 && resp.StatusCode <= 599 {
		return true
	}
	return false
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
