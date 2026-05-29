package provisioner

import (
	"database/sql"
	"fmt"
	"log"
	"regexp"

	_ "github.com/lib/pq"
)

// Pre-compiled regex patterns for efficiency
var (
	// quoteRegex matches double quotes for escaping in PostgreSQL identifiers
	quoteRegex = regexp.MustCompile(`"`)
	// identifierRegex validates PostgreSQL identifiers
	identifierRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
	// singleQuoteRegex matches single quotes for escaping in PostgreSQL string literals
	singleQuoteRegex = regexp.MustCompile(`'`)
)

// PostgresProvisioner implements DatabaseProvisioner for PostgreSQL databases.
type PostgresProvisioner struct{}

// Name returns the database type name.
func (p *PostgresProvisioner) Name() string {
	return "postgres"
}

// Provision creates a new database and user in PostgreSQL with strict isolation.
// It checks if the database or user already exists before creation.
// The created user is granted ownership of the database and public access is revoked.
func (p *PostgresProvisioner) Provision(config Config) error {
	log.Printf("[PostgreSQL] Connecting to server at %s:%s...", config.DBHost, config.DBPort)

	// Connect to the postgres system database
	connStr := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=postgres sslmode=disable",
		config.DBHost,
		config.DBPort,
		config.AdminUser,
		config.AdminPass,
	)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	defer db.Close()

	// Test the connection
	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping PostgreSQL server: %w", err)
	}
	log.Println("[PostgreSQL] Connected successfully")

	// Validate identifiers to prevent SQL injection
	if !isValidIdentifier(config.AppDBName) {
		return fmt.Errorf("invalid database name: %s (must contain only alphanumeric characters and underscores)", config.AppDBName)
	}
	if !isValidIdentifier(config.AppDBUser) {
		return fmt.Errorf("invalid username: %s (must contain only alphanumeric characters and underscores)", config.AppDBUser)
	}

	// Check if database already exists
	var dbExists bool
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", config.AppDBName).Scan(&dbExists)
	if err != nil {
		return fmt.Errorf("failed to check if database exists: %w", err)
	}
	if dbExists {
		log.Printf("[PostgreSQL] ABORT: Database '%s' already exists!", config.AppDBName)
		return fmt.Errorf("%w: %s", ErrDatabaseExists, config.AppDBName)
	}

	// Check if user already exists
	var userExists bool
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1)", config.AppDBUser).Scan(&userExists)
	if err != nil {
		return fmt.Errorf("failed to check if user exists: %w", err)
	}
	if userExists {
		log.Printf("[PostgreSQL] ABORT: User '%s' already exists!", config.AppDBUser)
		return fmt.Errorf("%w: %s", ErrUserExists, config.AppDBUser)
	}

	log.Printf("[PostgreSQL] Creating user '%s'...", config.AppDBUser)

	// Create the user with login permission
	// NOTE: DDL statements like CREATE USER don't support parameterized queries,
	// so we must escape the password manually to prevent SQL injection.
	createUserQuery := fmt.Sprintf(
		"CREATE USER %s WITH PASSWORD %s",
		quoteIdentifier(config.AppDBUser),
		escapeStringLiteral(config.AppDBPass),
	)
	_, err = db.Exec(createUserQuery)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}
	log.Printf("[PostgreSQL] User '%s' created successfully", config.AppDBUser)

	log.Printf("[PostgreSQL] Creating database '%s' with owner '%s'...", config.AppDBName, config.AppDBUser)

	// Create the database with the user as owner
	// Note: CREATE DATABASE doesn't support parameterized identifiers
	createDBQuery := fmt.Sprintf(
		"CREATE DATABASE %s OWNER %s",
		quoteIdentifier(config.AppDBName),
		quoteIdentifier(config.AppDBUser),
	)
	_, err = db.Exec(createDBQuery)
	if err != nil {
		// Cleanup: try to drop the user if database creation fails
		cleanupQuery := fmt.Sprintf("DROP USER IF EXISTS %s", quoteIdentifier(config.AppDBUser))
		db.Exec(cleanupQuery)
		return fmt.Errorf("failed to create database: %w", err)
	}
	log.Printf("[PostgreSQL] Database '%s' created successfully", config.AppDBName)

	// SECURITY: Revoke all public access to ensure strict isolation
	log.Printf("[PostgreSQL] Revoking public access from database '%s'...", config.AppDBName)
	revokeQuery := fmt.Sprintf(
		"REVOKE ALL ON DATABASE %s FROM PUBLIC",
		quoteIdentifier(config.AppDBName),
	)
	_, err = db.Exec(revokeQuery)
	if err != nil {
		log.Printf("[PostgreSQL] WARNING: Failed to revoke public access: %v", err)
		// Cleanup: drop the database and user to avoid leaving orphaned resources
		rollbackPostgres(db, config.AppDBName, config.AppDBUser)
		// This is a security-critical operation, so we treat it as an error
		return fmt.Errorf("failed to revoke public access: %w", err)
	}
	log.Printf("[PostgreSQL] Public access revoked successfully")

	// Grant all privileges on the database to the owner (explicit grant)
	grantQuery := fmt.Sprintf(
		"GRANT ALL PRIVILEGES ON DATABASE %s TO %s",
		quoteIdentifier(config.AppDBName),
		quoteIdentifier(config.AppDBUser),
	)
	_, err = db.Exec(grantQuery)
	if err != nil {
		// Cleanup: drop the database and user to avoid leaving orphaned resources
		rollbackPostgres(db, config.AppDBName, config.AppDBUser)
		return fmt.Errorf("failed to grant privileges: %w", err)
	}
	log.Printf("[PostgreSQL] Granted all privileges on '%s' to '%s'", config.AppDBName, config.AppDBUser)

	log.Println("[PostgreSQL] Provisioning completed successfully!")
	log.Printf("[PostgreSQL] Database: %s | User: %s | Host: %s:%s",
		config.AppDBName, config.AppDBUser, config.DBHost, config.DBPort)

	return nil
}

