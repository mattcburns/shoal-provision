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

// Package store provides a SQLite-backed persistence layer for the
// provisioner controller, including schema migrations, CRUD operations,
// and leasing helpers for job orchestration.
//
// Schema and behaviors are aligned with the design in:
// - 021_Provisioner_Controller_Service.md (tables, leasing, status)
// - 020_Provisioner_Architecture.md (index and acceptance criteria)
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"shoal/pkg/provisioner"
)

const (
	defaultBusyTimeout = 5 * time.Second

	// settings keys
	schemaVersionKey = "schema_version"
)

var (
	// ErrNotFound indicates no rows matched the query.
	ErrNotFound = errors.New("not found")
)

// Store wraps a SQLite database connection and provides typed accessors.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) a SQLite database at path, applies connection
// pragmas, runs migrations, and returns a ready Store.
func Open(ctx context.Context, path string) (*Store, error) {
	// DSN with pragmas for durability and concurrency.
	// - busy_timeout: backoff on locked database
	// - journal_mode=WAL: better concurrency
	// - foreign_keys=ON: enforce referential integrity
	// - synchronous=NORMAL: reasonable safety/perf tradeoff
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)", path, int(defaultBusyTimeout.Milliseconds()))

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Reasonable pool settings for a single-node embedded DB
	db.SetConnMaxLifetime(0)
	db.SetMaxIdleConns(4)
	db.SetMaxOpenConns(8)

	// Verify connection
	if err := pingContext(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// WithTx executes fn inside a transaction. If fn returns an error,
// the transaction is rolled back; otherwise, it's committed.
func (s *Store) WithTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{
		ReadOnly:  false,
		Isolation: sql.LevelSerializable,
	})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		// In case of panic, make best effort rollback
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// --------------- Migrations ---------------

func (s *Store) migrate(ctx context.Context) error {
	if err := s.ensureSettingsTable(ctx); err != nil {
		return err
	}

	cur, err := s.getSchemaVersion(ctx)
	if err != nil {
		return err
	}

	target := 1 // latest schema version in this file

	// v1: initial schema
	if cur < 1 {
		if err := s.migrateToV1(ctx); err != nil {
			return fmt.Errorf("migrate to v1: %w", err)
		}
		if err := s.setSchemaVersion(ctx, 1); err != nil {
			return err
		}
		cur = 1
	}

	if cur != target {
		// Future migrations go here
	}

	return nil
}

func (s *Store) ensureSettingsTable(ctx context.Context) error {
	ddl := `
CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);`
	_, err := s.db.ExecContext(ctx, ddl)
	return err
}

func (s *Store) getSchemaVersion(ctx context.Context) (int, error) {
	const q = `SELECT value FROM settings WHERE key=?`
	var val string
	err := s.db.QueryRowContext(ctx, q, schemaVersionKey).Scan(&val)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	var v int
	if _, err := fmt.Sscanf(val, "%d", &v); err != nil {
		// If corrupted, force to 0 to allow re-init
		return 0, nil
	}
	return v, nil
}

func (s *Store) setSchemaVersion(ctx context.Context, v int) error {
	const upsert = `
INSERT INTO settings(key, value) VALUES(?, ?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value;`
	_, err := s.db.ExecContext(ctx, upsert, schemaVersionKey, fmt.Sprintf("%d", v))
	if err != nil {
		return fmt.Errorf("set schema version: %w", err)
	}
	return nil
}

