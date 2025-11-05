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

// Package provisioner contains shared data models and constants used by
// the provisioner controller, dispatcher, and tests. These types mirror
// the conceptual models defined in the 020/021 design documents.
package provisioner

import (
	"encoding/json"
	"time"
)

// JobStatus is the lifecycle state of a provisioning job.
// States must remain compatible with the design documents:
// queued → provisioning → {succeeded|failed} → complete.
type JobStatus string

const (
	JobStatusQueued       JobStatus = "queued"
	JobStatusProvisioning JobStatus = "provisioning"
	JobStatusSucceeded    JobStatus = "succeeded"
	JobStatusFailed       JobStatus = "failed"
	JobStatusComplete     JobStatus = "complete"
)

// Valid reports whether the status is one of the allowed states.
func (s JobStatus) Valid() bool {
	switch s {
	case JobStatusQueued, JobStatusProvisioning, JobStatusSucceeded, JobStatusFailed, JobStatusComplete:
		return true
	default:
		return false
	}
}

// IsTerminal reports whether the status is a terminal state
// (succeeded, failed, or complete).
func (s JobStatus) IsTerminal() bool {
	switch s {
	case JobStatusSucceeded, JobStatusFailed, JobStatusComplete:
		return true
	default:
		return false
	}
}

// String returns the string value of the JobStatus.
func (s JobStatus) String() string { return string(s) }

// EventLevel represents the severity of a job event log entry.
type EventLevel string

const (
	EventLevelInfo  EventLevel = "info"
	EventLevelWarn  EventLevel = "warn"
	EventLevelError EventLevel = "error"
)

// String returns the string value of the EventLevel.
func (l EventLevel) String() string { return string(l) }

// Server represents a managed bare-metal server's BMC access details.
// These fields align with the "servers" table in the persistence model.
type Server struct {
	Serial     string     `json:"serial" db:"serial"`
	BMCAddress string     `json:"bmc_address" db:"bmc_address"`
	BMCUser    string     `json:"bmc_username" db:"bmc_username"`
	BMCPass    string     `json:"bmc_password" db:"bmc_password"` // NOTE: handle securely; do not log
	Vendor     string     `json:"vendor,omitempty" db:"vendor"`
	LastSeen   *time.Time `json:"last_seen,omitempty" db:"last_seen"`
}

// Job represents a single provisioning request and its lifecycle.
// The controller validates the recipe at creation-time and then treats
// it as opaque JSON, persisting it for the dispatcher/workers to use.
type Job struct {
	ID                string          `json:"job_id" db:"id"`
	ServerSerial      string          `json:"server_serial" db:"server_serial"`
	Status            JobStatus       `json:"status" db:"status"`
	FailedStep        *string         `json:"failed_step,omitempty" db:"failed_step"`
	Recipe            json.RawMessage `json:"recipe" db:"recipe_json"`
	CreatedAt         time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at" db:"updated_at"`
	PickedAt          *time.Time      `json:"picked_at,omitempty" db:"picked_at"`
	WorkerID          *string         `json:"worker_id,omitempty" db:"worker_id"`
	LeaseExpiresAt    *time.Time      `json:"lease_expires_at,omitempty" db:"lease_expires_at"`
	TaskISOPath       *string         `json:"task_iso_path,omitempty" db:"task_iso_path"`
	MaintenanceISOURL string          `json:"maintenance_iso_url" db:"maintenance_iso_url"`
}

// JobEvent is an append-only event stream for a Job.
// Used for user-visible progress and debugging observability.
type JobEvent struct {
	ID      int64      `json:"id" db:"id"`
	JobID   string     `json:"job_id" db:"job_id"`
	Time    time.Time  `json:"time" db:"time"`
	Level   EventLevel `json:"level" db:"level"`
	Message string     `json:"message" db:"message"`
	Step    *string    `json:"step,omitempty" db:"step"`
}

// NewJob constructs a new Job with initial queued status and timestamps.
// Caller should assign a unique ID (e.g., uuid) before persistence.
func NewJob(serverSerial string, recipe json.RawMessage, maintenanceISOURL string) Job {
	now := time.Now().UTC()
	return Job{
		ID:                "",
		ServerSerial:      serverSerial,
		Status:            JobStatusQueued,
		FailedStep:        nil,
		Recipe:            recipe,
		CreatedAt:         now,
		UpdatedAt:         now,
		PickedAt:          nil,
		WorkerID:          nil,
		LeaseExpiresAt:    nil,
		TaskISOPath:       nil,
		MaintenanceISOURL: maintenanceISOURL,
	}
}
