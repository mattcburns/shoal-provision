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
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"path"
	"strings"
	"time"

	"shoal/internal/provisioner/iso"
	"shoal/internal/provisioner/metrics"
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

	// ESXi workflow tuning
	ESXIInstallTimeout    time.Duration
	ESXIStableWindow      time.Duration
	ESXIPollIntervalStart time.Duration
	ESXIPollIntervalMax   time.Duration

	// ESXi vendor installer ISO URL (CD1) used for ESXi workflow handoff.
	// Example: https://controller.internal:8080/static/VMware-VMvisor-Installer-8.0U2.iso
	ESXIInstallerURL string
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

const esxiInstallTarget = "install-esxi.target"

const (
	opMountMaintenance      = metrics.OpMountMaintenance
	opMountTask             = metrics.OpMountTask
	opBootOverride          = metrics.OpBootOverride
	opResetGracefulFallback = metrics.OpResetGraceful
	opCleanupUnmountTask    = metrics.OpCleanupUnmountTask
	opCleanupUnmountMaint   = metrics.OpCleanupUnmountMaint
	opCleanupReset          = metrics.OpCleanupReset
	opESXIAwaitBMC          = metrics.OpESXIAwaitBMC
	opESXIPollPower         = metrics.OpESXIPollPower
)

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
	if cfg.ESXIInstallTimeout <= 0 {
		cfg.ESXIInstallTimeout = 90 * time.Minute
	}
	if cfg.ESXIStableWindow <= 0 {
		cfg.ESXIStableWindow = 90 * time.Second
	}
	if cfg.ESXIPollIntervalStart <= 0 {
		cfg.ESXIPollIntervalStart = time.Second
	}
	if cfg.ESXIPollIntervalMax <= 0 || cfg.ESXIPollIntervalMax < cfg.ESXIPollIntervalStart {
		cfg.ESXIPollIntervalMax = 15 * time.Second
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

func (w *Worker) runRedfishOp(ctx context.Context, job *provisioner.Job, step, op string, fn func(context.Context) error) error {
	start := w.now()
	err := w.withTimeout(ctx, w.cfg.RedfishTimeout, fn)
	dur := w.now().Sub(start)
	metrics.ObserveProvisioningPhase(op, dur)
	w.recordOpEvent(ctx, job.ID, step, op, 1, start, err)
	return err
}

func (w *Worker) recordOpEvent(ctx context.Context, jobID, step, op string, attempts int, start time.Time, err error) {
	elapsed := w.now().Sub(start)
	status := "success"
	level := provisioner.EventLevelInfo
	if err != nil {
		status = "error"
		level = provisioner.EventLevelError
	}
	dur := displayDuration(elapsed)
	msg := fmt.Sprintf("op=%s status=%s attempts=%d duration=%s", op, status, attempts, dur)
	if err != nil {
		msg += fmt.Sprintf(" error=%s", truncate(err.Error(), 256))
	}
	_ = w.appendEvent(ctx, jobID, level, msg, &step)
}

func displayDuration(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	if d < time.Millisecond {
		return d
	}
	return d.Round(time.Millisecond)
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

	taskTarget := taskTargetFromRecipe(job.Recipe)
	isESXi := strings.EqualFold(taskTarget, esxiInstallTarget)

	// Phase 6: ESXi requires ks_cfg in recipe; validate early so we fail fast.
	var ksCfg []byte
	if isESXi {
		validateStep := "validate-recipe"
		ksCfg = kickstartFromRecipe(job.Recipe)
		if len(ksCfg) == 0 {
			_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelError, "ESXi workflow: ks_cfg missing in recipe", &validateStep)
			_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, &validateStep)
			return fmt.Errorf("validate recipe: ks_cfg missing for esxi job")
		}
		const maxKickstartSize = 64 * 1024 // 64KiB reasonable constraint; align w/ design 022 size limits assumption
		if len(ksCfg) > maxKickstartSize {
			_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelError, fmt.Sprintf("ESXi workflow: ks_cfg exceeds max size (%d > %d bytes)", len(ksCfg), maxKickstartSize), &validateStep)
			_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, &validateStep)
			return fmt.Errorf("validate recipe: ks_cfg too large (%d bytes)", len(ksCfg))
		}
		_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelInfo, fmt.Sprintf("ESXi workflow: ks_cfg accepted size=%d bytes", len(ksCfg)), &validateStep)
	}

	// Resolve server
	srv, err := w.store.GetServerBySerial(ctx, job.ServerSerial)
	if err != nil {
		step = "resolve-server"
		_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelError, fmt.Sprintf("Failed to resolve server: %v", err), &step)
		_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, strPtr("resolve-server"))
		return fmt.Errorf("resolve server: %w", err)
	}

	// Build task ISO (stub + optional ks.cfg for ESXi). For ESXi we embed ks.cfg at /ks.cfg
	step = "build-task-iso"
	assets := iso.Assets{RecipeSchema: w.assetsSchema}
	if isESXi && len(ksCfg) > 0 {
		assets.Kickstart = ksCfg
	}
	res, err := w.isoBuilder.BuildTaskISO(ctx, job.ID, job.Recipe, assets)
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
	// 1) Mount vendor installer (ESXi) or maintenance.iso (Linux/Windows) as CD1
	step = "mount-maintenance"
	mountURL := job.MaintenanceISOURL
	if isESXi {
		if strings.TrimSpace(w.cfg.ESXIInstallerURL) == "" {
			_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelError, "ESXi workflow: ESXI_INSTALLER_URL not configured", &step)
			_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, &step)
			return fmt.Errorf("esxi: ESXIInstallerURL is empty")
		}
		mountURL = w.cfg.ESXIInstallerURL
	}
	if err := w.runRedfishOp(ctx, job, step, opMountMaintenance, func(c context.Context) error {
		return rf.MountVirtualMedia(c, 1, mountURL)
	}); err != nil {
		_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, &step)
		return fmt.Errorf("mount maintenance/vendor: %w", err)
	}

	// 2) Mount task.iso (CD2) using controller media URL base
	taskURL, err := w.composeTaskMediaURL(job.ID)
	if err != nil {
		step = "compose-task-url"
		_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelError, fmt.Sprintf("Compose task ISO URL failed: %v", err), &step)
		_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, &step)
		return fmt.Errorf("compose task url: %w", err)
	}
	step = "mount-task"
	if err := w.runRedfishOp(ctx, job, step, opMountTask, func(c context.Context) error {
		return rf.MountVirtualMedia(c, 2, taskURL)
	}); err != nil {
		_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, &step)
		return fmt.Errorf("mount task: %w", err)
	}

	// 3) Set one-time boot to CD
	step = "boot-override"
	if err := w.runRedfishOp(ctx, job, step, opBootOverride, func(c context.Context) error {
		return rf.SetOneTimeBoot(c, redfish.BootDeviceCD)
	}); err != nil {
		_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, &step)
		return fmt.Errorf("set boot: %w", err)
	}

	// 4) Reboot
	step = "reset"
	if err := w.runRedfishOp(ctx, job, step, opResetGracefulFallback, func(c context.Context) error {
		return rf.Reboot(c, redfish.RebootGracefulWithFallback)
	}); err != nil {
		_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, &step)
		return fmt.Errorf("reboot: %w", err)
	}

	finalStatus := provisioner.JobStatus("")
	if isESXi {
		status, esxiErr := w.runESXiCompletion(ctx, job, rf)
		finalStatus = status
		if esxiErr != nil {
			w.logf("job %s: ESXi completion error: %v", job.ID, esxiErr)
		}
	} else {
		// Wait for webhook: status should transition provisioning -> succeeded|failed
		waitStep := "await-webhook"
		_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelInfo, "Awaiting webhook status update from maintenance OS", &waitStep)
		status, err := w.awaitWebhook(ctx, job)
		finalStatus = status
		if err != nil {
			// Mark failed due to timeout or internal error
			failStep := "webhook-timeout"
			_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelError, fmt.Sprintf("Await webhook error: %v", err), &failStep)
			_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, &failStep)
		}
	}

	// Cleanup: unmount media and reboot to final OS (best effort)
	cleanupStep := "cleanup"
	_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelInfo, "Cleanup: unmounting virtual media", &cleanupStep)
	if err := w.runRedfishOp(ctx, job, opCleanupUnmountTask, opCleanupUnmountTask, func(c context.Context) error {
		return rf.UnmountVirtualMedia(c, 2)
	}); err != nil {
		w.logf("job %s: cleanup unmount task ISO error: %v", job.ID, err)
	}
	if err := w.runRedfishOp(ctx, job, opCleanupUnmountMaint, opCleanupUnmountMaint, func(c context.Context) error {
		return rf.UnmountVirtualMedia(c, 1)
	}); err != nil {
		w.logf("job %s: cleanup unmount maintenance ISO error: %v", job.ID, err)
	}
	if err := w.runRedfishOp(ctx, job, opCleanupReset, opCleanupReset, func(c context.Context) error {
		return rf.Reboot(c, redfish.RebootGracefulWithFallback)
	}); err != nil {
		w.logf("job %s: cleanup reboot error: %v", job.ID, err)
	}
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
// or the provided deadline is exceeded. This supports the ESXi flow where we need to
// detect BMC/API readiness after a reset without relying on a webhook.
func (w *Worker) awaitBMCReady(ctx context.Context, job *provisioner.Job, rf redfish.Client, deadline time.Time) error {
	backoff := 200 * time.Millisecond
	maxBackoff := 5 * time.Second
	start := w.now()
	attempts := 0
	for {
		if w.now().After(deadline) {
			err := errors.New("esxi: bmc not ready before deadline")
			if attempts == 0 {
				attempts = 1
			}
			w.recordOpEvent(ctx, job.ID, opESXIAwaitBMC, opESXIAwaitBMC, attempts, start, err)
			metrics.ObserveProvisioningPhase(opESXIAwaitBMC, w.now().Sub(start))
			return err
		}

		attempts++
		if err := rf.Ping(ctx); err == nil {
			w.recordOpEvent(ctx, job.ID, opESXIAwaitBMC, opESXIAwaitBMC, attempts, start, nil)
			metrics.ObserveProvisioningPhase(opESXIAwaitBMC, w.now().Sub(start))
			return nil
		}

		select {
		case <-ctx.Done():
			err := ctx.Err()
			w.recordOpEvent(ctx, job.ID, opESXIAwaitBMC, opESXIAwaitBMC, attempts, start, err)
			metrics.ObserveProvisioningPhase(opESXIAwaitBMC, w.now().Sub(start))
			return err
		case <-time.After(backoff):
		}

		if backoff < maxBackoff {
			backoff = minDur(backoff*2, maxBackoff)
		}
	}
}

