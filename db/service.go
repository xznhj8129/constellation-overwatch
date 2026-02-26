package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/shared"
	"go.uber.org/zap"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaFS embed.FS

// Service represents the database service with connection management
type Service struct {
	DB     *sql.DB
	DBPath string
}

// Config holds database configuration
type Config struct {
	DBPath         string
	MaxOpenConns   int
	MaxIdleConns   int
	AutoInitialize bool // Automatically initialize schema if DB doesn't exist
}

// DefaultConfig returns default database configuration
func DefaultConfig() *Config {
	return &Config{
		DBPath:         shared.GetEnv("OVERWATCH_DATA_DIR", "./data") + "/db/constellation.db",
		MaxOpenConns:   1, // SQLite doesn't handle concurrent writes well
		MaxIdleConns:   1,
		AutoInitialize: true,
	}
}

// New creates a new database service instance
func New(config *Config) (*Service, error) {
	if config == nil {
		config = DefaultConfig()
	}

	service := &Service{
		DBPath: config.DBPath,
	}

	// Check if database file exists
	dbExists := fileExists(config.DBPath)

	// Ensure the directory exists
	dbDir := filepath.Dir(config.DBPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database connection using libsql
	// Ensure absolute path for local files
	absPath, err := filepath.Abs(config.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path for database: %w", err)
	}

	connStr := "file:" + absPath + "?_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	db.SetConnMaxLifetime(0)

	service.DB = db

	// Test the connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Initialize schema if database is new and auto-initialization is enabled
	if !dbExists && config.AutoInitialize {
		if err := service.InitializeSchema(); err != nil {
			return nil, fmt.Errorf("failed to initialize schema: %w", err)
		}
		logger.Info("Database schema initialized")
	}

	logger.Infow("Database ready", "path", config.DBPath)
	return service, nil
}

// NewService creates a new database service with default configuration
func NewService() (*Service, error) {
	return New(DefaultConfig())
}

// Name returns the service name (implements Service interface)
func (s *Service) Name() string {
	return "database"
}

// Start initializes the database service (implements Service interface)
func (s *Service) Start(ctx context.Context) error {
	// Verify schema is properly initialized
	if err := s.VerifySchema(); err != nil {
		logger.Warn("Schema verification failed, reinitializing", zap.Error(err))
		if err := s.InitializeSchema(); err != nil {
			return fmt.Errorf("failed to initialize schema: %w", err)
		}
	}

	// Apply incremental migrations for existing databases
	if err := s.MigrateSchema(); err != nil {
		logger.Warnw("Schema migration encountered issues", "error", err)
	}

	return nil
}

// Stop gracefully shuts down the database service (implements Service interface)
func (s *Service) Stop(ctx context.Context) error {
	return s.Close()
}

// HealthCheck returns the health status of the database service (implements Service interface)
func (s *Service) HealthCheck() error {
	return s.Health()
}

// InitializeSchema loads and executes the schema.sql file
func (s *Service) InitializeSchema() error {
	// Read schema from embedded filesystem
	schemaSQL, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("failed to read schema file: %w", err)
	}

	// Execute schema

	// Parse and execute schema statement by statement
	// We need to handle triggers specially as they contain internal semicolons
	statements := splitSQLStatements(string(schemaSQL))

	for i, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		// For triggers, collapse to single line (turso driver workaround)
		upperStmt := strings.ToUpper(stmt)
		if strings.HasPrefix(upperStmt, "CREATE TRIGGER") {
			// Normalize whitespace - collapse multiple spaces/newlines to single space
			stmt = strings.Join(strings.Fields(stmt), " ")
		}

		if _, err := s.DB.Exec(stmt); err != nil {
			logger.Errorw("Failed to execute schema statement", "statement", stmt, "index", i)
			return fmt.Errorf("failed to execute schema statement %d: %w", i, err)
		}
	}

	return nil
}

