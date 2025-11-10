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

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"shoal/pkg/provisioner"
)

// mockWebhookStore implements WebhookStore for testing
type mockWebhookStore struct {
	jobs       map[string]*provisioner.Job
	jobStatus  map[string]provisioner.JobStatus
	failedStep map[string]*string
	events     []provisioner.JobEvent
}

func newMockWebhookStore() *mockWebhookStore {
	return &mockWebhookStore{
		jobs:       make(map[string]*provisioner.Job),
		jobStatus:  make(map[string]provisioner.JobStatus),
		failedStep: make(map[string]*string),
		events:     make([]provisioner.JobEvent, 0),
	}
}

func (m *mockWebhookStore) GetActiveProvisioningJobBySerial(ctx context.Context, serial string) (*provisioner.Job, error) {
	for _, job := range m.jobs {
		if job.ServerSerial == serial && m.jobStatus[job.ID] == provisioner.JobStatusProvisioning {
			return job, nil
		}
	}
	return nil, nil
}

func (m *mockWebhookStore) MarkJobStatus(ctx context.Context, id string, status provisioner.JobStatus, failedStep *string) error {
	m.jobStatus[id] = status
	if failedStep != nil {
		m.failedStep[id] = failedStep
	}
	return nil
}

func (m *mockWebhookStore) AppendJobEvent(ctx context.Context, ev provisioner.JobEvent) error {
	m.events = append(m.events, ev)
	return nil
}

