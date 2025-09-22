// Shoal is a Redfish aggregator service.
// Copyright (C) 2025  Matthew Burns
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

package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"shoal/pkg/crypto"
	"shoal/pkg/models"

	_ "modernc.org/sqlite"
)

// DB wraps the database connection and provides methods for data access
type DB struct {
	conn      *sql.DB
	encryptor *crypto.Encryptor
}

// New creates a new database connection without encryption
func New(dbPath string) (*DB, error) {
	return NewWithEncryption(dbPath, "")
}

// NewWithEncryption creates a new database connection with optional encryption
func NewWithEncryption(dbPath string, encryptionKey string) (*DB, error) {
	conn, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Initialize encryptor if key is provided
	var encryptor *crypto.Encryptor
	if encryptionKey != "" {
		encryptor, err = crypto.NewEncryptor(encryptionKey)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize encryptor: %w", err)
		}
		slog.Info("Password encryption enabled")
	} else {
		slog.Warn("Password encryption disabled - passwords will be stored in plaintext")
	}

	return &DB{
		conn:      conn,
		encryptor: encryptor,
	}, nil
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.conn.Close()
}

// Migrate runs database migrations
func (db *DB) Migrate(ctx context.Context) error {
	slog.Info("Running database migrations")

	migrations := []string{
		`CREATE TABLE IF NOT EXISTS bmcs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			address TEXT NOT NULL,
			username TEXT NOT NULL,
			password TEXT NOT NULL,
			description TEXT,
			enabled BOOLEAN DEFAULT true,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_seen DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			role TEXT DEFAULT 'user',
			enabled BOOLEAN DEFAULT true,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			token TEXT NOT NULL UNIQUE,
			expires_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_bmcs_enabled ON bmcs(enabled)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at)`,
		`CREATE TABLE IF NOT EXISTS connection_methods (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			connection_type TEXT NOT NULL DEFAULT 'Redfish',
			address TEXT NOT NULL,
			username TEXT NOT NULL,
			password TEXT NOT NULL,
			enabled BOOLEAN DEFAULT true,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_seen DATETIME,
			aggregated_managers TEXT,
			aggregated_systems TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_connection_methods_enabled ON connection_methods(enabled)`,
		// Settings persistence
		`CREATE TABLE IF NOT EXISTS settings_descriptors (
			id TEXT PRIMARY KEY,
			bmc_id INTEGER NOT NULL,
			resource_path TEXT NOT NULL,
			attribute TEXT NOT NULL,
			display_name TEXT,
			description TEXT,
			type TEXT NOT NULL,
			enum_values TEXT,
			min REAL,
			max REAL,
			pattern TEXT,
			units TEXT,
			read_only BOOLEAN DEFAULT false,
			oem BOOLEAN DEFAULT false,
			oem_vendor TEXT,
			apply_times TEXT,
			action_target TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (bmc_id) REFERENCES bmcs(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS settings_values (
			descriptor_id TEXT PRIMARY KEY,
			current_value TEXT,
			source_timestamp TEXT,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (descriptor_id) REFERENCES settings_descriptors(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_settings_by_bmc_resource ON settings_descriptors(bmc_id, resource_path)`,
		`CREATE INDEX IF NOT EXISTS idx_settings_by_bmc ON settings_descriptors(bmc_id)`,
		// Profiles (005)
		`CREATE TABLE IF NOT EXISTS profiles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			description TEXT,
			created_by TEXT,
			hardware_selector TEXT,
			firmware_ranges_json TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS profile_versions (
			id TEXT PRIMARY KEY,
			profile_id TEXT NOT NULL,
			version INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			notes TEXT,
			FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE CASCADE,
			UNIQUE(profile_id, version)
		)`,
		`CREATE TABLE IF NOT EXISTS profile_entries (
			id TEXT PRIMARY KEY,
			profile_version_id TEXT NOT NULL,
			resource_path TEXT NOT NULL,
			attribute TEXT NOT NULL,
			desired_value_json TEXT,
			apply_time_preference TEXT,
			oem_vendor TEXT,
			FOREIGN KEY (profile_version_id) REFERENCES profile_versions(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_profile_entries_by_version ON profile_entries(profile_version_id)`,
		`CREATE TABLE IF NOT EXISTS profile_assignments (
			id TEXT PRIMARY KEY,
			profile_id TEXT NOT NULL,
			version INTEGER NOT NULL,
			target_type TEXT NOT NULL,
			target_value TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_profile_assignments_profile ON profile_assignments(profile_id, version)`,
	}

	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, migration := range migrations {
		if _, err := tx.ExecContext(ctx, migration); err != nil {
			return fmt.Errorf("failed to execute migration: %w", err)
		}
	}

	return tx.Commit()
}

// Settings operations