// runESXiCompletion coordinates the post-reset polling flow for ESXi installations,
// updating job status and events as milestones are reached or deadlines expire.
func (w *Worker) runESXiCompletion(ctx context.Context, job *provisioner.Job, rf redfish.Client) (provisioner.JobStatus, error) {
	step := strPtr("redfish.poll")
	deadline := w.now().Add(w.cfg.ESXIInstallTimeout)
	timeoutDisplay := w.cfg.ESXIInstallTimeout
	if timeoutDisplay < time.Minute {
		timeoutDisplay = timeoutDisplay.Round(time.Second)
	} else {
		timeoutDisplay = timeoutDisplay.Round(time.Minute)
	}
	_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelInfo,
		fmt.Sprintf("ESXi workflow: monitoring BMC for installer completion (timeout %s)", timeoutDisplay), step)

	if err := w.awaitBMCReady(ctx, job, rf, deadline); err != nil {
		err = fmt.Errorf("esxi: bmc not reachable after reset: %w", err)
		_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelError, err.Error(), step)
		_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, step)
		return provisioner.JobStatusFailed, err
	}

	_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelInfo,
		"ESXi workflow: BMC reachable; observing power state transitions", step)

	if err := w.pollESXiPowerState(ctx, job.ID, rf, deadline); err != nil {
		err = fmt.Errorf("esxi: power state monitoring failed: %w", err)
		_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelError, err.Error(), step)
		_ = w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusFailed, step)
		return provisioner.JobStatusFailed, err
	}

	stableDisplay := w.cfg.ESXIStableWindow
	if stableDisplay <= 0 {
		stableDisplay = 90 * time.Second
	}
	if stableDisplay < time.Millisecond {
		stableDisplay = time.Millisecond
	}
	if stableDisplay < time.Second {
		stableDisplay = stableDisplay.Round(time.Millisecond)
	} else {
		stableDisplay = stableDisplay.Round(time.Second)
	}
	_ = w.appendEvent(ctx, job.ID, provisioner.EventLevelInfo,
		fmt.Sprintf("ESXi workflow: power state 'On' stable for %s; assuming install complete", stableDisplay), step)

	if err := w.store.MarkJobStatus(ctx, job.ID, provisioner.JobStatusSucceeded, nil); err != nil {
		w.logf("job %s: failed to mark succeeded: %v", job.ID, err)
	}
	return provisioner.JobStatusSucceeded, nil
}

