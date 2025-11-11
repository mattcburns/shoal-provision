package jobs

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

// Tests for Worker.awaitWebhook behavior using a fake store to lock
// leasing and transition semantics for Phase 1.

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"shoal/internal/provisioner/iso"
	"shoal/internal/provisioner/redfish"
	"shoal/pkg/provisioner"
)

type fakeStore struct {
	mu           sync.Mutex
	job          *provisioner.Job
	extendCount  int
	lastLeaseJID string
	lastLeaseWID string
	events       []provisioner.JobEvent
	// Overridable handlers
	acquireJobFunc    func(ctx context.Context, workerID string, leaseTTL time.Duration) (*provisioner.Job, error)
	getServerFunc     func(ctx context.Context, serial string) (*provisioner.Server, error)
	markJobStatusFunc func(ctx context.Context, id string, status provisioner.JobStatus, failedStep *string) error
}

func (f *fakeStore) GetServerBySerial(ctx context.Context, serial string) (*provisioner.Server, error) {
	if f.getServerFunc != nil {
		return f.getServerFunc(ctx, serial)
	}
	return nil, errors.New("not used in this test")
}

func (f *fakeStore) AcquireQueuedJob(ctx context.Context, workerID string, leaseTTL time.Duration) (*provisioner.Job, error) {
	if f.acquireJobFunc != nil {
		return f.acquireJobFunc(ctx, workerID, leaseTTL)
	}
	return nil, errors.New("not used in this test")
}

func (f *fakeStore) ExtendLease(ctx context.Context, jobID, workerID string, leaseTTL time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.extendCount++
	f.lastLeaseJID = jobID
	f.lastLeaseWID = workerID
	return true, nil
}

func (f *fakeStore) GetJobByID(ctx context.Context, id string) (*provisioner.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.job == nil || f.job.ID != id {
		return nil, errors.New("not found")
	}
	// Return a shallow copy to avoid external mutation without lock.
	j := *f.job
	return &j, nil
}

func (f *fakeStore) MarkJobStatus(ctx context.Context, id string, status provisioner.JobStatus, failedStep *string) error {
	if f.markJobStatusFunc != nil {
		return f.markJobStatusFunc(ctx, id, status, failedStep)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.job != nil && f.job.ID == id {
		f.job.Status = status
		f.job.FailedStep = failedStep
		return nil
	}
	return errors.New("not found")
}

func (f *fakeStore) UpdateJobTaskISOPath(ctx context.Context, id, path string) error {
	return nil
}

func (f *fakeStore) AppendJobEvent(ctx context.Context, ev provisioner.JobEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, ev)
	return nil
}

// nopBuilder satisfies iso.Builder but should not be invoked in these tests.
type nopBuilder struct{}

func (n *nopBuilder) BuildTaskISO(ctx context.Context, jobID string, recipe json.RawMessage, assets iso.Assets) (*iso.Result, error) {
	return nil, errors.New("not used")
}

func rfNoopFactory(ctx context.Context, s *provisioner.Server) (redfish.Client, error) {
	return nil, nil
}

func newWorkerForTest(t *testing.T, fs *fakeStore, wid string, poll, leaseTTL, extendEvery, stuckTimeout time.Duration) *Worker {
	t.Helper()
	cfg := WorkerConfig{
		WorkerID:          wid,
		PollInterval:      poll,
		LeaseTTL:          leaseTTL,
		ExtendLeaseEvery:  extendEvery,
		JobStuckTimeout:   stuckTimeout,
		RedfishTimeout:    50 * time.Millisecond,
		TaskISOMediaBase:  "http://controller/media/tasks",
		LogEveryHeartbeat: true,
	}
	w := NewWorker(fs, &nopBuilder{}, rfNoopFactory, cfg, nil)
	// Keep real time for simplicity (uses small durations to keep tests fast)
	return w
}