// UpsertSettingDescriptors stores or updates descriptors and current values for a BMC
func (db *DB) UpsertSettingDescriptors(ctx context.Context, bmcName string, descs []models.SettingDescriptor) error {
	if len(descs) == 0 {
		return nil
	}

	bmc, err := db.GetBMCByName(ctx, bmcName)
	if err != nil {
		return fmt.Errorf("failed to get BMC by name: %w", err)
	}
	if bmc == nil {
		return fmt.Errorf("BMC not found: %s", bmcName)
	}

	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback()

	descStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO settings_descriptors (
			id, bmc_id, resource_path, attribute, display_name, description, type,
			enum_values, min, max, pattern, units, read_only, oem, oem_vendor,
			apply_times, action_target, updated_at
		) VALUES (
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP
		)
		ON CONFLICT(id) DO UPDATE SET
			bmc_id=excluded.bmc_id,
			resource_path=excluded.resource_path,
			attribute=excluded.attribute,
			display_name=excluded.display_name,
			description=excluded.description,
			type=excluded.type,
			enum_values=excluded.enum_values,
			min=excluded.min,
			max=excluded.max,
			pattern=excluded.pattern,
			units=excluded.units,
			read_only=excluded.read_only,
			oem=excluded.oem,
			oem_vendor=excluded.oem_vendor,
			apply_times=excluded.apply_times,
			action_target=excluded.action_target,
			updated_at=CURRENT_TIMESTAMP
	`)
	if err != nil {
		return fmt.Errorf("prepare descriptor stmt failed: %w", err)
	}
	defer descStmt.Close()

	valStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO settings_values (descriptor_id, current_value, source_timestamp, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(descriptor_id) DO UPDATE SET
			current_value=excluded.current_value,
			source_timestamp=excluded.source_timestamp,
			updated_at=CURRENT_TIMESTAMP
	`)
	if err != nil {
		return fmt.Errorf("prepare values stmt failed: %w", err)
	}
	defer valStmt.Close()

	for _, d := range descs {
		// Marshal arrays and current value to JSON strings for storage
		enumJSON := ""
		if len(d.EnumValues) > 0 {
			if b, err := json.Marshal(d.EnumValues); err == nil {
				enumJSON = string(b)
			}
		}
		applyJSON := ""
		if len(d.ApplyTimes) > 0 {
			if b, err := json.Marshal(d.ApplyTimes); err == nil {
				applyJSON = string(b)
			}
		}
		var minPtr, maxPtr interface{}
		if d.Min != nil {
			minPtr = *d.Min
		} else {
			minPtr = nil
		}
		if d.Max != nil {
			maxPtr = *d.Max
		} else {
			maxPtr = nil
		}

		if _, err := descStmt.ExecContext(ctx,
			d.ID, bmc.ID, d.ResourcePath, d.Attribute, d.DisplayName, d.Description, d.Type,
			enumJSON, minPtr, maxPtr, d.Pattern, d.Units, d.ReadOnly, d.OEM, d.OEMVendor,
			applyJSON, d.ActionTarget,
		); err != nil {
			return fmt.Errorf("upsert descriptor failed: %w", err)
		}

		// Value row
		var valueJSON string
		if d.CurrentValue != nil {
			if b, err := json.Marshal(d.CurrentValue); err == nil {
				valueJSON = string(b)
			}
		}
		if _, err := valStmt.ExecContext(ctx, d.ID, valueJSON, d.SourceTimeISO); err != nil {
			return fmt.Errorf("upsert value failed: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit failed: %w", err)
	}
	return nil
}