// splitSQLStatements splits a SQL script into individual statements,
// correctly handling triggers (which contain internal semicolons)
func splitSQLStatements(sql string) []string {
	var statements []string
	var currentStmt strings.Builder

	lines := strings.Split(sql, "\n")
	inTrigger := false
	inView := false

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		// Skip empty lines and full-line comments
		if trimmedLine == "" || strings.HasPrefix(trimmedLine, "--") {
			continue
		}

		upperLine := strings.ToUpper(trimmedLine)

		// Detect start of a trigger definition
		if strings.HasPrefix(upperLine, "CREATE TRIGGER") {
			inTrigger = true
		}

		// Detect start of a view definition (may span multiple lines)
		if strings.HasPrefix(upperLine, "CREATE VIEW") {
			inView = true
		}

		currentStmt.WriteString(line)
		currentStmt.WriteString("\n")

		// Check if we should finalize this statement
		if strings.HasSuffix(trimmedLine, ";") {
			if inTrigger {
				// For triggers, only finalize when we see END;
				if upperLine == "END;" {
					inTrigger = false
					statements = append(statements, currentStmt.String())
					currentStmt.Reset()
				}
				// Otherwise continue accumulating the trigger
			} else if inView {
				// Views end with the closing semicolon after the SELECT
				inView = false
				statements = append(statements, currentStmt.String())
				currentStmt.Reset()
			} else {
				// Regular statement
				statements = append(statements, currentStmt.String())
				currentStmt.Reset()
			}
		}
	}

	// Handle any remaining statement
	if remaining := strings.TrimSpace(currentStmt.String()); remaining != "" {
		statements = append(statements, remaining)
	}

	return statements
}

// VerifySchema checks if the database schema is properly initialized
func (s *Service) VerifySchema() error {
	// Check if core tables exist
	requiredTables := []string{
		"organizations",
		"entities",
		"entity_relationships",
		"messages",
		"missions",
		"users",
		"telemetry",
		"audit_log",
		"webauthn_credentials",
		"webauthn_sessions",
		"api_keys",
		"invites",
	}

	for _, table := range requiredTables {
		var exists int
		query := `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`
		if err := s.DB.QueryRow(query, table).Scan(&exists); err != nil {
			return fmt.Errorf("failed to check table %s: %w", table, err)
		}
		if exists == 0 {
			return fmt.Errorf("required table missing: %s", table)
		}
	}

	return nil
}

// Close closes the database connection
func (s *Service) Close() error {
	if s.DB != nil {
		logger.Info("Closing database connection...")
		return s.DB.Close()
	}
	return nil
}

// GetDB returns the underlying database connection
func (s *Service) GetDB() *sql.DB {
	return s.DB
}

// Transaction executes a function within a database transaction
func (s *Service) Transaction(fn func(*sql.Tx) error) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p) // re-throw panic after rollback
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("transaction error: %v, rollback error: %v", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Health checks the database connection health
func (s *Service) Health() error {
	if s.DB == nil {
		return fmt.Errorf("database connection is nil")
	}
	return s.DB.Ping()
}

// GetStats returns database connection statistics
func (s *Service) GetStats() sql.DBStats {
	return s.DB.Stats()
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// MigrateSchema applies any pending schema migrations.
func (s *Service) MigrateSchema() error {
	// Ensure users.email has a unique index (added after initial schema).
	_, err := s.DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_unique ON users(email)`)
	if err != nil {
		logger.Warnw("Failed to create unique email index (may already exist)", "error", err)
	}

	// Add user_ref column to webauthn_sessions if it doesn't exist (needed for
	// random session keys that store the user reference alongside the session).
	if !s.columnExists("webauthn_sessions", "user_ref") {
		if _, err := s.DB.Exec(`ALTER TABLE webauthn_sessions ADD COLUMN user_ref TEXT DEFAULT ''`); err != nil {
			logger.Warnw("Failed to add user_ref column to webauthn_sessions", "error", err)
		} else {
			logger.Info("Added user_ref column to webauthn_sessions")
		}
	}

	return nil
}

// columnExists checks whether a column exists on a table.
func (s *Service) columnExists(table, column string) bool {
	rows, err := s.DB.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			continue
		}
		if name == column {
			return true
		}
	}
	return false
}

// GetSchemaVersion returns the current schema version
// This is a placeholder for future versioning support
func (s *Service) GetSchemaVersion() (string, error) {
	// TODO: Implement schema versioning
	return "1.0.0", nil
}

// EntityExists checks if an entity exists in the database
func (s *Service) EntityExists(entityID string) (bool, error) {
	if entityID == "" {
		return false, fmt.Errorf("entity_id cannot be empty")
	}

	var exists int
	query := `SELECT COUNT(*) FROM entities WHERE entity_id = ?`
	err := s.DB.QueryRow(query, entityID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check entity existence: %w", err)
	}

	return exists > 0, nil
}
