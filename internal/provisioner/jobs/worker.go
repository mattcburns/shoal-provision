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

// Package jobs implements the controller worker that acquires queued jobs,
// builds the task ISO (stub), performs Redfish orchestration (noop client
// in Phase 1), logs events, and waits for a webhook-driven completion.
import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"path"
	"strings"
	"time"

	"shoal/internal/provisioner/iso"
	"shoal/internal/provisioner/redfish"
	"shoal/pkg/provisioner"
)

// Store defines the persistence operations required by the worker.
type Store interface {
	// Servers
	GetServerBySerial(ctx context.Context, serial string) (*provisioner.Server, error)

	// Jobs
	AcquireQueuedJob(ctx context.Context, workerID string, leaseTTL time.Duration) (*provisioner.Job, error)
	ExtendLease(ctx context.Context, jobID, workerID string, leaseTTL time.Duration) (bool, error)
	GetJobByID(ctx context.Context, id string) (*provisioner.Job, error)
	MarkJobStatus(ctx context.Context, id string, status provisioner.JobStatus, failedStep *string) error
	UpdateJobTaskISOPath(ctx context.Context, id, path string) error

	// Events
	AppendJobEvent(ctx context.Context, ev provisioner.JobEvent) error
}

// RFClientFactory creates a Redfish client for a given server.
type RFClientFactory func(ctx context.Context, server *provisioner.Server) (redfish.Client, error)

// WorkerConfig controls worker behavior and timeouts.
type WorkerConfig struct {
	WorkerID string

	// How often to poll for new jobs when none are available.
	PollInterval time.Duration

	// Lease management
	LeaseTTL          time.Duration
	ExtendLeaseEvery  time.Duration
	JobStuckTimeout   time.Duration
	RedfishTimeout    time.Duration
	TaskISOMediaBase  string // e.g., "http://controller.internal:8080/media/tasks"
	LogEveryHeartbeat bool
}

// Worker performs job orchestration per the design documents.
type Worker struct {
	store        Store
	isoBuilder   iso.Builder
	newRFClient  RFClientFactory
	cfg          WorkerConfig
	logger       *log.Logger
	now          func() time.Time
	assetsSchema []byte // optional recipe.schema.json (future 022 integration)
}

// NewWorker constructs a new Worker.
func NewWorker(store Store, isoBuilder iso.Builder, newRF RFClientFactory, cfg WorkerConfig, logger *log.Logger) *Worker {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = 10 * time.Minute
	}
	if cfg.ExtendLeaseEvery <= 0 || cfg.ExtendLeaseEvery >= cfg.LeaseTTL {
		cfg.ExtendLeaseEvery = cfg.LeaseTTL / 2
	}
	if cfg.JobStuckTimeout <= 0 {
		cfg.JobStuckTimeout = 4 * time.Hour
	}
	if cfg.RedfishTimeout <= 0 {
		cfg.RedfishTimeout = 30 * time.Second
	}
	return &Worker{
		store:       store,
		isoBuilder:  isoBuilder,
		newRFClient: newRF,
		cfg:         cfg,
		logger:      logger,
		now:         func() time.Time { return time.Now().UTC() },
	}
}

func (w *Worker) logf(format string, args ...any) {
	if w.logger != nil {
		w.logger.Printf("[worker %s] %s", w.cfg.WorkerID, fmt.Sprintf(format, args...))
	}
}

