package database

import (
	"context"
	"database/sql"
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
