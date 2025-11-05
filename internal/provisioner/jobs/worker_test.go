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
}

func (f *fakeStore) GetServerBySerial(ctx context.Context, serial string) (*provisioner.Server, error) {
	return nil, errors.New("not used in this test")
}

func (f *fakeStore) AcquireQueuedJob(ctx context.Context, workerID string, leaseTTL time.Duration) (*provisioner.Job, error) {
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
