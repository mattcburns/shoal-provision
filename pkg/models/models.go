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

// ConnectionMethod represents an aggregation connection to an external BMC
type ConnectionMethod struct {
	ID                   string     `json:"id" db:"id"`
	Name                 string     `json:"Name" db:"name"`
	ConnectionMethodType string     `json:"ConnectionMethodType" db:"connection_type"`
	Address              string     `json:"ConnectionMethodVariant.Address" db:"address"`
	Username             string     `json:"-" db:"username"`
	Password             string     `json:"-" db:"password"`
	Enabled              bool       `json:"Enabled" db:"enabled"`
	CreatedAt            time.Time  `json:"CreatedAt" db:"created_at"`
	UpdatedAt            time.Time  `json:"UpdatedAt" db:"updated_at"`
	LastSeen             *time.Time `json:"LastSeen,omitempty" db:"last_seen"`
	// Cached aggregated data (stored as JSON in database)
	AggregatedManagers string `json:"-" db:"aggregated_managers"`
	AggregatedSystems  string `json:"-" db:"aggregated_systems"`
}

// DetailedBMCStatus represents comprehensive information about a BMC
type DetailedBMCStatus struct {
	BMC               BMC                `json:"bmc"`
	SystemInfo        *SystemInfo        `json:"system_info,omitempty"`
	NetworkInterfaces []NetworkInterface `json:"network_interfaces,omitempty"`
	StorageDevices    []StorageDevice    `json:"storage_devices,omitempty"`
	SELEntries        []SELEntry         `json:"sel_entries,omitempty"`
}

// SystemInfo represents basic system information
type SystemInfo struct {
	SerialNumber string `json:"serial_number,omitempty"`
	SKU          string `json:"sku,omitempty"`
	PowerState   string `json:"power_state,omitempty"`
	Model        string `json:"model,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
}

// NetworkInterface represents a network interface
type NetworkInterface struct {
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	MACAddress  string   `json:"mac_address,omitempty"`
	IPAddresses []string `json:"ip_addresses,omitempty"`
}

// StorageDevice represents a storage device
type StorageDevice struct {
	Name          string `json:"name,omitempty"`
	Model         string `json:"model,omitempty"`
	SerialNumber  string `json:"serial_number,omitempty"`
	CapacityBytes int64  `json:"capacity_bytes,omitempty"`
	Status        string `json:"status,omitempty"`
	MediaType     string `json:"media_type,omitempty"`
}

// SELEntry represents a System Event Log entry
type SELEntry struct {
	ID        string `json:"id,omitempty"`
	Message   string `json:"message,omitempty"`
	Severity  string `json:"severity,omitempty"`
	Created   string `json:"created,omitempty"`
	EntryType string `json:"entry_type,omitempty"`
}
