package api

// Shoal is a Redfish aggregator service.
// Copyright (C) 2025 Matthew Burns
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

// Webhook handler for:
//   POST /api/v1/status-webhook/{serial}
//
// Implements secret-based auth, finds the active provisioning job for the
// server serial, transitions job status to succeeded|failed, persists the
// failed_step (when provided), and appends events.
//
// This file provides a standalone constructor that returns an http.HandlerFunc
// so it can be wired from the application without changing the API struct.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"shoal/internal/provisioner/metrics"
	"shoal/pkg/provisioner"
)

// WebhookStore is the persistence surface required by the webhook handler.
type WebhookStore interface {
	// Find active job for the server currently in provisioning.
	GetActiveProvisioningJobBySerial(ctx context.Context, serial string) (*provisioner.Job, error)
	// MarkJobStatus updates the job status and optionally records a failed step.
	MarkJobStatus(ctx context.Context, id string, status provisioner.JobStatus, failedStep *string) error
	// AppendJobEvent appends an event entry for the job.
	AppendJobEvent(ctx context.Context, ev provisioner.JobEvent) error
}

// WebhookRequest is the payload for the webhook POST.
type WebhookRequest struct {
	Status      string  `json:"status"`                // "success" or "failed"
	FailedStep  *string `json:"failed_step,omitempty"` // e.g., "bootloader-linux.service"
	DeliveryID  string  `json:"delivery_id,omitempty"` // unique ID for deduplication
	TaskTarget  string  `json:"task_target,omitempty"` // e.g., "install-linux.target"
	Started     string  `json:"started_at,omitempty"`  // RFC3339
	Finished    string  `json:"finished_at,omitempty"` // RFC3339
	DispatcherV string  `json:"dispatcher_version,omitempty"`
	SchemaID    string  `json:"schema_id,omitempty"`
}

// deliveryCache provides simple LRU-style deduplication of delivery IDs per job.
// Thread-safe. Max 32 delivery_ids kept per job (design doc guidance).
type deliveryCache struct {
	mu    sync.RWMutex
	cache map[string][]string // jobID -> delivery_id list (LRU-like)
	max   int
}

func newDeliveryCache(maxPerJob int) *deliveryCache {
	if maxPerJob <= 0 {
		maxPerJob = 32
	}
	return &deliveryCache{
		cache: make(map[string][]string),
		max:   maxPerJob,
	}
}

func (dc *deliveryCache) seen(jobID, deliveryID string) bool {
	if deliveryID == "" {
		return false
	}
	dc.mu.RLock()
	list, ok := dc.cache[jobID]
	dc.mu.RUnlock()
	if !ok {
		return false
	}
	for _, id := range list {
		if id == deliveryID {
			return true
		}
	}
	return false
}

func (dc *deliveryCache) record(jobID, deliveryID string) {
	if deliveryID == "" {
		return
	}
	dc.mu.Lock()
	defer dc.mu.Unlock()
	list := dc.cache[jobID]
	// Check if already present (idempotent)
	for _, id := range list {
		if id == deliveryID {
			return
		}
	}
	// Prepend (most recent first)
	list = append([]string{deliveryID}, list...)
	if len(list) > dc.max {
		list = list[:dc.max]
	}
	dc.cache[jobID] = list
}