func (s *Store) migrateToV1(ctx context.Context) error {
	stmts := []string{
		// servers table
		`CREATE TABLE IF NOT EXISTS servers (
  serial       TEXT PRIMARY KEY,
  bmc_address  TEXT NOT NULL,
  bmc_username TEXT NOT NULL,
  bmc_password TEXT NOT NULL,
  vendor       TEXT NULL,
  last_seen    TIMESTAMP NULL
);`,
		// jobs table
		`CREATE TABLE IF NOT EXISTS jobs (
  id                   TEXT PRIMARY KEY,
  server_serial        TEXT NOT NULL REFERENCES servers(serial) ON DELETE RESTRICT,
  status               TEXT NOT NULL CHECK (status IN ('queued','provisioning','succeeded','failed','complete')),
  failed_step          TEXT NULL,
  recipe_json          TEXT NOT NULL,
  created_at           TIMESTAMP NOT NULL,
  updated_at           TIMESTAMP NOT NULL,
  picked_at            TIMESTAMP NULL,
  worker_id            TEXT NULL,
  lease_expires_at     TIMESTAMP NULL,
  task_iso_path        TEXT NULL,
  maintenance_iso_url  TEXT NOT NULL
);`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_server ON jobs(server_serial);`,

		// job_events table
		`CREATE TABLE IF NOT EXISTS job_events (
  id       INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id   TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  time     TIMESTAMP NOT NULL,
  level    TEXT NOT NULL CHECK (level IN ('info','warn','error')),
  message  TEXT NOT NULL,
  step     TEXT NULL
);`,
		`CREATE INDEX IF NOT EXISTS idx_job_events_job_time ON job_events(job_id, time);`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("execute ddl: %w", err)
		}
	}
	return nil
}

// --------------- Settings helpers ---------------

// SetSetting upserts a key/value in settings.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	const upsert = `
INSERT INTO settings(key, value) VALUES(?, ?)
ON CONFLICT(key) DO UPDATE SET value=excluded.value;`
	_, err := s.db.ExecContext(ctx, upsert, key, value)
	return err
}

// GetSetting returns a value for key or ErrNotFound.
func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	const q = `SELECT value FROM settings WHERE key=?`
	var v string
	if err := s.db.QueryRowContext(ctx, q, key).Scan(&v); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return v, nil
}

// --------------- Servers ---------------

// UpsertServer inserts or updates a server record by serial.
func (s *Store) UpsertServer(ctx context.Context, sv provisioner.Server) error {
	const upsert = `
INSERT INTO servers(serial, bmc_address, bmc_username, bmc_password, vendor, last_seen)
VALUES(?, ?, ?, ?, ?, ?)
ON CONFLICT(serial) DO UPDATE SET
  bmc_address=excluded.bmc_address,
  bmc_username=excluded.bmc_username,
  bmc_password=excluded.bmc_password,
  vendor=excluded.vendor,
  last_seen=excluded.last_seen;`

	var lastSeen any
	if sv.LastSeen != nil {
		lastSeen = sv.LastSeen.UTC()
	} else {
		lastSeen = nil
	}

	_, err := s.db.ExecContext(ctx, upsert,
		sv.Serial, sv.BMCAddress, sv.BMCUser, sv.BMCPass, nullIfEmpty(sv.Vendor), lastSeen)
	if err != nil {
		return fmt.Errorf("upsert server: %w", err)
	}
	return nil
}

// GetServerBySerial retrieves a server by its serial.
func (s *Store) GetServerBySerial(ctx context.Context, serial string) (*provisioner.Server, error) {
	const q = `SELECT serial, bmc_address, bmc_username, bmc_password, vendor, last_seen FROM servers WHERE serial=?`
	var row struct {
		serial, addr, user, pass string
		vendor                   sql.NullString
		lastSeen                 sql.NullTime
	}
	err := s.db.QueryRowContext(ctx, q, serial).Scan(&row.serial, &row.addr, &row.user, &row.pass, &row.vendor, &row.lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get server: %w", err)
	}
	var ls *time.Time
	if row.lastSeen.Valid {
		t := row.lastSeen.Time.UTC()
		ls = &t
	}
	return &provisioner.Server{
		Serial:     row.serial,
		BMCAddress: row.addr,
		BMCUser:    row.user,
		BMCPass:    row.pass,
		Vendor:     fromNullString(row.vendor),
		LastSeen:   ls,
	}, nil
}

// --------------- Jobs ---------------

// InsertJob inserts a new job. The caller must set Job.ID and Job.Recipe.
// Timestamps and initial status are trusted from the model and should
// be aligned with provisioner.NewJob.
func (s *Store) InsertJob(ctx context.Context, job *provisioner.Job) error {
	const ins = `
INSERT INTO jobs (id, server_serial, status, failed_step, recipe_json, created_at, updated_at, picked_at, worker_id, lease_expires_at, task_iso_path, maintenance_iso_url)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);`

	// Prepare nullable fields
	var failedStep, pickedAt, workerID, leaseExpiresAt, taskISOPath any
	if job.FailedStep != nil {
		failedStep = *job.FailedStep
	}
	if job.PickedAt != nil {
		pickedAt = job.PickedAt.UTC()
	}
	if job.WorkerID != nil {
		workerID = *job.WorkerID
	}
	if job.LeaseExpiresAt != nil {
		leaseExpiresAt = job.LeaseExpiresAt.UTC()
	}
	if job.TaskISOPath != nil {
		taskISOPath = *job.TaskISOPath
	}

	_, err := s.db.ExecContext(ctx, ins,
		job.ID, job.ServerSerial, job.Status.String(), failedStep, string(job.Recipe),
		job.CreatedAt.UTC(), job.UpdatedAt.UTC(), pickedAt, workerID, leaseExpiresAt, taskISOPath, job.MaintenanceISOURL)
	if err != nil {
		return fmt.Errorf("insert job: %w", err)
	}
	return nil
}

// GetJobByID retrieves a job by ID.
func (s *Store) GetJobByID(ctx context.Context, id string) (*provisioner.Job, error) {
	const q = `SELECT id, server_serial, status, failed_step, recipe_json, created_at, updated_at, picked_at, worker_id, lease_expires_at, task_iso_path, maintenance_iso_url
FROM jobs WHERE id=?`

	var row struct {
		id, serial, status, recipeJSON, maintURL string
		failedStep                               sql.NullString
		createdAt, updatedAt                     time.Time
		pickedAt                                 sql.NullTime
		workerID                                 sql.NullString
		leaseExpiresAt                           sql.NullTime
		taskISOPath                              sql.NullString
	}
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&row.id, &row.serial, &row.status, &row.failedStep, &row.recipeJSON,
		&row.createdAt, &row.updatedAt, &row.pickedAt, &row.workerID, &row.leaseExpiresAt, &row.taskISOPath, &row.maintURL)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get job: %w", err)
	}

	job := &provisioner.Job{
		ID:                row.id,
		ServerSerial:      row.serial,
		Status:            provisioner.JobStatus(row.status),
		FailedStep:        fromNullStringPtr(row.failedStep),
		Recipe:            []byte(row.recipeJSON),
		CreatedAt:         row.createdAt.UTC(),
		UpdatedAt:         row.updatedAt.UTC(),
		PickedAt:          fromNullTimePtr(row.pickedAt),
		WorkerID:          fromNullStringPtr(row.workerID),
		LeaseExpiresAt:    fromNullTimePtr(row.leaseExpiresAt),
		TaskISOPath:       fromNullStringPtr(row.taskISOPath),
		MaintenanceISOURL: row.maintURL,
	}
	return job, nil
}

// GetActiveProvisioningJobBySerial returns the active provisioning job for a server serial.
// Returns ErrNotFound if no such job exists.
func (s *Store) GetActiveProvisioningJobBySerial(ctx context.Context, serial string) (*provisioner.Job, error) {
	const q = `SELECT id, server_serial, status, failed_step, recipe_json, created_at, updated_at, picked_at, worker_id, lease_expires_at, task_iso_path, maintenance_iso_url
FROM jobs WHERE server_serial=? AND status='provisioning' ORDER BY created_at DESC LIMIT 1`

	var row struct {
		id, serverSerial, status, recipeJSON, maintURL string
		failedStep                                     sql.NullString
		createdAt, updatedAt                           time.Time
		pickedAt                                       sql.NullTime
		workerID                                       sql.NullString
		leaseExpiresAt                                 sql.NullTime
		taskISOPath                                    sql.NullString
	}
	err := s.db.QueryRowContext(ctx, q, serial).Scan(
		&row.id, &row.serverSerial, &row.status, &row.failedStep, &row.recipeJSON,
		&row.createdAt, &row.updatedAt, &row.pickedAt, &row.workerID, &row.leaseExpiresAt, &row.taskISOPath, &row.maintURL)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get active job by serial: %w", err)
	}

	job := &provisioner.Job{
		ID:                row.id,
		ServerSerial:      row.serverSerial,
		Status:            provisioner.JobStatus(row.status),
		FailedStep:        fromNullStringPtr(row.failedStep),
		Recipe:            []byte(row.recipeJSON),
		CreatedAt:         row.createdAt.UTC(),
		UpdatedAt:         row.updatedAt.UTC(),
		PickedAt:          fromNullTimePtr(row.pickedAt),
		WorkerID:          fromNullStringPtr(row.workerID),
		LeaseExpiresAt:    fromNullTimePtr(row.leaseExpiresAt),
		TaskISOPath:       fromNullStringPtr(row.taskISOPath),
		MaintenanceISOURL: row.maintURL,
	}
	return job, nil
}

// UpdateJobTaskISOPath sets the task ISO path for a job.
func (s *Store) UpdateJobTaskISOPath(ctx context.Context, id, path string) error {
	const upd = `UPDATE jobs SET task_iso_path=?, updated_at=? WHERE id=?`
	_, err := s.db.ExecContext(ctx, upd, path, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("update task iso path: %w", err)
	}
	return nil
}

// MarkJobStatus transitions a job to a new status, optionally recording a failed step.
func (s *Store) MarkJobStatus(ctx context.Context, id string, status provisioner.JobStatus, failedStep *string) error {
	if !status.Valid() {
		return fmt.Errorf("invalid status: %s", status)
	}
	const upd = `UPDATE jobs SET status=?, failed_step=?, updated_at=? WHERE id=?`
	var fs any
	if failedStep != nil {
		fs = *failedStep
	}
	_, err := s.db.ExecContext(ctx, upd, status.String(), fs, time.Now().UTC(), id)
	return err
}

// ListJobsByStatus returns jobs matching the provided status ordered by creation time.
func (s *Store) ListJobsByStatus(ctx context.Context, status provisioner.JobStatus) ([]*provisioner.Job, error) {
	if !status.Valid() {
		return nil, fmt.Errorf("invalid status: %s", status)
	}
	const q = `SELECT id, server_serial, status, failed_step, recipe_json, created_at, updated_at, picked_at, worker_id, lease_expires_at, task_iso_path, maintenance_iso_url
FROM jobs WHERE status=? ORDER BY created_at ASC`
	rows, err := s.db.QueryContext(ctx, q, status.String())
	if err != nil {
		return nil, fmt.Errorf("list jobs by status: %w", err)
	}
	defer rows.Close()

	var out []*provisioner.Job
	for rows.Next() {
		var row struct {
			id, serial, status, recipeJSON, maintURL string
			failedStep                               sql.NullString
			createdAt, updatedAt                     time.Time
			pickedAt                                 sql.NullTime
			workerID                                 sql.NullString
			leaseExpiresAt                           sql.NullTime
			taskISOPath                              sql.NullString
		}
		if err := rows.Scan(
			&row.id, &row.serial, &row.status, &row.failedStep, &row.recipeJSON,
			&row.createdAt, &row.updatedAt, &row.pickedAt, &row.workerID, &row.leaseExpiresAt, &row.taskISOPath, &row.maintURL,
		); err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		job := &provisioner.Job{
			ID:                row.id,
			ServerSerial:      row.serial,
			Status:            provisioner.JobStatus(row.status),
			FailedStep:        fromNullStringPtr(row.failedStep),
			Recipe:            []byte(row.recipeJSON),
			CreatedAt:         row.createdAt.UTC(),
			UpdatedAt:         row.updatedAt.UTC(),
			PickedAt:          fromNullTimePtr(row.pickedAt),
			WorkerID:          fromNullStringPtr(row.workerID),
			LeaseExpiresAt:    fromNullTimePtr(row.leaseExpiresAt),
			TaskISOPath:       fromNullStringPtr(row.taskISOPath),
			MaintenanceISOURL: row.maintURL,
		}
		out = append(out, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate jobs: %w", err)
	}
	return out, nil
}

// RequeueProvisioningJob resets a provisioning job back to queued so that the worker
// loop can safely reprocess it (e.g., after controller restart). Returns ErrNotFound
// if the job is not currently in provisioning state.
func (s *Store) RequeueProvisioningJob(ctx context.Context, id string) error {
	const upd = `UPDATE jobs
SET status='queued', failed_step=NULL, worker_id=NULL, picked_at=NULL, lease_expires_at=NULL, task_iso_path=NULL, updated_at=?
WHERE id=? AND status='provisioning'`
	res, err := s.db.ExecContext(ctx, upd, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("requeue provisioning job: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// --------------- Leasing helpers ---------------

// AcquireQueuedJob tries to atomically lease the next queued job, transitioning it
// to provisioning and assigning worker/lease timers. Returns ErrNotFound if none.
func (s *Store) AcquireQueuedJob(ctx context.Context, workerID string, leaseTTL time.Duration) (*provisioner.Job, error) {
	now := time.Now().UTC()
	leaseUntil := now.Add(leaseTTL)

	var acquiredJob *provisioner.Job
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		// Select a candidate
		const sel = `SELECT id FROM jobs WHERE status='queued' ORDER BY created_at ASC LIMIT 1`
		var id string
		err := tx.QueryRowContext(ctx, sel).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("select queued job: %w", err)
		}

		// Try to acquire atomically
		const upd = `UPDATE jobs
SET status='provisioning', worker_id=?, picked_at=?, lease_expires_at=?, updated_at=?
WHERE id=? AND status='queued'`
		res, err := tx.ExecContext(ctx, upd, workerID, now, leaseUntil, now, id)
		if err != nil {
			return fmt.Errorf("acquire queued job: %w", err)
		}
		affected, _ := res.RowsAffected()
		if affected != 1 {
			return ErrNotFound
		}

		// Return the job
		j, err := s.getJobByIDTx(ctx, tx, id)
		if err != nil {
			return err
		}
		acquiredJob = j
		return nil
	})
	if err != nil {
		return nil, err
	}
	return acquiredJob, nil
}

// ExtendLease extends the lease for a provisioning job, asserting worker ownership.
func (s *Store) ExtendLease(ctx context.Context, jobID, workerID string, leaseTTL time.Duration) (bool, error) {
	now := time.Now().UTC()
	leaseUntil := now.Add(leaseTTL)
	const upd = `UPDATE jobs
SET lease_expires_at=?, updated_at=?
WHERE id=? AND status='provisioning' AND worker_id=?`
	res, err := s.db.ExecContext(ctx, upd, leaseUntil, now, jobID, workerID)
	if err != nil {
		return false, fmt.Errorf("extend lease: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// StealExpiredLease transfers ownership of a provisioning job whose lease expired.
func (s *Store) StealExpiredLease(ctx context.Context, jobID, newWorkerID string, leaseTTL time.Duration) (bool, error) {
	now := time.Now().UTC()
	leaseUntil := now.Add(leaseTTL)
	const upd = `UPDATE jobs
SET worker_id=?, picked_at=?, lease_expires_at=?, updated_at=?
WHERE id=? AND status='provisioning' AND lease_expires_at IS NOT NULL AND lease_expires_at < ?`
	res, err := s.db.ExecContext(ctx, upd, newWorkerID, now, leaseUntil, now, jobID, now)
	if err != nil {
		return false, fmt.Errorf("steal lease: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// --------------- Job events ---------------

// AppendJobEvent inserts a new event row for a job.
func (s *Store) AppendJobEvent(ctx context.Context, ev provisioner.JobEvent) error {
	const ins = `INSERT INTO job_events(job_id, time, level, message, step) VALUES(?, ?, ?, ?, ?)`
	var step any
	if ev.Step != nil {
		step = *ev.Step
	}
	_, err := s.db.ExecContext(ctx, ins, ev.JobID, ev.Time.UTC(), ev.Level.String(), ev.Message, step)
	if err != nil {
		return fmt.Errorf("insert job event: %w", err)
	}
	return nil
}

// ListJobEvents fetches events for a job ordered by time ascending.
// If limit <= 0, returns all.
func (s *Store) ListJobEvents(ctx context.Context, jobID string, limit int) ([]provisioner.JobEvent, error) {
	q := `SELECT id, job_id, time, level, message, step FROM job_events WHERE job_id=? ORDER BY time ASC`
	if limit > 0 {
		q = q + fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.QueryContext(ctx, q, jobID)
	if err != nil {
		return nil, fmt.Errorf("query job events: %w", err)
	}
	defer rows.Close()

	var out []provisioner.JobEvent
	for rows.Next() {
		var (
			id       int64
			rowJobID string
			t        time.Time
			level    string
			msg      string
			step     sql.NullString
		)
		if err := rows.Scan(&id, &rowJobID, &t, &level, &msg, &step); err != nil {
			return nil, fmt.Errorf("scan job event: %w", err)
		}
		out = append(out, provisioner.JobEvent{
			ID:      id,
			JobID:   rowJobID,
			Time:    t.UTC(),
			Level:   provisioner.EventLevel(level),
			Message: msg,
			Step:    fromNullStringPtr(step),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate job events: %w", err)
	}
	return out, nil
}

// --------------- Internal helpers ---------------

func (s *Store) getJobByIDTx(ctx context.Context, tx *sql.Tx, id string) (*provisioner.Job, error) {
	const q = `SELECT id, server_serial, status, failed_step, recipe_json, created_at, updated_at, picked_at, worker_id, lease_expires_at, task_iso_path, maintenance_iso_url
FROM jobs WHERE id=?`
	var row struct {
		id, serial, status, recipeJSON, maintURL string
		failedStep                               sql.NullString
		createdAt, updatedAt                     time.Time
		pickedAt                                 sql.NullTime
		workerID                                 sql.NullString
		leaseExpiresAt                           sql.NullTime
		taskISOPath                              sql.NullString
	}
	err := tx.QueryRowContext(ctx, q, id).Scan(
		&row.id, &row.serial, &row.status, &row.failedStep, &row.recipeJSON,
		&row.createdAt, &row.updatedAt, &row.pickedAt, &row.workerID, &row.leaseExpiresAt, &row.taskISOPath, &row.maintURL)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get job tx: %w", err)
	}
	return &provisioner.Job{
		ID:                row.id,
		ServerSerial:      row.serial,
		Status:            provisioner.JobStatus(row.status),
		FailedStep:        fromNullStringPtr(row.failedStep),
		Recipe:            []byte(row.recipeJSON),
		CreatedAt:         row.createdAt.UTC(),
		UpdatedAt:         row.updatedAt.UTC(),
		PickedAt:          fromNullTimePtr(row.pickedAt),
		WorkerID:          fromNullStringPtr(row.workerID),
		LeaseExpiresAt:    fromNullTimePtr(row.leaseExpiresAt),
		TaskISOPath:       fromNullStringPtr(row.taskISOPath),
		MaintenanceISOURL: row.maintURL,
	}, nil
}

func pingContext(ctx context.Context, db *sql.DB) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return db.PingContext(ctx)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func fromNullString(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

func fromNullStringPtr(ns sql.NullString) *string {
	if ns.Valid {
		v := ns.String
		return &v
	}
	return nil
}

func fromNullTimePtr(nt sql.NullTime) *time.Time {
	if nt.Valid {
		t := nt.Time.UTC()
		return &t
	}
	return nil
}