// Run starts the worker loop that acquires and processes jobs until ctx is canceled.
func (w *Worker) Run(ctx context.Context) {
	w.logf("starting worker; poll=%s lease_ttl=%s", w.cfg.PollInterval, w.cfg.LeaseTTL)
	defer w.logf("worker stopped")

	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	for {
		// Try to acquire a job
		job, err := w.store.AcquireQueuedJob(ctx, w.cfg.WorkerID, w.cfg.LeaseTTL)
		if err == nil && job != nil {
			w.logf("acquired job id=%s serial=%s", job.ID, job.ServerSerial)
			if err := w.processJob(ctx, job); err != nil {
				w.logf("job %s processing error: %v", job.ID, err)
				// processJob is responsible for updating job status on error
			}
			continue
		}
		// If context canceled, stop
		if ctx.Err() != nil {
			return
		}
		// Sleep/poll
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) processJob(ctx context.Context, job *provisioner.Job) error {
	start := w.now()
	step := "start"
	_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelInfo, "Starting provisioning orchestration", &step)

	// Resolve server
	srv, err := w.store.GetServerBySerial(ctx, job.ServerSerial)
	if err != nil {
		step = "resolve-server"
		_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelError, fmt.Sprintf("Failed to resolve server: %v", err), &step)
		_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, strPtr("resolve-server"))
		return fmt.Errorf("resolve server: %w", err)
	}

	// Build task ISO (stub)
	step = "build-task-iso"
	res, err := w.isoBuilder.BuildTaskISO(ctx, job.ID, job.Recipe, iso.Assets{
		RecipeSchema: w.assetsSchema,
	})
	if err != nil {
		_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelError, fmt.Sprintf("Task ISO build failed: %v", err), &step)
		_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, &step)
		return fmt.Errorf("build iso: %w", err)
	}
	_ = w.store.UpdateJobTaskISOPath(ctx, job.ID, res.Path)
	_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelInfo, fmt.Sprintf("Task ISO ready size=%d sha256=%s", res.Size, res.SHA256), &step)

	// Create Redfish client (noop in Phase 1)
	rf, err := w.newRFClient(ctx, srv)
	if err != nil {
		step = "redfish-client"
		_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelError, fmt.Sprintf("Redfish client init failed: %v", err), &step)
		_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, &step)
		return fmt.Errorf("redfish client: %w", err)
	}
	defer rf.Close()

	// Orchestration
	// 1) Mount maintenance.iso (CD1)
	step = "mount-maintenance"
	if err := w.withTimeout(ctx, w.cfg.RedfishTimeout, func(c context.Context) error {
		return rf.MountVirtualMedia(c, 1, job.MaintenanceISOURL)
	}); err != nil {
		_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelError, fmt.Sprintf("Mount maintenance ISO failed: %v", err), &step)
		_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, &step)
		return fmt.Errorf("mount maintenance: %w", err)
	}
	_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelInfo, "Mounted maintenance ISO (CD1)", &step)

	// 2) Mount task.iso (CD2) using controller media URL base
	taskURL, err := w.composeTaskMediaURL(job.ID)
	if err != nil {
		step = "compose-task-url"
		_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelError, fmt.Sprintf("Compose task ISO URL failed: %v", err), &step)
		_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, &step)
		return fmt.Errorf("compose task url: %w", err)
	}
	step = "mount-task"
	if err := w.withTimeout(ctx, w.cfg.RedfishTimeout, func(c context.Context) error {
		return rf.MountVirtualMedia(c, 2, taskURL)
	}); err != nil {
		_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelError, fmt.Sprintf("Mount task ISO failed: %v", err), &step)
		_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, &step)
		return fmt.Errorf("mount task: %w", err)
	}
	_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelInfo, "Mounted task ISO (CD2)", &step)

	// 3) Set one-time boot to CD
	step = "set-boot-cd"
	if err := w.withTimeout(ctx, w.cfg.RedfishTimeout, func(c context.Context) error {
		return rf.SetOneTimeBoot(c, redfish.BootDeviceCD)
	}); err != nil {
		_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelError, fmt.Sprintf("Set one-time boot failed: %v", err), &step)
		_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, &step)
		return fmt.Errorf("set boot: %w", err)
	}
	_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelInfo, "Set one-time boot device to CD", &step)

	// 4) Reboot
	step = "reboot"
	if err := w.withTimeout(ctx, w.cfg.RedfishTimeout, func(c context.Context) error {
		return rf.Reboot(c, redfish.RebootGracefulWithFallback)
	}); err != nil {
		_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelError, fmt.Sprintf("Reboot failed: %v", err), &step)
		_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, &step)
		return fmt.Errorf("reboot: %w", err)
	}
	_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelInfo, "Reboot triggered", &step)

	// Wait for webhook: status should transition provisioning -> succeeded|failed
	waitStep := "await-webhook"
	_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelInfo, "Awaiting webhook status update from maintenance OS", &waitStep)
	finalStatus, err := w.awaitWebhook(ctx, job)
	if err != nil {
		// Mark failed due to timeout or internal error
		failStep := "webhook-timeout"
		_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelError, fmt.Sprintf("Await webhook error: %v", err), &failStep)
		_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, &failStep)
	}

	// Cleanup: unmount media and reboot to final OS (best effort)
	cleanupStep := "cleanup"
	_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelInfo, "Cleanup: unmounting virtual media", &cleanupStep)
	_ = w.withTimeout(ctx, w.cfg.RedfishTimeout, func(c context.Context) error { return rf.UnmountVirtualMedia(c, 2) })
	_ = w.withTimeout(ctx, w.cfg.RedfishTimeout, func(c context.Context) error { return rf.UnmountVirtualMedia(c, 1) })
	_ = w.withTimeout(ctx, w.cfg.RedfishTimeout, func(c context.Context) error { return rf.Reboot(c, redfish.RebootGracefulWithFallback) })
	_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelInfo, "Cleanup complete", &cleanupStep)

	// Mark complete (even if earlier status was failed)
	if finalStatus == "" {
		finalStatus = provisioner.JobStatusFailed // conservative default if unknown
	}
	if err := w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusComplete, nil); err != nil {
		w.logf("job %s: failed to mark complete: %v", job.ID, err)
	}
	_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelInfo, fmt.Sprintf("Job finished in %s", w.now().Sub(start).Round(time.Second)), strPtr("done"))
	return nil
}

