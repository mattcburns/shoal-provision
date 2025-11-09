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
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestDoWithRetry_SucceedsAfterTransientErrors(t *testing.T) {
	s := &Service{}
	attempts := 0
	fn := func(ctx context.Context) (*http.Response, error) {
		attempts++
		if attempts < 3 {
			// Simulate 500
			return &http.Response{StatusCode: 500, Body: io.NopCloser(bytes.NewReader(nil))}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	}
	resp, err := s.doWithRetry(context.Background(), retryConfig{maxAttempts: 5, baseDelay: 10 * time.Millisecond, maxDelay: 20 * time.Millisecond, opLabel: "test"}, fn)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if resp == nil || resp.StatusCode != 200 {
		t.Fatalf("expected 200 response, got %#v", resp)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}
