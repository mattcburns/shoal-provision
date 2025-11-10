package api_test

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

// API tests for POST /api/v1/jobs, GET /api/v1/jobs/{id}, and webhook transitions
// using an in-memory SQLite store.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"shoal/internal/provisioner/api"
	"shoal/internal/provisioner/store"
	"shoal/pkg/provisioner"
)

const maintISO = "http://controller.local/isos/bootc-maintenance.iso"

type createJobResp struct {
	JobID        string `json:"job_id"`
	Status       string `json:"status"`
	ServerSerial string `json:"server_serial"`
	CreatedAt    string `json:"created_at"`
}

type getJobResp struct {
	JobID        string                 `json:"job_id"`
	ServerSerial string                 `json:"server_serial"`
	Status       string                 `json:"status"`
	FailedStep   *string                `json:"failed_step"`
	CreatedAt    string                 `json:"created_at"`
	LastUpdate   string                 `json:"last_update"`
	Events       []map[string]any       `json:"events"`
	Extra        map[string]interface{} `json:"-"` // ignore unknowns
}

type jsonErr struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func newInMemoryStore(t *testing.T) *store.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	s, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open in-memory store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedServer(t *testing.T, s *store.Store, serial string) {
	t.Helper()
	ctx := context.Background()
	sv := provisioner.Server{
		Serial:     serial,
		BMCAddress: "https://bmc.example",
		BMCUser:    "root",
		BMCPass:    "pw",
		Vendor:     "acme",
	}
	if err := s.UpsertServer(ctx, sv); err != nil {
		t.Fatalf("seed server failed: %v", err)
	}
}

func newTestMux(t *testing.T, s *store.Store, webhookSecret string) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	ap := api.New(s, maintISO, nil)
	ap.Register(mux)
	wh := api.NewWebhookHandler(s, webhookSecret, "", nil, func() time.Time { return time.Now().UTC() })
	mux.Handle("/api/v1/status-webhook/", wh)
	return mux
}

func doJSON(t *testing.T, client *http.Client, method, url string, body any, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return resp, data
}

func TestCreateJobAndGet(t *testing.T) {
	s := newInMemoryStore(t)
	seedServer(t, s, "SER-1")
	mux := newTestMux(t, s, "")
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Create job
	reqBody := map[string]any{
		"server_serial": "SER-1",
		"recipe":        map[string]any{"task_target": "install-linux.target", "target_disk": "/dev/sda"},
	}
	resp, data := doJSON(t, srv.Client(), http.MethodPost, srv.URL+"/api/v1/jobs", reqBody, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", resp.StatusCode, string(data))
	}
	var cj createJobResp
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatalf("decode createJob resp: %v", err)
	}
	if cj.JobID == "" || cj.ServerSerial != "SER-1" || cj.Status != provisioner.JobStatusQueued.String() {
		t.Fatalf("unexpected createJob resp: %+v", cj)
	}

	// Get job
	resp2, data2 := doJSON(t, srv.Client(), http.MethodGet, srv.URL+"/api/v1/jobs/"+cj.JobID, nil, nil)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", resp2.StatusCode, string(data2))
	}
	var gj getJobResp
	if err := json.Unmarshal(data2, &gj); err != nil {
		t.Fatalf("decode getJob resp: %v", err)
	}
	if gj.JobID != cj.JobID || gj.ServerSerial != "SER-1" || gj.Status != provisioner.JobStatusQueued.String() {
		t.Fatalf("unexpected getJob resp: %+v", gj)
	}
	// Events should be empty at this stage (no worker ran)
	if len(gj.Events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(gj.Events))
	}
}

func TestCreateJob_InvalidRecipe(t *testing.T) {
	s := newInMemoryStore(t)
	seedServer(t, s, "SER-2")
	mux := newTestMux(t, s, "")
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// recipe=null invalid
	reqBody := map[string]any{
		"server_serial": "SER-2",
		"recipe":        nil,
	}
	resp, data := doJSON(t, srv.Client(), http.MethodPost, srv.URL+"/api/v1/jobs", reqBody, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, string(data))
	}
	var je jsonErr
	_ = json.Unmarshal(data, &je)
	if je.Error == "" || !strings.Contains(je.Error, "invalid") {
		t.Fatalf("expected invalid error, got: %+v", je)
	}

	// recipe is array (invalid per stub)
	reqBody["recipe"] = []any{1, 2, 3}
	resp2, data2 := doJSON(t, srv.Client(), http.MethodPost, srv.URL+"/api/v1/jobs", reqBody, nil)
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for array recipe, got %d: %s", resp2.StatusCode, string(data2))
	}
}

