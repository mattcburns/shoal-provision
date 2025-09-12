package models

import (
	"time"
)

// BMC represents a managed Baseboard Management Controller
type BMC struct {
	ID          int64      `json:"id" db:"id"`
	Name        string     `json:"name" db:"name"`
	Address     string     `json:"address" db:"address"`
	Username    string     `json:"username" db:"username"`
	Password    string     `json:"-" db:"password"` // Never expose password in JSON
	Description string     `json:"description" db:"description"`
	Enabled     bool       `json:"enabled" db:"enabled"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
	LastSeen    *time.Time `json:"last_seen,omitempty" db:"last_seen"`
}

// Session represents an authentication session for the aggregator API
type Session struct {
	ID        string    `json:"id" db:"id"`
	UserID    string    `json:"user_id" db:"user_id"`
	Token     string    `json:"token" db:"token"`
	ExpiresAt time.Time `json:"expires_at" db:"expires_at"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// User represents a user of the aggregator service
type User struct {
	ID           string    `json:"id" db:"id"`
	Username     string    `json:"username" db:"username"`
	PasswordHash string    `json:"-" db:"password_hash"` // Never expose password hash
	Role         string    `json:"role" db:"role"`
	Enabled      bool      `json:"enabled" db:"enabled"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// User roles
const (
	RoleAdmin    = "admin"    // Full access to everything
	RoleOperator = "operator" // Can manage BMCs but not users
	RoleViewer   = "viewer"   // Read-only access
)

// PowerAction represents a power control action
type PowerAction string

const (
	PowerActionOn               PowerAction = "On"
	PowerActionForceOff         PowerAction = "ForceOff"
	PowerActionGracefulShutdown PowerAction = "GracefulShutdown"
	PowerActionForceRestart     PowerAction = "ForceRestart"
	PowerActionGracefulRestart  PowerAction = "GracefulRestart"
	PowerActionNmi              PowerAction = "Nmi"
)

// PowerRequest represents a power control request
type PowerRequest struct {
	ResetType PowerAction `json:"ResetType"`
}
