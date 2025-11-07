package integration

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

// End-to-end integration test exercising Phase 1 flow:
//   POST /jobs (queued) → worker builds task ISO and orchestrates Redfish (noop)
//   → webhook "success" → cleanup → job marked complete.
// This test runs an in-memory store, HTTP API + webhook on httptest.Server,
// and a real worker goroutine with an ISO stub and Redfish noop client.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shoal/internal/provisioner/api"
	"shoal/internal/provisioner/iso"
	"shoal/internal/provisioner/jobs"
	"shoal/internal/provisioner/redfish"
	"shoal/internal/provisioner/store"
	"shoal/pkg/provisioner"
)

func TestIntegration_EndToEndPhase1Success(t *testing.T) {
	// Store (in-memory)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	// Seed a server mapping
	serial := "SER-INTEG-1"
	err = st.UpsertServer(ctx, provisioner.Server{
		Serial:     serial,
		BMCAddress: "https://bmc.example", // valid URL for Redfish noop validation
		BMCUser:    "root",
		BMCPass:    "pw",
		Vendor:     "acme",
	})
	if err != nil {
		t.Fatalf("seed server: %v", err)
	}

	// ISO builder root (task ISO output)
	taskRoot := t.TempDir()
	builder := iso.NewFileBuilder(taskRoot)

	// HTTP mux with API, webhook, and media serving
	mux := http.NewServeMux()
	// API with placeholder MaintenanceISOURL (we will set correct value after we know server URL)
	ap := api.New(st, "http://localhost/isos/bootc-maintenance.iso", nil)
	ap.Register(mux)

	webhookSecret := "testsecret"
	wh := api.NewWebhookHandler(st, webhookSecret, nil, func() time.Time { return time.Now().UTC() })
	mux.Handle("/api/v1/status-webhook/", wh)

	// Media tasks handler serving files from builder root:
	// GET /media/tasks/{job_id}/task.iso
	mux.HandleFunc("/media/tasks/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		trim := strings.TrimPrefix(r.URL.Path, "/media/tasks/")
		parts := strings.Split(trim, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] != "task.iso" {
			http.NotFound(w, r)
			return
		}
		jobID := parts[0]
		fpath := filepath.Join(taskRoot, jobID, "task.iso")
		http.ServeFile(w, r, fpath)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// Now that we know server URL, set MaintenanceISOURL used by POST /jobs
	ap.MaintenanceISOURL = srv.URL + "/isos/bootc-maintenance.iso"

	// Start worker with noop Redfish client and file builder
	workerCtx, workerCancel := context.WithCancel(context.Background())
	t.Cleanup(workerCancel)

	mediaBase := srv.URL + "/media/tasks"
	rfFactory := func(ctx context.Context, s *provisioner.Server) (redfish.Client, error) {
		return redfish.NewNoopClient(redfish.Config{
			Endpoint: s.BMCAddress,
			Username: s.BMCUser,
			Password: s.BMCPass,
			Vendor:   s.Vendor,
			Timeout:  2 * time.Second,
			Logger:   nil,
		}, 25*time.Millisecond), nil
	}
	wcfg := jobs.WorkerConfig{
		WorkerID:          "w1",
		PollInterval:      10 * time.Millisecond,
		LeaseTTL:          500 * time.Millisecond,
		ExtendLeaseEvery:  200 * time.Millisecond,
		JobStuckTimeout:   5 * time.Second,
		RedfishTimeout:    500 * time.Millisecond,
		TaskISOMediaBase:  mediaBase,
		LogEveryHeartbeat: false,
	}
	w := jobs.NewWorker(st, builder, rfFactory, wcfg, nil)
	go w.Run(workerCtx)

	// Submit a job via API
	client := srv.Client()
	jobReq := map[string]any{
		"server_serial": serial,
		"recipe":        map[string]any{"task_target": "install-linux.target", "target_disk": "/dev/sda"},
	}
	cjResp, jobID := postCreateJob(t, client, srv.URL, jobReq)

	if cjResp.JobID == "" || cjResp.JobID != jobID {
		t.Fatalf("invalid create job response: %+v (jobID=%s)", cjResp, jobID)
	}

	// Wait for worker to pick job and build task ISO (status -> provisioning, task.iso created)
	waitUntil(t, 3*time.Second, 10*time.Millisecond, func() bool {
		j, err := st.GetJobByID(ctx, jobID)
		if err != nil {
			return false
		}
		if j.Status != provisioner.JobStatusProvisioning {
			return false
		}
		if j.TaskISOPath == nil {
			return false
		}
		_, err = os.Stat(*j.TaskISOPath)
		return err == nil
	})

	// Assert media endpoint serves the same bytes as on disk
	onDisk, err := os.ReadFile(filepath.Join(taskRoot, jobID, "task.iso"))
	if err != nil {
		t.Fatalf("read on-disk task.iso: %v", err)
	}
	httpContent := httpGetBytes(t, client, fmt.Sprintf("%s/media/tasks/%s/task.iso", srv.URL, jobID))
	if !bytes.Equal(onDisk, httpContent) {
		t.Fatalf("media served content mismatch with on-disk file")
	}

	// Send success webhook
	headers := map[string]string{"X-Webhook-Secret": webhookSecret}
	doJSON(t, client, http.MethodPost, fmt.Sprintf("%s/api/v1/status-webhook/%s", srv.URL, serial), map[string]any{"status": "success"}, headers)

	// Wait for worker cleanup and final completion
	waitUntil(t, 4*time.Second, 20*time.Millisecond, func() bool {
		j, err := st.GetJobByID(ctx, jobID)
		if err != nil {
			return false
		}
		return j.Status == provisioner.JobStatusComplete
	})

	// Verify events include a webhook success entry (appended either by webhook handler or worker)
	evs, err := st.ListJobEvents(ctx, jobID, 0)
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	found := false
	for _, e := range evs {
		if strings.Contains(strings.ToLower(e.Message), "webhook reported success") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected at least one 'Webhook reported success' event, got %d events", len(evs))
	}
}

func TestIntegration_EndToEndPhase1Failed(t *testing.T) {
	// Store and server seed
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	serial := "SER-INTEG-FAIL"
	if err := st.UpsertServer(ctx, provisioner.Server{
		Serial:     serial,
		BMCAddress: "https://bmc.example",
		BMCUser:    "root",
		BMCPass:    "pw",
	}); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	taskRoot := t.TempDir()
	builder := iso.NewFileBuilder(taskRoot)

	mux := http.NewServeMux()
	ap := api.New(st, "http://localhost/isos/bootc-maintenance.iso", nil)
	ap.Register(mux)
	webhookSecret := "testsecret"
	wh := api.NewWebhookHandler(st, webhookSecret, nil, func() time.Time { return time.Now().UTC() })
	mux.Handle("/api/v1/status-webhook/", wh)
	mux.HandleFunc("/media/tasks/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		trim := strings.TrimPrefix(r.URL.Path, "/media/tasks/")
		parts := strings.Split(trim, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] != "task.iso" {
			http.NotFound(w, r)
			return
		}
		jobID := parts[0]
		http.ServeFile(w, r, filepath.Join(taskRoot, jobID, "task.iso"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ap.MaintenanceISOURL = srv.URL + "/isos/bootc-maintenance.iso"

	workerCtx, workerCancel := context.WithCancel(context.Background())
	t.Cleanup(workerCancel)
	rfFactory := func(ctx context.Context, s *provisioner.Server) (redfish.Client, error) {
		return redfish.NewNoopClient(redfish.Config{
			Endpoint: s.BMCAddress,
			Username: s.BMCUser,
			Password: s.BMCPass,
			Timeout:  2 * time.Second,
		}, 25*time.Millisecond), nil
	}
	w := jobs.NewWorker(st, builder, rfFactory, jobs.WorkerConfig{
		WorkerID:         "wf1",
		PollInterval:     10 * time.Millisecond,
		LeaseTTL:         500 * time.Millisecond,
		ExtendLeaseEvery: 200 * time.Millisecond,
		JobStuckTimeout:  5 * time.Second,
		RedfishTimeout:   500 * time.Millisecond,
		TaskISOMediaBase: srv.URL + "/media/tasks",
	}, nil)
	go w.Run(workerCtx)

	client := srv.Client()
	cj, jobID := postCreateJob(t, client, srv.URL, map[string]any{
		"server_serial": serial,
		"recipe":        map[string]any{"task_target": "install-linux.target", "target_disk": "/dev/sda"},
	})
	if cj.JobID == "" {
		t.Fatalf("create job failed: %+v", cj)
	}

	// Wait for provisioning and task.iso existence
	waitUntil(t, 3*time.Second, 10*time.Millisecond, func() bool {
		j, err := st.GetJobByID(ctx, jobID)
		if err != nil {
			return false
		}
		if j.Status != provisioner.JobStatusProvisioning || j.TaskISOPath == nil {
			return false
		}
		_, err = os.Stat(*j.TaskISOPath)
		return err == nil
	})

	// Send failed webhook with a failed_step
	headers := map[string]string{"X-Webhook-Secret": webhookSecret}
	failStep := "bootloader-linux.service"
	doJSON(t, client, http.MethodPost, fmt.Sprintf("%s/api/v1/status-webhook/%s", srv.URL, serial), map[string]any{
		"status":      "failed",
		"failed_step": failStep,
	}, headers)

	waitUntil(t, 4*time.Second, 20*time.Millisecond, func() bool {
		j, err := st.GetJobByID(ctx, jobID)
		return err == nil && j.Status == provisioner.JobStatusComplete
	})

	// Verify failure event recorded
	evs, err := st.ListJobEvents(ctx, jobID, 0)
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	found := false
	for _, e := range evs {
		if strings.Contains(strings.ToLower(e.Message), "webhook reported failure") &&
			(failStep == "" || (e.Step != nil && *e.Step == failStep)) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected failure event mentioning step %q", failStep)
	}
}

func TestIntegration_EndToEndPhase1TimeoutFailure(t *testing.T) {
	// Store and server seed
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "store.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	serial := "SER-INTEG-TIMEOUT"
	if err := st.UpsertServer(ctx, provisioner.Server{
		Serial:     serial,
		BMCAddress: "https://bmc.example",
		BMCUser:    "root",
		BMCPass:    "pw",
	}); err != nil {
		t.Fatalf("seed server: %v", err)
	}

	taskRoot := t.TempDir()
	builder := iso.NewFileBuilder(taskRoot)

	mux := http.NewServeMux()
	ap := api.New(st, "http://localhost/isos/bootc-maintenance.iso", nil)
	ap.Register(mux)
	// Webhook registered but we won't call it to force timeout
	mux.Handle("/api/v1/status-webhook/", api.NewWebhookHandler(st, "secret", nil, func() time.Time { return time.Now().UTC() }))
	mux.HandleFunc("/media/tasks/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		trim := strings.TrimPrefix(r.URL.Path, "/media/tasks/")
		parts := strings.Split(trim, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] != "task.iso" {
			http.NotFound(w, r)
			return
		}
		jobID := parts[0]
		http.ServeFile(w, r, filepath.Join(taskRoot, jobID, "task.iso"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ap.MaintenanceISOURL = srv.URL + "/isos/bootc-maintenance.iso"

	workerCtx, workerCancel := context.WithCancel(context.Background())
	t.Cleanup(workerCancel)
	rfFactory := func(ctx context.Context, s *provisioner.Server) (redfish.Client, error) {
		return redfish.NewNoopClient(redfish.Config{
			Endpoint: s.BMCAddress,
			Username: s.BMCUser,
			Password: s.BMCPass,
			Timeout:  2 * time.Second,
		}, 10*time.Millisecond), nil
	}
	// Use small JobStuckTimeout to keep test fast
	w := jobs.NewWorker(st, builder, rfFactory, jobs.WorkerConfig{
		WorkerID:         "wt1",
		PollInterval:     10 * time.Millisecond,
		LeaseTTL:         200 * time.Millisecond,
		ExtendLeaseEvery: 50 * time.Millisecond,
		JobStuckTimeout:  250 * time.Millisecond,
		RedfishTimeout:   100 * time.Millisecond,
		TaskISOMediaBase: srv.URL + "/media/tasks",
	}, nil)
	go w.Run(workerCtx)

	client := srv.Client()
	_, jobID := postCreateJob(t, client, srv.URL, map[string]any{
		"server_serial": serial,
		"recipe":        map[string]any{"task_target": "install-linux.target", "target_disk": "/dev/sda"},
	})

	// Wait for completion due to webhook timeout path
	waitForJobStatus(t, ctx, st, jobID, provisioner.JobStatusComplete, 8*time.Second)

	// Verify an event indicates webhook timeout/failure
	evs, err := st.ListJobEvents(ctx, jobID, 0)
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	found := false
	for _, e := range evs {
		if strings.Contains(strings.ToLower(e.Message), "await webhook error") ||
			strings.Contains(strings.ToLower(e.Message), "webhook wait timeout") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected an event indicating webhook timeout/failure, got %d events", len(evs))
	}
}

// Helpers

type createJobResp struct {
	JobID        string `json:"job_id"`
	Status       string `json:"status"`
	ServerSerial string `json:"server_serial"`
	CreatedAt    string `json:"created_at"`
}

func postCreateJob(t *testing.T, client *http.Client, baseURL string, body any) (createJobResp, string) {
	t.Helper()
	resp, data := doJSON(t, client, http.MethodPost, baseURL+"/api/v1/jobs", body, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("create job expected 202, got %d: %s", resp.StatusCode, string(data))
	}
	var cj createJobResp
	if err := json.Unmarshal(data, &cj); err != nil {
		t.Fatalf("decode create job resp: %v", err)
	}
	return cj, cj.JobID
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

func httpGetBytes(t *testing.T, client *http.Client, url string) []byte {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("http get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("http get unexpected status: %d: %s", resp.StatusCode, string(b))
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return b
}

func waitUntil(t *testing.T, timeout, step time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %s", timeout)
		}
		time.Sleep(step)
	}
}

func waitForJobStatus(t *testing.T, ctx context.Context, st *store.Store, jobID string, want provisioner.JobStatus, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		j, err := st.GetJobByID(ctx, jobID)
		if err == nil && j.Status == want {
			return
		}
		if time.Now().After(deadline) {
			status := "<error>"
			if err == nil {
				status = j.Status.String()
			} else {
				status = err.Error()
			}
			ev, _ := st.ListJobEvents(ctx, jobID, 0)
			var b strings.Builder
			for _, e := range ev {
				b.WriteString(fmt.Sprintf("[%s %s]", e.Level, e.Message))
			}
			t.Fatalf("job %s did not reach %s (status=%s events=%s)", jobID, want, status, b.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