func TestGetJob_NotFound(t *testing.T) {
	s := newInMemoryStore(t)
	mux := newTestMux(t, s, "")
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, data := doJSON(t, srv.Client(), http.MethodGet, srv.URL+"/api/v1/jobs/does-not-exist", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", resp.StatusCode, string(data))
	}
}

func TestWebhook_AuthAndTransitions(t *testing.T) {
	s := newInMemoryStore(t)
	seedServer(t, s, "SER-WH")
	mux := newTestMux(t, s, "topsecret")
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := srv.Client()

	// Create job (queued)
	reqBody := map[string]any{
		"server_serial": "SER-WH",
		"recipe":        map[string]any{"task_target": "install-linux.target", "target_disk": "/dev/sda"},
	}
	resp, data := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/jobs", reqBody, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("create job expected 202, got %d: %s", resp.StatusCode, string(data))
	}
	var cj createJobResp
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatalf("decode createJob: %v", err)
	}

	// Transition to provisioning manually (simulate worker acquisition)
	if err := s.MarkJobStatus(context.Background(), cj.JobID, provisioner.JobStatusProvisioning, nil); err != nil {
		t.Fatalf("mark provisioning failed: %v", err)
	}

	// Unauthorized webhook (missing secret)
	resp2, data2 := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/status-webhook/SER-WH", map[string]any{"status": "success"}, nil)
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing secret, got %d: %s", resp2.StatusCode, string(data2))
	}

	// Authorized success webhook
	headers := map[string]string{"X-Webhook-Secret": "topsecret"}
	resp3, data3 := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/status-webhook/SER-WH", map[string]any{"status": "success"}, headers)
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for webhook success, got %d: %s", resp3.StatusCode, string(data3))
	}

	// Verify job now succeeded
	job, err := s.GetJobByID(context.Background(), cj.JobID)
	if err != nil {
		t.Fatalf("GetJobByID failed: %v", err)
	}
	if job.Status != provisioner.JobStatusSucceeded {
		t.Fatalf("expected job to be succeeded, got %s", job.Status)
	}

	// Create another job for failure path
	resp4, data4 := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/jobs", reqBody, nil)
	if resp4.StatusCode != http.StatusAccepted {
		t.Fatalf("create job2 expected 202, got %d: %s", resp4.StatusCode, string(data4))
	}
	var cj2 createJobResp
	if err := json.Unmarshal(data4, &cj2); err != nil {
		t.Fatalf("decode createJob2: %v", err)
	}
	if err := s.MarkJobStatus(context.Background(), cj2.JobID, provisioner.JobStatusProvisioning, nil); err != nil {
		t.Fatalf("mark provisioning job2 failed: %v", err)
	}

	// Authorized failed webhook with failed_step
	failStep := "bootloader-linux.service"
	resp5, data5 := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/status-webhook/SER-WH", map[string]any{
		"status":      "failed",
		"failed_step": failStep,
	}, headers)
	if resp5.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for webhook failed, got %d: %s", resp5.StatusCode, string(data5))
	}

	// Verify job2 failed with step
	job2, err := s.GetJobByID(context.Background(), cj2.JobID)
	if err != nil {
		t.Fatalf("GetJobByID job2 failed: %v", err)
	}
	if job2.Status != provisioner.JobStatusFailed {
		t.Fatalf("expected job2 to be failed, got %s", job2.Status)
	}
	if job2.FailedStep == nil || *job2.FailedStep != failStep {
		t.Fatalf("expected failed_step=%q, got %v", failStep, job2.FailedStep)
	}

	// Events appended by webhook should exist
	evs, err := s.ListJobEvents(context.Background(), cj2.JobID, 0)
	if err != nil {
		t.Fatalf("ListJobEvents failed: %v", err)
	}
	if len(evs) == 0 {
		t.Fatalf("expected some events appended by webhook, got 0")
	}
}