func (w *Worker) pollESXiPowerState(ctx context.Context, jobID string, rf redfish.Client, deadline time.Time) error {
	minInterval := w.cfg.ESXIPollIntervalStart
	maxInterval := w.cfg.ESXIPollIntervalMax
	stableWindow := w.cfg.ESXIStableWindow
	if minInterval <= 0 {
		minInterval = time.Second
	}
	if maxInterval <= 0 || maxInterval < minInterval {
		maxInterval = minInterval
	}
	if stableWindow <= 0 {
		stableWindow = 90 * time.Second
	}

	interval := minInterval
	stableSince := time.Time{}
	lastState := redfish.PowerStateUnknown
	start := w.now()
	attempts := 0

	for {
		if ctx.Err() != nil {
			err := ctx.Err()
			if attempts == 0 {
				attempts = 1
			}
			w.recordOpEvent(ctx, jobID, opESXIPollPower, opESXIPollPower, attempts, start, err)
			metrics.ObserveProvisioningPhase(opESXIPollPower, w.now().Sub(start))
			return err
		}
		now := w.now()
		if now.After(deadline) {
			err := fmt.Errorf("install deadline exceeded waiting for stable power state (last=%s)", lastState)
			if attempts == 0 {
				attempts = 1
			}
			w.recordOpEvent(ctx, jobID, opESXIPollPower, opESXIPollPower, attempts, start, err)
			metrics.ObserveProvisioningPhase(opESXIPollPower, w.now().Sub(start))
			return err
		}

		attempts++
		state, err := rf.SystemPowerState(ctx)
		resetInterval := false
		if err != nil {
			stableSince = time.Time{}
			resetInterval = true
			lastState = redfish.PowerStateUnknown
		} else {
			if state != lastState {
				w.logf("job %s: ESXi power state %s", jobID, state)
			}
			lastState = state
			switch state {
			case redfish.PowerStateOn:
				if stableSince.IsZero() {
					stableSince = now
				}
				if now.Sub(stableSince) >= stableWindow {
					w.recordOpEvent(ctx, jobID, opESXIPollPower, opESXIPollPower, attempts, start, nil)
					metrics.ObserveProvisioningPhase(opESXIPollPower, w.now().Sub(start))
					return nil
				}
				resetInterval = true
			case redfish.PowerStateOff,
				redfish.PowerStatePoweringOn,
				redfish.PowerStatePoweringOff,
				redfish.PowerStateResetting,
				redfish.PowerStateStandby,
				redfish.PowerStateUnknown:
				stableSince = time.Time{}
				resetInterval = true
			default:
				stableSince = time.Time{}
				resetInterval = true
			}
		}

		sleep := interval
		if sleep > maxInterval {
			sleep = maxInterval
		}

		select {
		case <-ctx.Done():
			err := ctx.Err()
			if attempts == 0 {
				attempts = 1
			}
			w.recordOpEvent(ctx, jobID, opESXIPollPower, opESXIPollPower, attempts, start, err)
			metrics.ObserveProvisioningPhase(opESXIPollPower, w.now().Sub(start))
			return err
		case <-time.After(sleep):
		}

		if resetInterval {
			interval = minInterval
		} else if interval < maxInterval {
			next := interval * 2
			if next > maxInterval {
				interval = maxInterval
			} else {
				interval = next
			}
		}
	}
}

func taskTargetFromRecipe(recipe json.RawMessage) string {
	if len(recipe) == 0 {
		return ""
	}
	var payload struct {
		TaskTarget string `json:"task_target"`
	}
	if err := json.Unmarshal(recipe, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.TaskTarget)
}

// kickstartFromRecipe extracts the ks_cfg string from the job recipe, if present.
// It returns the raw bytes for inclusion in the task ISO at /ks.cfg for ESXi handoff.
func kickstartFromRecipe(recipe json.RawMessage) []byte {
	if len(recipe) == 0 {
		return nil
	}
	var payload struct {
		KSCfg string `json:"ks_cfg"`
	}
	if err := json.Unmarshal(recipe, &payload); err != nil {
		return nil
	}
	s := strings.TrimSpace(payload.KSCfg)
	if s == "" {
		return nil
	}
	return []byte(s)
}