// GetSettingsDescriptors returns descriptors (with latest values) for a BMC, optionally filtered by resource path substring
func (db *DB) GetSettingsDescriptors(ctx context.Context, bmcName, resourceFilter string) ([]models.SettingDescriptor, error) {
	bmc, err := db.GetBMCByName(ctx, bmcName)
	if err != nil {
		return nil, fmt.Errorf("failed to get BMC by name: %w", err)
	}
	if bmc == nil {
		return nil, nil
	}

	query := `
		SELECT d.id, d.resource_path, d.attribute, d.display_name, d.description, d.type,
			   d.enum_values, d.min, d.max, d.pattern, d.units, d.read_only, d.oem, d.oem_vendor,
			   d.apply_times, d.action_target, v.current_value, v.source_timestamp
		FROM settings_descriptors d
		LEFT JOIN settings_values v ON v.descriptor_id = d.id
		WHERE d.bmc_id = ? AND (? = '' OR d.resource_path LIKE '%' || ? || '%')
		ORDER BY d.resource_path, d.attribute`

	rows, err := db.conn.QueryContext(ctx, query, bmc.ID, resourceFilter, resourceFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to query settings descriptors: %w", err)
	}
	defer rows.Close()

	var result []models.SettingDescriptor
	for rows.Next() {
		var (
			id, resourcePath, attribute, displayName, description, typ, enumJSON, pattern, units, oemVendor, applyJSON, actionTarget string
			readOnly, oem                                                                                                            bool
			minNull, maxNull                                                                                                         sql.NullFloat64
			curValJSON, sourceTS                                                                                                     sql.NullString
		)
		if err := rows.Scan(&id, &resourcePath, &attribute, &displayName, &description, &typ,
			&enumJSON, &minNull, &maxNull, &pattern, &units, &readOnly, &oem, &oemVendor,
			&applyJSON, &actionTarget, &curValJSON, &sourceTS); err != nil {
			return nil, fmt.Errorf("failed to scan settings descriptor: %w", err)
		}

		var enumVals []string
		if enumJSON != "" {
			_ = json.Unmarshal([]byte(enumJSON), &enumVals)
		}
		var applyVals []string
		if applyJSON != "" {
			_ = json.Unmarshal([]byte(applyJSON), &applyVals)
		}
		var minPtr, maxPtr *float64
		if minNull.Valid {
			v := minNull.Float64
			minPtr = &v
		}
		if maxNull.Valid {
			v := maxNull.Float64
			maxPtr = &v
		}

		var current interface{}
		if curValJSON.Valid && curValJSON.String != "" {
			_ = json.Unmarshal([]byte(curValJSON.String), &current)
		}
		source := ""
		if sourceTS.Valid {
			source = sourceTS.String
		}

		result = append(result, models.SettingDescriptor{
			ID:            id,
			BMCName:       bmcName,
			ResourcePath:  resourcePath,
			Attribute:     attribute,
			DisplayName:   displayName,
			Description:   description,
			Type:          typ,
			EnumValues:    enumVals,
			Min:           minPtr,
			Max:           maxPtr,
			Pattern:       pattern,
			Units:         units,
			ReadOnly:      readOnly,
			OEM:           oem,
			OEMVendor:     oemVendor,
			ApplyTimes:    applyVals,
			ActionTarget:  actionTarget,
			CurrentValue:  current,
			SourceTimeISO: source,
		})
	}
	return result, rows.Err()
}

// GetSettingDescriptor returns a single descriptor by id for a BMC
func (db *DB) GetSettingDescriptor(ctx context.Context, bmcName, descriptorID string) (*models.SettingDescriptor, error) {
	bmc, err := db.GetBMCByName(ctx, bmcName)
	if err != nil {
		return nil, fmt.Errorf("failed to get BMC by name: %w", err)
	}
	if bmc == nil {
		return nil, nil
	}

	query := `
		SELECT d.id, d.resource_path, d.attribute, d.display_name, d.description, d.type,
			   d.enum_values, d.min, d.max, d.pattern, d.units, d.read_only, d.oem, d.oem_vendor,
			   d.apply_times, d.action_target, v.current_value, v.source_timestamp
		FROM settings_descriptors d
		LEFT JOIN settings_values v ON v.descriptor_id = d.id
		WHERE d.bmc_id = ? AND d.id = ?`

	var (
		id, resourcePath, attribute, displayName, description, typ, enumJSON, pattern, units, oemVendor, applyJSON, actionTarget string
		readOnly, oem                                                                                                            bool
		minNull, maxNull                                                                                                         sql.NullFloat64
		curValJSON, sourceTS                                                                                                     sql.NullString
	)
	err = db.conn.QueryRowContext(ctx, query, bmc.ID, descriptorID).Scan(&id, &resourcePath, &attribute, &displayName, &description, &typ,
		&enumJSON, &minNull, &maxNull, &pattern, &units, &readOnly, &oem, &oemVendor,
		&applyJSON, &actionTarget, &curValJSON, &sourceTS)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get settings descriptor: %w", err)
	}

	var enumVals []string
	if enumJSON != "" {
		_ = json.Unmarshal([]byte(enumJSON), &enumVals)
	}
	var applyVals []string
	if applyJSON != "" {
		_ = json.Unmarshal([]byte(applyJSON), &applyVals)
	}
	var minPtr, maxPtr *float64
	if minNull.Valid {
		v := minNull.Float64
		minPtr = &v
	}
	if maxNull.Valid {
		v := maxNull.Float64
		maxPtr = &v
	}
	var current interface{}
	if curValJSON.Valid && curValJSON.String != "" {
		_ = json.Unmarshal([]byte(curValJSON.String), &current)
	}
	source := ""
	if sourceTS.Valid {
		source = sourceTS.String
	}

	desc := &models.SettingDescriptor{
		ID:            id,
		BMCName:       bmcName,
		ResourcePath:  resourcePath,
		Attribute:     attribute,
		DisplayName:   displayName,
		Description:   description,
		Type:          typ,
		EnumValues:    enumVals,
		Min:           minPtr,
		Max:           maxPtr,
		Pattern:       pattern,
		Units:         units,
		ReadOnly:      readOnly,
		OEM:           oem,
		OEMVendor:     oemVendor,
		ApplyTimes:    applyVals,
		ActionTarget:  actionTarget,
		CurrentValue:  current,
		SourceTimeISO: source,
	}
	return desc, nil
}