func TestWebhookHandler_CurrentSecretAccepted(t *testing.T) {
	store := newMockWebhookStore()
	job := &provisioner.Job{
		ID:           "job-123",
		ServerSerial: "SN12345",
	}
	store.jobs[job.ID] = job
	store.jobStatus[job.ID] = provisioner.JobStatusProvisioning

	handler := NewWebhookHandler(store, "current-secret", "", nil, nil)

	body := WebhookRequest{Status: "success"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/status-webhook/SN12345", bytes.NewReader(bodyBytes))
	req.Header.Set("X-Webhook-Secret", "current-secret")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	if store.jobStatus[job.ID] != provisioner.JobStatusSucceeded {
		t.Errorf("expected job status Succeeded, got %v", store.jobStatus[job.ID])
	}
}

func TestWebhookHandler_OldSecretAccepted(t *testing.T) {
	store := newMockWebhookStore()
	job := &provisioner.Job{
		ID:           "job-456",
		ServerSerial: "SN67890",
	}
	store.jobs[job.ID] = job
	store.jobStatus[job.ID] = provisioner.JobStatusProvisioning

	handler := NewWebhookHandler(store, "new-secret", "old-secret", nil, nil)

	body := WebhookRequest{Status: "success"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/status-webhook/SN67890", bytes.NewReader(bodyBytes))
	req.Header.Set("X-Webhook-Secret", "old-secret")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	if store.jobStatus[job.ID] != provisioner.JobStatusSucceeded {
		t.Errorf("expected job status Succeeded, got %v", store.jobStatus[job.ID])
	}
}

func TestWebhookHandler_InvalidSecretRejected(t *testing.T) {
	store := newMockWebhookStore()
	job := &provisioner.Job{
		ID:           "job-789",
		ServerSerial: "SN11111",
	}
	store.jobs[job.ID] = job
	store.jobStatus[job.ID] = provisioner.JobStatusProvisioning

	handler := NewWebhookHandler(store, "new-secret", "old-secret", nil, nil)

	body := WebhookRequest{Status: "success"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/status-webhook/SN11111", bytes.NewReader(bodyBytes))
	req.Header.Set("X-Webhook-Secret", "wrong-secret")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}

	// Job status should not change
	if store.jobStatus[job.ID] != provisioner.JobStatusProvisioning {
		t.Errorf("job status should not change, got %v", store.jobStatus[job.ID])
	}
}

func TestWebhookHandler_MissingSecretRejected(t *testing.T) {
	store := newMockWebhookStore()
	job := &provisioner.Job{
		ID:           "job-999",
		ServerSerial: "SN22222",
	}
	store.jobs[job.ID] = job
	store.jobStatus[job.ID] = provisioner.JobStatusProvisioning

	handler := NewWebhookHandler(store, "required-secret", "", nil, nil)

	body := WebhookRequest{Status: "success"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/status-webhook/SN22222", bytes.NewReader(bodyBytes))
	// No X-Webhook-Secret header
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestWebhookHandler_NoAuthWhenBothSecretsEmpty(t *testing.T) {
	store := newMockWebhookStore()
	job := &provisioner.Job{
		ID:           "job-noauth",
		ServerSerial: "SN33333",
	}
	store.jobs[job.ID] = job
	store.jobStatus[job.ID] = provisioner.JobStatusProvisioning

	handler := NewWebhookHandler(store, "", "", nil, nil)

	body := WebhookRequest{Status: "success"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/status-webhook/SN33333", bytes.NewReader(bodyBytes))
	// No secret header required
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestWebhookHandler_SuccessTransition(t *testing.T) {
	store := newMockWebhookStore()
	job := &provisioner.Job{
		ID:           "job-success",
		ServerSerial: "SN-SUCCESS",
	}
	store.jobs[job.ID] = job
	store.jobStatus[job.ID] = provisioner.JobStatusProvisioning

	handler := NewWebhookHandler(store, "", "", nil, func() time.Time {
		return time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	})

	body := WebhookRequest{Status: "success"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/status-webhook/SN-SUCCESS", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	if store.jobStatus[job.ID] != provisioner.JobStatusSucceeded {
		t.Errorf("expected job status Succeeded, got %v", store.jobStatus[job.ID])
	}

	// Check event logged
	if len(store.events) == 0 {
		t.Fatal("expected at least one event")
	}
	lastEvent := store.events[len(store.events)-1]
	if lastEvent.Level != provisioner.EventLevelInfo {
		t.Errorf("expected Info event, got %v", lastEvent.Level)
	}
}

func TestWebhookHandler_FailureTransition(t *testing.T) {
	store := newMockWebhookStore()
	job := &provisioner.Job{
		ID:           "job-fail",
		ServerSerial: "SN-FAIL",
	}
	store.jobs[job.ID] = job
	store.jobStatus[job.ID] = provisioner.JobStatusProvisioning

	handler := NewWebhookHandler(store, "", "", nil, nil)

	failedStep := "bootloader-linux.service"
	body := WebhookRequest{
		Status:     "failed",
		FailedStep: &failedStep,
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/status-webhook/SN-FAIL", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	if store.jobStatus[job.ID] != provisioner.JobStatusFailed {
		t.Errorf("expected job status Failed, got %v", store.jobStatus[job.ID])
	}

	if store.failedStep[job.ID] == nil || *store.failedStep[job.ID] != "bootloader-linux.service" {
		t.Errorf("failed_step not recorded correctly")
	}

	// Check error event logged
	if len(store.events) == 0 {
		t.Fatal("expected at least one event")
	}
	lastEvent := store.events[len(store.events)-1]
	if lastEvent.Level != provisioner.EventLevelError {
		t.Errorf("expected Error event, got %v", lastEvent.Level)
	}
}

func TestWebhookHandler_IdempotentDelivery(t *testing.T) {
	store := newMockWebhookStore()
	job := &provisioner.Job{
		ID:           "job-idem",
		ServerSerial: "SN-IDEM",
	}
	store.jobs[job.ID] = job
	store.jobStatus[job.ID] = provisioner.JobStatusProvisioning

	handler := NewWebhookHandler(store, "", "", nil, nil)

	deliveryID := "delivery-12345"
	body := WebhookRequest{
		Status:     "success",
		DeliveryID: deliveryID,
	}
	bodyBytes, _ := json.Marshal(body)

	// First request
	req1 := httptest.NewRequest("POST", "/api/v1/status-webhook/SN-IDEM", bytes.NewReader(bodyBytes))
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("first request: expected status 200, got %d", w1.Code)
	}

	if store.jobStatus[job.ID] != provisioner.JobStatusSucceeded {
		t.Errorf("first request should mark job succeeded")
	}

	// Reset job to provisioning for idempotency test
	// (In reality, the dispatcher would retry before job transitions)
	store.jobStatus[job.ID] = provisioner.JobStatusProvisioning

	// Second request with same delivery_id (need new reader)
	req2 := httptest.NewRequest("POST", "/api/v1/status-webhook/SN-IDEM", bytes.NewReader(bodyBytes))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("second request: expected status 200, got %d", w2.Code)
	}

	// Check response indicates idempotency
	var resp map[string]any
	if err := json.NewDecoder(w2.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if idempotent, ok := resp["idempotent"].(bool); !ok || !idempotent {
		t.Error("second request should return idempotent:true")
	}

	// Job should NOT transition again
	if store.jobStatus[job.ID] != provisioner.JobStatusProvisioning {
		t.Error("idempotent request should not change job status")
	}
}

func TestWebhookHandler_NoJobFound(t *testing.T) {
	store := newMockWebhookStore()
	handler := NewWebhookHandler(store, "", "", nil, nil)

	body := WebhookRequest{Status: "success"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/status-webhook/SN-NOTFOUND", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestWebhookHandler_InvalidStatusRejected(t *testing.T) {
	store := newMockWebhookStore()
	job := &provisioner.Job{
		ID:           "job-invalid",
		ServerSerial: "SN-INVALID",
	}
	store.jobs[job.ID] = job
	store.jobStatus[job.ID] = provisioner.JobStatusProvisioning

	handler := NewWebhookHandler(store, "", "", nil, nil)

	body := WebhookRequest{Status: "invalid-status"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/v1/status-webhook/SN-INVALID", bytes.NewReader(bodyBytes))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	// Job status should not change
	if store.jobStatus[job.ID] != provisioner.JobStatusProvisioning {
		t.Errorf("job status should not change on invalid request")
	}
}