// NewWebhookHandler builds an http.HandlerFunc that processes webhook calls.
// If secret is non-empty, requests must include header "X-Webhook-Secret" with a
// matching value. If secret is empty, authentication is disabled.
func NewWebhookHandler(store WebhookStore, secret string, logger *log.Logger, now func() time.Time) http.HandlerFunc {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	cache := newDeliveryCache(32) // 32 delivery_ids per job
	redact := func(s string) string {
		if s == "" {
			return ""
		}
		if len(s) <= 4 {
			return "****"
		}
		return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
	}
	logf := func(format string, args ...any) {
		if logger != nil {
			logger.Printf("[webhook] "+format, args...)
		}
	}

	writeJSON := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	type jsonError struct {
		Error   string `json:"error"`
		Message string `json:"message,omitempty"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Method check
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}

		// Path: /api/v1/status-webhook/{serial}
		serial := strings.TrimPrefix(r.URL.Path, "/api/v1/status-webhook/")
		if serial == "" || strings.Contains(serial, "/") {
			http.NotFound(w, r)
			return
		}

		// Auth check (shared secret)
		if secret != "" {
			got := r.Header.Get("X-Webhook-Secret")
			if got == "" || got != secret {
				logf("unauthorized webhook for serial=%s header=%s expected=%s", serial, redact(got), redact(secret))
				writeJSON(w, http.StatusUnauthorized, jsonError{
					Error:   "unauthorized",
					Message: "invalid or missing webhook secret",
				})
				return
			}
		}

		// Decode body
		var req WebhookRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, jsonError{
				Error:   "invalid_json",
				Message: "request body must be valid JSON",
			})
			return
		}
		st := strings.ToLower(strings.TrimSpace(req.Status))
		if st != "success" && st != "failed" {
			writeJSON(w, http.StatusBadRequest, jsonError{
				Error:   "invalid_request",
				Message: `status must be "success" or "failed"`,
			})
			return
		}

		ctx := r.Context()

		// Find the active provisioning job for this server
		job, err := store.GetActiveProvisioningJobBySerial(ctx, serial)
		if err != nil {
			// Keep 404 generic (no job)
			writeJSON(w, http.StatusNotFound, jsonError{
				Error:   "not_found",
				Message: "no active job for this server",
			})
			return
		}

		// Deduplication: check if delivery_id was seen before
		if req.DeliveryID != "" && cache.seen(job.ID, req.DeliveryID) {
			logf("idempotent duplicate delivery_id=%s for job=%s serial=%s; returning 200 OK", req.DeliveryID, job.ID, serial)
			// Append idempotent event
			_ = store.AppendJobEvent(ctx, provisioner.JobEvent{
				JobID:   job.ID,
				Time:    now(),
				Level:   provisioner.EventLevelInfo,
				Message: fmt.Sprintf("Idempotent webhook delivery (delivery_id=%s)", req.DeliveryID),
				Step:    strPtr("webhook-duplicate"),
			})
			metrics.ObserveWebhookRequest("duplicate", time.Since(start))
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "idempotent": true})
			return
		}

		// Record delivery_id for future deduplication
		if req.DeliveryID != "" {
			cache.record(job.ID, req.DeliveryID)
		}

		// Transition and append events
		switch st {
		case "success":
			if err := store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusSucceeded, nil); err != nil {
				logf("job=%s serial=%s mark succeeded failed: %v", job.ID, serial, err)
				writeJSON(w, http.StatusInternalServerError, jsonError{
					Error:   "server_error",
					Message: "failed to update job",
				})
				return
			}
			msg := "Webhook reported success"
			if req.DeliveryID != "" {
				msg = fmt.Sprintf("%s (delivery_id=%s)", msg, req.DeliveryID)
			}
			_ = store.AppendJobEvent(ctx, provisioner.JobEvent{
				JobID:   job.ID,
				Time:    now(),
				Level:   provisioner.EventLevelInfo,
				Message: msg,
				Step:    strPtr("webhook-success"),
			})
			metrics.ObserveWebhookRequest("success", time.Since(start))
		case "failed":
			failedStep := req.FailedStep
			if failedStep == nil || strings.TrimSpace(*failedStep) == "" {
				unknown := "unknown"
				failedStep = &unknown
			}
			if err := store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, failedStep); err != nil {
				logf("job=%s serial=%s mark failed failed: %v", job.ID, serial, err)
				writeJSON(w, http.StatusInternalServerError, jsonError{
					Error:   "server_error",
					Message: "failed to update job",
				})
				return
			}
			msg := "Webhook reported failure"
			if failedStep != nil {
				msg = fmt.Sprintf("%s at step %s", msg, *failedStep)
			}
			if req.DeliveryID != "" {
				msg = fmt.Sprintf("%s (delivery_id=%s)", msg, req.DeliveryID)
			}
			_ = store.AppendJobEvent(ctx, provisioner.JobEvent{
				JobID:   job.ID,
				Time:    now(),
				Level:   provisioner.EventLevelError,
				Message: msg,
				Step:    failedStep,
			})
			metrics.ObserveWebhookRequest("failed", time.Since(start))
		default:
			// Should not happen due to earlier validation
			writeJSON(w, http.StatusBadRequest, jsonError{
				Error:   "invalid_request",
				Message: "unsupported status",
			})
			return
		}

		// Success, let workers observe the transition and perform cleanup.
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func strPtr(s string) *string { return &s }

// isNotFound provides a generic way to treat not found errors without coupling
// to a specific store implementation's sentinel. Currently unused here but kept
// for parity with other handlers.