// BMC operations

// GetBMCs returns all BMCs from the database
func (db *DB) GetBMCs(ctx context.Context) ([]models.BMC, error) {
	query := `SELECT id, name, address, username, password, description, enabled, created_at, updated_at, last_seen FROM bmcs ORDER BY name`

	rows, err := db.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query BMCs: %w", err)
	}
	defer rows.Close()

	var bmcs []models.BMC
	for rows.Next() {
		var bmc models.BMC
		err := rows.Scan(&bmc.ID, &bmc.Name, &bmc.Address, &bmc.Username, &bmc.Password,
			&bmc.Description, &bmc.Enabled, &bmc.CreatedAt, &bmc.UpdatedAt, &bmc.LastSeen)
		if err != nil {
			return nil, fmt.Errorf("failed to scan BMC: %w", err)
		}

		// Decrypt password if encryptor is available
		if db.encryptor != nil && crypto.IsEncrypted(bmc.Password) {
			decrypted, err := db.encryptor.Decrypt(bmc.Password)
			if err != nil {
				slog.Error("Failed to decrypt BMC password", "bmc", bmc.Name, "error", err)
				// Keep encrypted password if decryption fails
			} else {
				bmc.Password = decrypted
			}
		}

		bmcs = append(bmcs, bmc)
	}

	return bmcs, rows.Err()
}

// GetBMC returns a single BMC by ID
func (db *DB) GetBMC(ctx context.Context, id int64) (*models.BMC, error) {
	query := `SELECT id, name, address, username, password, description, enabled, created_at, updated_at, last_seen FROM bmcs WHERE id = ?`

	var bmc models.BMC
	err := db.conn.QueryRowContext(ctx, query, id).Scan(
		&bmc.ID, &bmc.Name, &bmc.Address, &bmc.Username, &bmc.Password,
		&bmc.Description, &bmc.Enabled, &bmc.CreatedAt, &bmc.UpdatedAt, &bmc.LastSeen)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get BMC: %w", err)
	}

	// Decrypt password if encryptor is available
	if db.encryptor != nil && crypto.IsEncrypted(bmc.Password) {
		decrypted, err := db.encryptor.Decrypt(bmc.Password)
		if err != nil {
			slog.Error("Failed to decrypt BMC password", "bmc", bmc.Name, "error", err)
			// Keep encrypted password if decryption fails
		} else {
			bmc.Password = decrypted
		}
	}

	return &bmc, nil
}

// GetBMCByName returns a single BMC by name
func (db *DB) GetBMCByName(ctx context.Context, name string) (*models.BMC, error) {
	query := `SELECT id, name, address, username, password, description, enabled, created_at, updated_at, last_seen FROM bmcs WHERE name = ?`

	var bmc models.BMC
	err := db.conn.QueryRowContext(ctx, query, name).Scan(
		&bmc.ID, &bmc.Name, &bmc.Address, &bmc.Username, &bmc.Password,
		&bmc.Description, &bmc.Enabled, &bmc.CreatedAt, &bmc.UpdatedAt, &bmc.LastSeen)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get BMC by name: %w", err)
	}

	// Decrypt password if encryptor is available
	if db.encryptor != nil && crypto.IsEncrypted(bmc.Password) {
		decrypted, err := db.encryptor.Decrypt(bmc.Password)
		if err != nil {
			slog.Error("Failed to decrypt BMC password", "bmc", bmc.Name, "error", err)
			// Keep encrypted password if decryption fails
		} else {
			bmc.Password = decrypted
		}
	}

	return &bmc, nil
}

// CreateBMC creates a new BMC
func (db *DB) CreateBMC(ctx context.Context, bmc *models.BMC) error {
	query := `INSERT INTO bmcs (name, address, username, password, description, enabled) VALUES (?, ?, ?, ?, ?, ?)`

	// Encrypt password if encryptor is available
	password := bmc.Password
	if db.encryptor != nil && password != "" {
		encrypted, err := db.encryptor.Encrypt(password)
		if err != nil {
			return fmt.Errorf("failed to encrypt password: %w", err)
		}
		password = encrypted
	}

	result, err := db.conn.ExecContext(ctx, query, bmc.Name, bmc.Address, bmc.Username, password, bmc.Description, bmc.Enabled)
	if err != nil {
		return fmt.Errorf("failed to create BMC: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get last insert ID: %w", err)
	}

	bmc.ID = id
	bmc.CreatedAt = time.Now()
	bmc.UpdatedAt = time.Now()

	return nil
}