// rollbackPostgres drops the newly created database and user to avoid leaving
// orphaned resources when provisioning fails after both have been created.
// Identifiers are safely escaped using quoteIdentifier(). Cleanup errors are
// logged but not returned, since the original provisioning error takes priority.
func rollbackPostgres(db *sql.DB, dbName, dbUser string) {
	log.Printf("[PostgreSQL] Rolling back: dropping database '%s' and user '%s'...", dbName, dbUser)

	dropDBQuery := fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoteIdentifier(dbName))
	if _, err := db.Exec(dropDBQuery); err != nil {
		log.Printf("[PostgreSQL] WARNING: Failed to drop database '%s' during rollback: %v", dbName, err)
	}

	dropUserQuery := fmt.Sprintf("DROP USER IF EXISTS %s", quoteIdentifier(dbUser))
	if _, err := db.Exec(dropUserQuery); err != nil {
		log.Printf("[PostgreSQL] WARNING: Failed to drop user '%s' during rollback: %v", dbUser, err)
	}
}

// quoteIdentifier properly quotes a PostgreSQL identifier to prevent SQL injection.
func quoteIdentifier(name string) string {
	// Double any existing double quotes and wrap in double quotes
	return `"` + quoteRegex.ReplaceAllString(name, `""`) + `"`
}

// isValidIdentifier checks if a string is a valid database identifier.
func isValidIdentifier(name string) bool {
	if name == "" || len(name) > 63 {
		return false
	}
	// Allow alphanumeric characters and underscores only
	return identifierRegex.MatchString(name)
}

// escapeStringLiteral escapes a string for safe use as a PostgreSQL string literal.
// This is necessary for DDL statements like CREATE USER which don't support
// parameterized queries for the password field.
func escapeStringLiteral(s string) string {
	// Escape single quotes by doubling them and wrap in single quotes
	return "'" + singleQuoteRegex.ReplaceAllString(s, "''") + "'"
}