func TestAwaitWebhook_SuccessTransition(t *testing.T) {
	fs := &fakeStore{
		job: &provisioner.Job{
			ID:     "job-1",
			Status: provisioner.JobStatusProvisioning,
		},
	}
	w := newWorkerForTest(t, fs, "w1", 10*time.Millisecond, 200*time.Millisecond, 30*time.Millisecond, 500*time.Millisecond)

	// Flip job to succeeded shortly after
	go func() {
		time.Sleep(40 * time.Millisecond)
		_ = fs.MarkJobStatus(context.Background(), "job-1", provisioner.JobStatusSucceeded, nil)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	status, err := w.awaitWebhook(ctx, fs.job)
	if err != nil {
		t.Fatalf("awaitWebhook returned error: %v", err)
	}
	if status != provisioner.JobStatusSucceeded {
		t.Fatalf("expected status succeeded, got %s", status)
	}

	// Verify lease was extended at least once and recorded correct job/worker ids
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.extendCount < 1 {
		t.Fatalf("expected ExtendLease to be called at least once, got %d", fs.extendCount)
	}
	if fs.lastLeaseJID != "job-1" || fs.lastLeaseWID != "w1" {
		t.Fatalf("unexpected lease tracking: jobID=%s workerID=%s", fs.lastLeaseJID, fs.lastLeaseWID)
	}
}

func TestAwaitWebhook_FailedTransition(t *testing.T) {
	fs := &fakeStore{
		job: &provisioner.Job{
			ID:     "job-2",
			Status: provisioner.JobStatusProvisioning,
		},
	}
	w := newWorkerForTest(t, fs, "w2", 10*time.Millisecond, 200*time.Millisecond, 30*time.Millisecond, 500*time.Millisecond)

	// Flip job to failed with a step
	go func() {
		time.Sleep(35 * time.Millisecond)
		step := "bootloader-linux.service"
		_ = fs.MarkJobStatus(context.Background(), "job-2", provisioner.JobStatusFailed, &step)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	status, err := w.awaitWebhook(ctx, fs.job)
	if err != nil {
		t.Fatalf("awaitWebhook returned error: %v", err)
	}
	if status != provisioner.JobStatusFailed {
		t.Fatalf("expected status failed, got %s", status)
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.extendCount < 1 {
		t.Fatalf("expected ExtendLease to be called at least once, got %d", fs.extendCount)
	}
}

func TestAwaitWebhook_TimeoutExtendsLeaseRepeatedly(t *testing.T) {
	fs := &fakeStore{
		job: &provisioner.Job{
			ID:     "job-3",
			Status: provisioner.JobStatusProvisioning,
		},
	}
	// Aggressive timing to keep test fast:
	// - poll every 10ms
	// - lease TTL 100ms
	// - extend every 20ms
	// - overall stuck timeout 120ms (should time out)
	w := newWorkerForTest(t, fs, "w3", 10*time.Millisecond, 100*time.Millisecond, 20*time.Millisecond, 120*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	status, err := w.awaitWebhook(ctx, fs.job)
	if err == nil {
		t.Fatalf("expected awaitWebhook to time out, got nil error and status=%s", status)
	}
	if status != "" {
		t.Fatalf("expected empty status on timeout, got %s", status)
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.extendCount < 3 {
		t.Fatalf("expected multiple lease extensions on timeout, got %d", fs.extendCount)
	}
	if fs.lastLeaseJID != "job-3" || fs.lastLeaseWID != "w3" {
		t.Fatalf("unexpected lease tracking: jobID=%s workerID=%s", fs.lastLeaseJID, fs.lastLeaseWID)
	}
}

// TestWorker_RunLoop tests the main worker Run loop's ability to acquire
// jobs, handle cancellation, and poll correctly.
func TestWorker_RunLoop(t *testing.T) {
	t.Parallel()

	jobCount := 0
	fs := &fakeStore{
		acquireJobFunc: func(ctx context.Context, workerID string, leaseTTL time.Duration) (*provisioner.Job, error) {
			if jobCount == 0 {
				jobCount++
				return &provisioner.Job{
					ID:           "test-job",
					ServerSerial: "TESTSERIAL",
					Status:       provisioner.JobStatusQueued,
					Recipe:       json.RawMessage(`{"target":"linux"}`),
				}, nil
			}
			return nil, errors.New("no jobs available")
		},
	}

	w := newWorkerForTest(t, fs, "test-worker", 10*time.Millisecond, 100*time.Millisecond, 30*time.Millisecond, 500*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Run should exit when context is canceled
	w.Run(ctx)

	// Verify job was acquired at least once
	if jobCount == 0 {
		t.Fatal("expected Run to acquire at least one job")
	}
}

// TestWorker_RunLoopCancellation tests that Run respects context cancellation
func TestWorker_RunLoopCancellation(t *testing.T) {
	t.Parallel()

	acquireCount := 0
	fs := &fakeStore{
		acquireJobFunc: func(ctx context.Context, workerID string, leaseTTL time.Duration) (*provisioner.Job, error) {
			acquireCount++
			return nil, errors.New("no jobs")
		},
	}

	w := newWorkerForTest(t, fs, "cancel-test", 5*time.Millisecond, 100*time.Millisecond, 30*time.Millisecond, 500*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	// Let it poll a few times
	time.Sleep(25 * time.Millisecond)
	cancel()

	// Wait for Run to exit
	select {
	case <-done:
		// Success
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Run did not exit after context cancellation")
	}

	if acquireCount < 2 {
		t.Fatalf("expected multiple acquire attempts, got %d", acquireCount)
	}
}

// TestWorker_ProcessJobMissingServer tests processJob failure when server not found
func TestWorker_ProcessJobMissingServer(t *testing.T) {
	t.Parallel()

	statusCalled := false
	fs := &fakeStore{
		getServerFunc: func(ctx context.Context, serial string) (*provisioner.Server, error) {
			return nil, errors.New("server not found")
		},
		markJobStatusFunc: func(ctx context.Context, id string, status provisioner.JobStatus, failedStep *string) error {
			statusCalled = true
			if status != provisioner.JobStatusFailed {
				t.Errorf("expected status Failed, got %s", status)
			}
			if failedStep == nil || *failedStep != "resolve-server" {
				t.Errorf("expected failedStep=resolve-server, got %v", failedStep)
			}
			return nil
		},
	}

	w := newWorkerForTest(t, fs, "test", 10*time.Millisecond, 100*time.Millisecond, 30*time.Millisecond, 500*time.Millisecond)

	job := &provisioner.Job{
		ID:           "job-missing-server",
		ServerSerial: "NONEXISTENT",
		Recipe:       json.RawMessage(`{"target":"linux"}`),
	}

	err := w.processJob(context.Background(), job)
	if err == nil {
		t.Fatal("expected processJob to return error for missing server")
	}
	if !statusCalled {
		t.Error("expected MarkJobStatus to be called")
	}
}

// TestWorker_HelperFunctions tests utility functions for complete coverage
func TestWorker_HelperFunctions(t *testing.T) {
	t.Parallel()

	// Test displayDuration edge cases
	d1 := displayDuration(45 * time.Second)
	if d1 != 45*time.Second {
		t.Errorf("displayDuration(45s) = %s, want 45s", d1)
	}
	d2 := displayDuration(90*time.Second + 500*time.Millisecond)
	expected := (90*time.Second + 500*time.Millisecond).Round(time.Millisecond)
	if d2 != expected {
		t.Errorf("displayDuration(90.5s) = %s, want %s", d2, expected)
	}
	d3 := displayDuration(0)
	if d3 != 0 {
		t.Errorf("displayDuration(0) = %s, want 0", d3)
	}

	// Test truncate
	long := "this is a very long string that should be truncated"
	short := truncate(long, 10)
	if len(short) > 13 { // 10 + "..."
		t.Errorf("truncate exceeded max length: %d", len(short))
	}
	notLong := "short"
	notTruncated := truncate(notLong, 10)
	if notTruncated != "short" {
		t.Errorf("truncate(short, 10) = %s, want short", notTruncated)
	}

	// Test minDur
	min := minDur(5*time.Second, 10*time.Second)
	if min != 5*time.Second {
		t.Errorf("minDur(5s, 10s) = %s, want 5s", min)
	}
	min2 := minDur(15*time.Second, 10*time.Second)
	if min2 != 10*time.Second {
		t.Errorf("minDur(15s, 10s) = %s, want 10s", min2)
	}

	// Test strPtr
	s := "test"
	ptr := strPtr(s)
	if ptr == nil || *ptr != "test" {
		t.Error("strPtr failed to create pointer correctly")
	}
}

// TestWorker_ComposeTaskMediaURL tests URL composition with valid and invalid base URLs
func TestWorker_ComposeTaskMediaURL(t *testing.T) {
	t.Parallel()

	fs := &fakeStore{}

	// Test with valid base URL
	w1 := newWorkerForTest(t, fs, "test", 10*time.Millisecond, 100*time.Millisecond, 30*time.Millisecond, 500*time.Millisecond)
	url, err := w1.composeTaskMediaURL("job-123")
	if err != nil {
		t.Fatalf("composeTaskMediaURL failed: %v", err)
	}
	expected := "http://controller/media/tasks/job-123/task.iso"
	if url != expected {
		t.Errorf("composeTaskMediaURL = %s, want %s", url, expected)
	}

	// Test with invalid base URL (should still succeed with url.JoinPath)
	cfg := WorkerConfig{
		WorkerID:          "test",
		PollInterval:      10 * time.Millisecond,
		LeaseTTL:          100 * time.Millisecond,
		ExtendLeaseEvery:  30 * time.Millisecond,
		JobStuckTimeout:   500 * time.Millisecond,
		RedfishTimeout:    50 * time.Millisecond,
		TaskISOMediaBase:  ":::invalid:::",
		LogEveryHeartbeat: false,
	}
	w2 := NewWorker(fs, &nopBuilder{}, rfNoopFactory, cfg, nil)
	_, err2 := w2.composeTaskMediaURL("job-456")
	// JoinPath is lenient, but we expect an error for malformed URL
	if err2 == nil {
		t.Error("expected error for malformed base URL")
	}
}

// TestWorker_LoggingCoverage tests logf function branches
func TestWorker_LoggingCoverage(t *testing.T) {
	t.Parallel()

	fs := &fakeStore{}

	// Test with LogEveryHeartbeat = true
	cfg1 := WorkerConfig{
		WorkerID:          "verbose",
		PollInterval:      10 * time.Millisecond,
		LeaseTTL:          100 * time.Millisecond,
		ExtendLeaseEvery:  30 * time.Millisecond,
		JobStuckTimeout:   500 * time.Millisecond,
		RedfishTimeout:    50 * time.Millisecond,
		TaskISOMediaBase:  "http://test",
		LogEveryHeartbeat: true,
	}
	w1 := NewWorker(fs, &nopBuilder{}, rfNoopFactory, cfg1, nil)
	w1.logf("test message: %d", 42) // Should print

	// Test with LogEveryHeartbeat = false
	cfg2 := WorkerConfig{
		WorkerID:          "quiet",
		PollInterval:      10 * time.Millisecond,
		LeaseTTL:          100 * time.Millisecond,
		ExtendLeaseEvery:  30 * time.Millisecond,
		JobStuckTimeout:   500 * time.Millisecond,
		RedfishTimeout:    50 * time.Millisecond,
		TaskISOMediaBase:  "http://test",
		LogEveryHeartbeat: false,
	}
	w2 := NewWorker(fs, &nopBuilder{}, rfNoopFactory, cfg2, nil)
	w2.logf("lease extended") // Should NOT print (heartbeat suppression)
	w2.logf("other message")  // Should print (not a heartbeat)
}