// UpdateBMC updates an existing BMC
func (db *DB) UpdateBMC(ctx context.Context, bmc *models.BMC) error {
	query := `UPDATE bmcs SET name = ?, address = ?, username = ?, password = ?, description = ?, enabled = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`

	// Encrypt password if encryptor is available
	password := bmc.Password
	if db.encryptor != nil && password != "" {
		// Only encrypt if it's not already encrypted
		if !crypto.IsEncrypted(password) {
			encrypted, err := db.encryptor.Encrypt(password)
			if err != nil {
				return fmt.Errorf("failed to encrypt password: %w", err)
			}
			password = encrypted
		}
	}

	_, err := db.conn.ExecContext(ctx, query, bmc.Name, bmc.Address, bmc.Username, password, bmc.Description, bmc.Enabled, bmc.ID)
	if err != nil {
		return fmt.Errorf("failed to update BMC: %w", err)
	}

	return nil
}

// DeleteBMC deletes a BMC by ID
func (db *DB) DeleteBMC(ctx context.Context, id int64) error {
	query := `DELETE FROM bmcs WHERE id = ?`

	_, err := db.conn.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete BMC: %w", err)
	}

	return nil
}

// UpdateBMCLastSeen updates the last_seen timestamp for a BMC
func (db *DB) UpdateBMCLastSeen(ctx context.Context, id int64) error {
	query := `UPDATE bmcs SET last_seen = CURRENT_TIMESTAMP WHERE id = ?`

	_, err := db.conn.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to update BMC last seen: %w", err)
	}

	return nil
}

// Session operations

// CreateSession creates a new session
func (db *DB) CreateSession(ctx context.Context, session *models.Session) error {
	query := `INSERT INTO sessions (id, user_id, token, expires_at) VALUES (?, ?, ?, ?)`

	_, err := db.conn.ExecContext(ctx, query, session.ID, session.UserID, session.Token, session.ExpiresAt)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	return nil
}