// awaitWebhook polls for a job status change to succeeded|failed, extending leases.
// Returns the terminal status (succeeded|failed) or error on timeout.
func (w *Worker) awaitWebhook(ctx context.Context, job *provisioner.Job) (provisioner.JobStatus, error) {
	deadline := w.now().Add(w.cfg.JobStuckTimeout)
	nextLease := w.now().Add(w.cfg.ExtendLeaseEvery)
	pollEvery := minDur(w.cfg.PollInterval, 5*time.Second)

	for {
		// Context or timeout
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if w.now().After(deadline) {
			return "", errors.New("webhook wait timeout")
		}

		// Extend lease as needed
		if w.now().After(nextLease) {
			ok, err := w.store.ExtendLease(ctx, job.ID, w.cfg.WorkerID, w.cfg.LeaseTTL)
			if w.cfg.LogEveryHeartbeat {
				w.logf("heartbeat: extend lease job=%s ok=%v err=%v", job.ID, ok, err)
			}
			nextLease = w.now().Add(w.cfg.ExtendLeaseEvery)
		}

		// Check status
		j, err := w.store.GetJobByID(ctx, job.ID)
		if err == nil {
			switch j.Status {
			case provisioner.JobStatusSucceeded:
				_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelInfo, "Webhook reported success", strPtr("webhook-success"))
				return provisioner.JobStatusSucceeded, nil
			case provisioner.JobStatusFailed:
				_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelWarn, "Webhook reported failure", strPtr("webhook-failure"))
				return provisioner.JobStatusFailed, nil
			}
		}

		// Sleep
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(pollEvery):
		}
	}
}

func (w *Worker) withTimeout(ctx context.Context, d time.Duration, fn func(context.Context) error) error {
	ctx2, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	return fn(ctx2)
}

func (w *Worker) appendEvent(ctx context.Context, jobID string, level provisioner.EventLevel, msg string, step *string) error {
	ev := provisioner.JobEvent{
		JobID:   jobID,
		Time:    w.now(),
		Level:   level,
		Message: truncate(msg, 2000),
		Step:    step,
	}
	return w.store.AppendJobEvent(ctx, ev)
}

func (w *Worker) composeTaskMediaURL(jobID string) (string, error) {
	base := strings.TrimSpace(w.cfg.TaskISOMediaBase)
	if base == "" {
		return "", errors.New("TaskISOMediaBase is not set")
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("invalid TaskISOMediaBase: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("TaskISOMediaBase must be http(s), got %q", u.Scheme)
	}
	u.Path = path.Join(u.Path, jobID, "task.iso")
	return u.String(), nil
}

func strPtr(s string) *string { return &s }

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}

func minDur(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// awaitBMCReady polls the Redfish service using Client.Ping until it becomes reachable
// or the provided deadline is exceeded. This scaffolds the ESXi flow where we need to
// detect BMC/API readiness after a reset without relying on a webhook.
func (w *Worker) awaitBMCReady(ctx context.Context, rf redfish.Client, deadline time.Time) error {
	backoff := 200 * time.Millisecond
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return errors.New("esxi: bmc not ready before deadline")
		}
		if err := rf.Ping(ctx); err == nil {
			return nil
		}
		time.Sleep(backoff)
		if backoff < 5*time.Second {
			backoff *= 2
		}
	}
}

// pollPowerStatePlaceholder is a scaffold for ESXi flow power-state based detection.
// TODO(phase2): Implement power/state transitions and stabilization heuristics per 028.
// For now it reuses awaitBMCReady as a placeholder.
func (w *Worker) pollPowerStatePlaceholder(ctx context.Context, rf redfish.Client, deadline time.Time) error {
	return w.awaitBMCReady(ctx, rf, deadline)
}