// TestWebhook_DeliveryIDDeduplication verifies that webhooks with duplicate delivery_id
// are handled idempotently: they return 200 OK without changing job state or appending
// duplicate success/failure events. Tests both success and failed scenarios.
func TestWebhook_DeliveryIDDeduplication(t *testing.T) {
	s := newInMemoryStore(t)
	seedServer(t, s, "SER-DUP")
	mux := newTestMux(t, s, "secret123")
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := srv.Client()
	headers := map[string]string{"X-Webhook-Secret": "secret123"}

	// === Test 1: Duplicate delivery_id for success transition ===
	// Create job
	reqBody := map[string]any{
		"server_serial": "SER-DUP",
		"recipe":        map[string]any{"task_target": "install-linux.target", "target_disk": "/dev/sda"},
	}
	resp, data := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/jobs", reqBody, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("create job expected 202, got %d: %s", resp.StatusCode, string(data))
	}
	var cj createJobResp
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatalf("decode createJob: %v", err)
	}

	// Mark provisioning
	if err := s.MarkJobStatus(context.Background(), cj.JobID, provisioner.JobStatusProvisioning, nil); err != nil {
		t.Fatalf("mark provisioning failed: %v", err)
	}

	// First success webhook with delivery_id
	deliveryID1 := "delivery-success-abc123"
	webhookPayload1 := map[string]any{
		"status":      "success",
		"delivery_id": deliveryID1,
	}
	resp1, data1 := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/status-webhook/SER-DUP", webhookPayload1, headers)
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first success webhook expected 200, got %d: %s", resp1.StatusCode, string(data1))
	}
	var result1 map[string]any
	if err := json.Unmarshal(data1, &result1); err != nil {
		t.Fatalf("decode first webhook response: %v", err)
	}
	if result1["ok"] != true {
		t.Fatalf("first webhook expected ok=true, got %v", result1)
	}
	if result1["idempotent"] != nil && result1["idempotent"] == true {
		t.Fatalf("first webhook should not be idempotent, got %v", result1)
	}

	// Verify job succeeded
	job, err := s.GetJobByID(context.Background(), cj.JobID)
	if err != nil {
		t.Fatalf("GetJobByID failed: %v", err)
	}
	if job.Status != provisioner.JobStatusSucceeded {
		t.Fatalf("expected job succeeded, got %s", job.Status)
	}

	// Manually revert job to provisioning to simulate duplicate delivery (test scenario)
	if err := s.MarkJobStatus(context.Background(), cj.JobID, provisioner.JobStatusProvisioning, nil); err != nil {
		t.Fatalf("revert to provisioning failed: %v", err)
	}

	// Count events before duplicate
	evsBefore, err := s.ListJobEvents(context.Background(), cj.JobID, 0)
	if err != nil {
		t.Fatalf("ListJobEvents failed: %v", err)
	}
	countBefore := len(evsBefore)

	// Send duplicate webhook with same delivery_id
	resp2, data2 := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/status-webhook/SER-DUP", webhookPayload1, headers)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("duplicate webhook expected 200, got %d: %s", resp2.StatusCode, string(data2))
	}
	var result2 map[string]any
	if err := json.Unmarshal(data2, &result2); err != nil {
		t.Fatalf("decode duplicate webhook response: %v", err)
	}
	if result2["ok"] != true {
		t.Fatalf("duplicate webhook expected ok=true, got %v", result2)
	}
	if result2["idempotent"] != true {
		t.Fatalf("duplicate webhook expected idempotent=true, got %v", result2)
	}

	// Verify job status unchanged (still provisioning, NOT transitioned to succeeded again)
	job2, err := s.GetJobByID(context.Background(), cj.JobID)
	if err != nil {
		t.Fatalf("GetJobByID after duplicate failed: %v", err)
	}
	if job2.Status != provisioner.JobStatusProvisioning {
		t.Fatalf("duplicate webhook should not change status; expected provisioning, got %s", job2.Status)
	}

	// Verify only one new event added (webhook-duplicate), not a second success event
	evsAfter, err := s.ListJobEvents(context.Background(), cj.JobID, 0)
	if err != nil {
		t.Fatalf("ListJobEvents after duplicate failed: %v", err)
	}
	countAfter := len(evsAfter)
	if countAfter != countBefore+1 {
		t.Fatalf("expected exactly 1 new event (webhook-duplicate), got %d events before, %d after", countBefore, countAfter)
	}

	// Verify the new event is webhook-duplicate
	lastEvent := evsAfter[countAfter-1]
	if lastEvent.Step == nil || *lastEvent.Step != "webhook-duplicate" {
		t.Fatalf("expected last event step=webhook-duplicate, got %v", lastEvent.Step)
	}
	if !strings.Contains(lastEvent.Message, deliveryID1) {
		t.Fatalf("expected duplicate event to contain delivery_id %s, got message: %s", deliveryID1, lastEvent.Message)
	}

	// === Test 2: Multiple duplicates and then different delivery_id ===
	// Send third request with same delivery_id - should still be idempotent
	resp3, data3 := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/status-webhook/SER-DUP", webhookPayload1, headers)
	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("third webhook expected 200, got %d: %s", resp3.StatusCode, string(data3))
	}
	var result3 map[string]any
	if err := json.Unmarshal(data3, &result3); err != nil {
		t.Fatalf("decode third webhook response: %v", err)
	}
	if result3["idempotent"] != true {
		t.Fatalf("third webhook expected idempotent=true, got %v", result3)
	}

	// Verify different delivery_id is processed normally
	webhookPayload2 := map[string]any{
		"status":      "success",
		"delivery_id": "delivery-success-xyz789",
	}
	resp4, data4 := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/status-webhook/SER-DUP", webhookPayload2, headers)
	if resp4.StatusCode != http.StatusOK {
		t.Fatalf("new delivery_id webhook expected 200, got %d: %s", resp4.StatusCode, string(data4))
	}
	var result4 map[string]any
	if err := json.Unmarshal(data4, &result4); err != nil {
		t.Fatalf("decode new delivery_id response: %v", err)
	}
	if result4["idempotent"] == true {
		t.Fatalf("new delivery_id should not be idempotent, got %v", result4)
	}

	// Verify new delivery_id caused transition to succeeded
	job3, err := s.GetJobByID(context.Background(), cj.JobID)
	if err != nil {
		t.Fatalf("GetJobByID after new delivery_id failed: %v", err)
	}
	if job3.Status != provisioner.JobStatusSucceeded {
		t.Fatalf("new delivery_id should transition to succeeded, got %s", job3.Status)
	}

	// === Test 3: Deduplication for failed webhooks ===
	// Create second job for failed path
	resp5, data5 := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/jobs", reqBody, nil)
	if resp5.StatusCode != http.StatusAccepted {
		t.Fatalf("create job2 expected 202, got %d: %s", resp5.StatusCode, string(data5))
	}
	var cj2 createJobResp
	if err := json.Unmarshal(data5, &cj2); err != nil {
		t.Fatalf("decode createJob2: %v", err)
	}
	if err := s.MarkJobStatus(context.Background(), cj2.JobID, provisioner.JobStatusProvisioning, nil); err != nil {
		t.Fatalf("mark provisioning job2 failed: %v", err)
	}

	// First failed webhook with delivery_id
	deliveryID2 := "delivery-failed-123"
	failedStep := "bootloader-linux.service"
	webhookPayload3 := map[string]any{
		"status":      "failed",
		"failed_step": failedStep,
		"delivery_id": deliveryID2,
	}
	resp6, data6 := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/status-webhook/SER-DUP", webhookPayload3, headers)
	if resp6.StatusCode != http.StatusOK {
		t.Fatalf("first failed webhook expected 200, got %d: %s", resp6.StatusCode, string(data6))
	}

	// Verify failed transition
	job4, err := s.GetJobByID(context.Background(), cj2.JobID)
	if err != nil {
		t.Fatalf("GetJobByID job2 failed: %v", err)
	}
	if job4.Status != provisioner.JobStatusFailed {
		t.Fatalf("expected job2 failed, got %s", job4.Status)
	}

	// Revert to provisioning for duplicate test
	if err := s.MarkJobStatus(context.Background(), cj2.JobID, provisioner.JobStatusProvisioning, nil); err != nil {
		t.Fatalf("revert job2 to provisioning failed: %v", err)
	}

	// Send duplicate failed webhook
	resp7, data7 := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/status-webhook/SER-DUP", webhookPayload3, headers)
	if resp7.StatusCode != http.StatusOK {
		t.Fatalf("duplicate failed webhook expected 200, got %d: %s", resp7.StatusCode, string(data7))
	}
	var result7 map[string]any
	if err := json.Unmarshal(data7, &result7); err != nil {
		t.Fatalf("decode duplicate failed response: %v", err)
	}
	if result7["idempotent"] != true {
		t.Fatalf("duplicate failed webhook expected idempotent=true, got %v", result7)
	}

	// Verify status unchanged
	job5, err := s.GetJobByID(context.Background(), cj2.JobID)
	if err != nil {
		t.Fatalf("GetJobByID after duplicate failed: %v", err)
	}
	if job5.Status != provisioner.JobStatusProvisioning {
		t.Fatalf("duplicate failed webhook should not transition, expected provisioning, got %s", job5.Status)
	}
}