// GetSessionByToken returns a session by token
func (db *DB) GetSessionByToken(ctx context.Context, token string) (*models.Session, error) {
	query := `SELECT id, user_id, token, expires_at, created_at FROM sessions WHERE token = ? AND expires_at > ?`

	var session models.Session
	err := db.conn.QueryRowContext(ctx, query, token, time.Now()).Scan(
		&session.ID, &session.UserID, &session.Token, &session.ExpiresAt, &session.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	return &session, nil
}

// DeleteSession deletes a session
func (db *DB) DeleteSession(ctx context.Context, token string) error {
	query := `DELETE FROM sessions WHERE token = ?`

	_, err := db.conn.ExecContext(ctx, query, token)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	return nil
}

// CleanupExpiredSessions removes expired sessions
func (db *DB) CleanupExpiredSessions(ctx context.Context) error {
	query := `DELETE FROM sessions WHERE expires_at <= ?`

	_, err := db.conn.ExecContext(ctx, query, time.Now())
	if err != nil {
		return fmt.Errorf("failed to cleanup expired sessions: %w", err)
	}

	return nil
}

// GetSession returns a session by ID (only active sessions)
func (db *DB) GetSession(ctx context.Context, id string) (*models.Session, error) {
	query := `SELECT id, user_id, token, expires_at, created_at FROM sessions WHERE id = ? AND expires_at > ?`

	var session models.Session
	err := db.conn.QueryRowContext(ctx, query, id, time.Now()).Scan(
		&session.ID, &session.UserID, &session.Token, &session.ExpiresAt, &session.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session by id: %w", err)
	}
	return &session, nil
}

// GetSessions returns all active sessions
func (db *DB) GetSessions(ctx context.Context) ([]models.Session, error) {
	query := `SELECT id, user_id, token, expires_at, created_at FROM sessions WHERE expires_at > ? ORDER BY created_at DESC`

	rows, err := db.conn.QueryContext(ctx, query, time.Now())
	if err != nil {
		return nil, fmt.Errorf("failed to query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []models.Session
	for rows.Next() {
		var s models.Session
		if err := rows.Scan(&s.ID, &s.UserID, &s.Token, &s.ExpiresAt, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// DeleteSessionByID deletes a session by ID
func (db *DB) DeleteSessionByID(ctx context.Context, id string) error {
	query := `DELETE FROM sessions WHERE id = ?`
	_, err := db.conn.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete session by id: %w", err)
	}
	return nil
}

// DisableForeignKeys disables foreign key constraints (for testing)
func (db *DB) DisableForeignKeys() error {
	_, err := db.conn.Exec("PRAGMA foreign_keys=OFF")
	return err
}

// User operations

// GetUsers returns all users from the database
func (db *DB) GetUsers(ctx context.Context) ([]models.User, error) {
	query := `SELECT id, username, password_hash, role, enabled, created_at, updated_at FROM users ORDER BY username`

	rows, err := db.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query users: %w", err)
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var user models.User
		err := rows.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Role,
			&user.Enabled, &user.CreatedAt, &user.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}
		users = append(users, user)
	}

	return users, rows.Err()
}

// GetUser returns a single user by ID
func (db *DB) GetUser(ctx context.Context, id string) (*models.User, error) {
	query := `SELECT id, username, password_hash, role, enabled, created_at, updated_at FROM users WHERE id = ?`

	var user models.User
	err := db.conn.QueryRowContext(ctx, query, id).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.Role,
		&user.Enabled, &user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

// GetUserByUsername returns a single user by username
func (db *DB) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	query := `SELECT id, username, password_hash, role, enabled, created_at, updated_at FROM users WHERE username = ?`

	var user models.User
	err := db.conn.QueryRowContext(ctx, query, username).Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.Role,
		&user.Enabled, &user.CreatedAt, &user.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user by username: %w", err)
	}

	return &user, nil
}

// CreateUser creates a new user
func (db *DB) CreateUser(ctx context.Context, user *models.User) error {
	query := `INSERT INTO users (id, username, password_hash, role, enabled) VALUES (?, ?, ?, ?, ?)`

	_, err := db.conn.ExecContext(ctx, query, user.ID, user.Username, user.PasswordHash, user.Role, user.Enabled)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	user.CreatedAt = time.Now()
	user.UpdatedAt = time.Now()

	return nil
}

// UpdateUser updates an existing user
func (db *DB) UpdateUser(ctx context.Context, user *models.User) error {
	query := `UPDATE users SET username = ?, password_hash = ?, role = ?, enabled = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`

	_, err := db.conn.ExecContext(ctx, query, user.Username, user.PasswordHash, user.Role, user.Enabled, user.ID)
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	return nil
}

// DeleteUser deletes a user by ID
func (db *DB) DeleteUser(ctx context.Context, id string) error {
	query := `DELETE FROM users WHERE id = ?`

	_, err := db.conn.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	return nil
}

// CountUsers returns the number of users in the database
func (db *DB) CountUsers(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM users`

	var count int
	err := db.conn.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count users: %w", err)
	}

	return count, nil
}

// ConnectionMethod operations

// GetConnectionMethods returns all connection methods from the database
func (db *DB) GetConnectionMethods(ctx context.Context) ([]models.ConnectionMethod, error) {
	query := `SELECT id, name, connection_type, address, username, password, enabled, created_at, updated_at, last_seen, aggregated_managers, aggregated_systems FROM connection_methods ORDER BY name`

	rows, err := db.conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query connection methods: %w", err)
	}
	defer rows.Close()

	var methods []models.ConnectionMethod
	for rows.Next() {
		var method models.ConnectionMethod
		err := rows.Scan(&method.ID, &method.Name, &method.ConnectionMethodType, &method.Address, &method.Username, &method.Password,
			&method.Enabled, &method.CreatedAt, &method.UpdatedAt, &method.LastSeen, &method.AggregatedManagers, &method.AggregatedSystems)
		if err != nil {
			return nil, fmt.Errorf("failed to scan connection method: %w", err)
		}

		// Decrypt password if encryptor is available
		if db.encryptor != nil && crypto.IsEncrypted(method.Password) {
			decrypted, err := db.encryptor.Decrypt(method.Password)
			if err != nil {
				slog.Error("Failed to decrypt connection method password", "method", method.Name, "error", err)
			} else {
				method.Password = decrypted
			}
		}

		methods = append(methods, method)
	}

	return methods, rows.Err()
}

// GetConnectionMethod returns a single connection method by ID
func (db *DB) GetConnectionMethod(ctx context.Context, id string) (*models.ConnectionMethod, error) {
	query := `SELECT id, name, connection_type, address, username, password, enabled, created_at, updated_at, last_seen, aggregated_managers, aggregated_systems FROM connection_methods WHERE id = ?`

	var method models.ConnectionMethod
	err := db.conn.QueryRowContext(ctx, query, id).Scan(
		&method.ID, &method.Name, &method.ConnectionMethodType, &method.Address, &method.Username, &method.Password,
		&method.Enabled, &method.CreatedAt, &method.UpdatedAt, &method.LastSeen, &method.AggregatedManagers, &method.AggregatedSystems)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get connection method: %w", err)
	}

	// Decrypt password if encryptor is available
	if db.encryptor != nil && crypto.IsEncrypted(method.Password) {
		decrypted, err := db.encryptor.Decrypt(method.Password)
		if err != nil {
			slog.Error("Failed to decrypt connection method password", "method", method.Name, "error", err)
		} else {
			method.Password = decrypted
		}
	}

	return &method, nil
}

// CreateConnectionMethod creates a new connection method
func (db *DB) CreateConnectionMethod(ctx context.Context, method *models.ConnectionMethod) error {
	// Encrypt password if encryptor is available
	password := method.Password
	if db.encryptor != nil {
		encrypted, err := db.encryptor.Encrypt(password)
		if err != nil {
			return fmt.Errorf("failed to encrypt connection method password: %w", err)
		}
		password = encrypted
	}

	query := `INSERT INTO connection_methods (id, name, connection_type, address, username, password, enabled, aggregated_managers, aggregated_systems) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := db.conn.ExecContext(ctx, query, method.ID, method.Name, method.ConnectionMethodType, method.Address, method.Username, password, method.Enabled, method.AggregatedManagers, method.AggregatedSystems)
	if err != nil {
		return fmt.Errorf("failed to create connection method: %w", err)
	}

	method.CreatedAt = time.Now()
	method.UpdatedAt = time.Now()

	return nil
}

// UpdateConnectionMethodAggregatedData updates the cached aggregated data for a connection method
func (db *DB) UpdateConnectionMethodAggregatedData(ctx context.Context, id string, managers, systems string) error {
	query := `UPDATE connection_methods SET aggregated_managers = ?, aggregated_systems = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`

	_, err := db.conn.ExecContext(ctx, query, managers, systems, id)
	if err != nil {
		return fmt.Errorf("failed to update connection method aggregated data: %w", err)
	}

	return nil
}

// DeleteConnectionMethod deletes a connection method by ID
func (db *DB) DeleteConnectionMethod(ctx context.Context, id string) error {
	query := `DELETE FROM connection_methods WHERE id = ?`

	_, err := db.conn.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete connection method: %w", err)
	}

	return nil
}

// UpdateConnectionMethodLastSeen updates the last seen timestamp
func (db *DB) UpdateConnectionMethodLastSeen(ctx context.Context, id string) error {
	query := `UPDATE connection_methods SET last_seen = CURRENT_TIMESTAMP WHERE id = ?`

	_, err := db.conn.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to update connection method last seen: %w", err)
	}

	return nil
}

// Profile operations (005)

// CreateProfile inserts a new profile
func (db *DB) CreateProfile(ctx context.Context, p *models.Profile) error {
	if p.ID == "" {
		p.ID = fmt.Sprintf("p_%d", time.Now().UnixNano())
	}
	q := `INSERT INTO profiles (id, name, description, created_by, hardware_selector, firmware_ranges_json) VALUES (?, ?, ?, ?, ?, ?)`
	_, err := db.conn.ExecContext(ctx, q, p.ID, p.Name, p.Description, p.CreatedBy, p.HardwareSelector, p.FirmwareRangesJSON)
	if err != nil {
		return fmt.Errorf("create profile: %w", err)
	}
	p.CreatedAt = time.Now()
	p.UpdatedAt = time.Now()
	return nil
}

// UpdateProfile updates mutable fields of a profile
func (db *DB) UpdateProfile(ctx context.Context, p *models.Profile) error {
	q := `UPDATE profiles SET name = ?, description = ?, created_by = ?, hardware_selector = ?, firmware_ranges_json = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := db.conn.ExecContext(ctx, q, p.Name, p.Description, p.CreatedBy, p.HardwareSelector, p.FirmwareRangesJSON, p.ID)
	if err != nil {
		return fmt.Errorf("update profile: %w", err)
	}
	return nil
}

// GetProfiles lists profiles
func (db *DB) GetProfiles(ctx context.Context) ([]models.Profile, error) {
	rows, err := db.conn.QueryContext(ctx, `SELECT id, name, description, created_by, hardware_selector, firmware_ranges_json, created_at, updated_at FROM profiles ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	defer rows.Close()
	var out []models.Profile
	for rows.Next() {
		var p models.Profile
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.CreatedBy, &p.HardwareSelector, &p.FirmwareRangesJSON, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan profile: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProfile retrieves a profile by id
func (db *DB) GetProfile(ctx context.Context, id string) (*models.Profile, error) {
	var p models.Profile
	err := db.conn.QueryRowContext(ctx, `SELECT id, name, description, created_by, hardware_selector, firmware_ranges_json, created_at, updated_at FROM profiles WHERE id = ?`, id).Scan(&p.ID, &p.Name, &p.Description, &p.CreatedBy, &p.HardwareSelector, &p.FirmwareRangesJSON, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get profile: %w", err)
	}
	return &p, nil
}

// DeleteProfile deletes a profile by id
func (db *DB) DeleteProfile(ctx context.Context, id string) error {
	_, err := db.conn.ExecContext(ctx, `DELETE FROM profiles WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete profile: %w", err)
	}
	return nil
}

// CreateProfileVersion creates a new version row and associated entries
func (db *DB) CreateProfileVersion(ctx context.Context, v *models.ProfileVersion) error {
	if v.ID == "" {
		v.ID = fmt.Sprintf("pv_%d", time.Now().UnixNano())
	}
	tx, err := db.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `INSERT INTO profile_versions (id, profile_id, version, notes) VALUES (?, ?, ?, ?)`, v.ID, v.ProfileID, v.Version, v.Notes)
	if err != nil {
		return fmt.Errorf("insert version: %w", err)
	}

	if len(v.Entries) > 0 {
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO profile_entries (id, profile_version_id, resource_path, attribute, desired_value_json, apply_time_preference, oem_vendor) VALUES (?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return fmt.Errorf("prepare entries: %w", err)
		}
		defer stmt.Close()
		for i := range v.Entries {
			e := &v.Entries[i]
			if e.ID == "" {
				e.ID = fmt.Sprintf("pe_%d", time.Now().UnixNano())
			}
			var dv string
			if e.DesiredValue != nil {
				if b, err := json.Marshal(e.DesiredValue); err == nil {
					dv = string(b)
				}
			}
			if _, err := stmt.ExecContext(ctx, e.ID, v.ID, e.ResourcePath, e.Attribute, dv, e.ApplyTimePreference, e.OEMVendor); err != nil {
				return fmt.Errorf("insert entry: %w", err)
			}
			e.ProfileVersionID = v.ID
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit version: %w", err)
	}
	v.CreatedAt = time.Now()
	return nil
}

// GetProfileVersions lists versions for a profile
func (db *DB) GetProfileVersions(ctx context.Context, profileID string) ([]models.ProfileVersion, error) {
	rows, err := db.conn.QueryContext(ctx, `SELECT id, profile_id, version, created_at, notes FROM profile_versions WHERE profile_id = ? ORDER BY version DESC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	defer rows.Close()
	var out []models.ProfileVersion
	for rows.Next() {
		var v models.ProfileVersion
		if err := rows.Scan(&v.ID, &v.ProfileID, &v.Version, &v.CreatedAt, &v.Notes); err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetProfileVersion loads a version and entries
func (db *DB) GetProfileVersion(ctx context.Context, profileID string, version int) (*models.ProfileVersion, error) {
	var v models.ProfileVersion
	err := db.conn.QueryRowContext(ctx, `SELECT id, profile_id, version, created_at, notes FROM profile_versions WHERE profile_id = ? AND version = ?`, profileID, version).Scan(&v.ID, &v.ProfileID, &v.Version, &v.CreatedAt, &v.Notes)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get version: %w", err)
	}
	erows, err := db.conn.QueryContext(ctx, `SELECT id, resource_path, attribute, desired_value_json, apply_time_preference, oem_vendor FROM profile_entries WHERE profile_version_id = ? ORDER BY resource_path, attribute`, v.ID)
	if err != nil {
		return nil, fmt.Errorf("list entries: %w", err)
	}
	defer erows.Close()
	for erows.Next() {
		var e models.ProfileEntry
		var dv string
		if err := erows.Scan(&e.ID, &e.ResourcePath, &e.Attribute, &dv, &e.ApplyTimePreference, &e.OEMVendor); err != nil {
			return nil, fmt.Errorf("scan entry: %w", err)
		}
		if dv != "" {
			_ = json.Unmarshal([]byte(dv), &e.DesiredValue)
		}
		e.ProfileVersionID = v.ID
		v.Entries = append(v.Entries, e)
	}
	if err := erows.Err(); err != nil {
		return nil, err
	}
	return &v, nil
}

// CreateProfileAssignment adds an assignment
func (db *DB) CreateProfileAssignment(ctx context.Context, a *models.ProfileAssignment) error {
	if a.ID == "" {
		a.ID = fmt.Sprintf("pa_%d", time.Now().UnixNano())
	}
	_, err := db.conn.ExecContext(ctx, `INSERT INTO profile_assignments (id, profile_id, version, target_type, target_value) VALUES (?, ?, ?, ?, ?)`, a.ID, a.ProfileID, a.Version, a.TargetType, a.TargetValue)
	if err != nil {
		return fmt.Errorf("create assignment: %w", err)
	}
	a.CreatedAt = time.Now()
	return nil
}

// GetProfileAssignments lists assignments for a profile
func (db *DB) GetProfileAssignments(ctx context.Context, profileID string) ([]models.ProfileAssignment, error) {
	rows, err := db.conn.QueryContext(ctx, `SELECT id, profile_id, version, target_type, target_value, created_at FROM profile_assignments WHERE profile_id = ? ORDER BY created_at DESC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list assignments: %w", err)
	}
	defer rows.Close()
	var out []models.ProfileAssignment
	for rows.Next() {
		var a models.ProfileAssignment
		if err := rows.Scan(&a.ID, &a.ProfileID, &a.Version, &a.TargetType, &a.TargetValue, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan assignment: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteProfileAssignment deletes an assignment by id
func (db *DB) DeleteProfileAssignment(ctx context.Context, id string) error {
	_, err := db.conn.ExecContext(ctx, `DELETE FROM profile_assignments WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete assignment: %w", err)
	}
	return nil
}
