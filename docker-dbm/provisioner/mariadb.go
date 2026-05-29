package provisioner

import (
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"strings"

	_ "github.com/go-sql-driver/mysql"
)

// Pre-compiled regex pattern for MariaDB identifier validation
var mariaDBIdentifierRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// MariaDBProvisioner implements DatabaseProvisioner for MariaDB/MySQL databases.
type MariaDBProvisioner struct{}

// Name returns the database type name.
func (m *MariaDBProvisioner) Name() string {
	return "mariadb"
}

// Provision creates a new database and user in MariaDB with strict isolation.
// It checks if the database or user already exists before creation.
// The created user is granted full privileges ONLY on the specified database.
func (m *MariaDBProvisioner) Provision(config Config) error {
	log.Printf("[MariaDB] Connecting to server at %s:%s...", config.DBHost, config.DBPort)

	// Build DSN (Data Source Name)
	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/",
		config.AdminUser,
		config.AdminPass,
		config.DBHost,
		config.DBPort,
	)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to connect to MariaDB: %w", err)
	}
	defer db.Close()

	// Test the connection
	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping MariaDB server: %w", err)
	}
	log.Println("[MariaDB] Connected successfully")

	return m.provision(db, config)
}

// provision performs the actual database and user provisioning against an
// already-established connection. It is separated from Provision so the
// provisioning logic can be exercised with a mocked database in tests.
func (m *MariaDBProvisioner) provision(db *sql.DB, config Config) error {
	var err error

	// Validate identifiers to prevent SQL injection
	if !isValidMariaDBIdentifier(config.AppDBName) {
		return fmt.Errorf("invalid database name: %s (must contain only alphanumeric characters and underscores)", config.AppDBName)
	}
	if !isValidMariaDBIdentifier(config.AppDBUser) {
		return fmt.Errorf("invalid username: %s (must contain only alphanumeric characters and underscores)", config.AppDBUser)
	}

	// Check if database already exists
	var dbExists int
	err = db.QueryRow(
		"SELECT COUNT(*) FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME = ?",
		config.AppDBName,
	).Scan(&dbExists)
	if err != nil {
		return fmt.Errorf("failed to check if database exists: %w", err)
	}
	if dbExists > 0 {
		log.Printf("[MariaDB] ABORT: Database '%s' already exists!", config.AppDBName)
		return fmt.Errorf("%w: %s", ErrDatabaseExists, config.AppDBName)
	}

	// Check if user already exists
	var userExists int
	err = db.QueryRow(
		"SELECT COUNT(*) FROM mysql.user WHERE User = ?",
		config.AppDBUser,
	).Scan(&userExists)
	if err != nil {
		return fmt.Errorf("failed to check if user exists: %w", err)
	}
	if userExists > 0 {
		log.Printf("[MariaDB] ABORT: User '%s' already exists!", config.AppDBUser)
		return fmt.Errorf("%w: %s", ErrUserExists, config.AppDBUser)
	}

	log.Printf("[MariaDB] Creating database '%s'...", config.AppDBName)

	// Create the database
	createDBQuery := fmt.Sprintf(
		"CREATE DATABASE %s CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci",
		quoteMariaDBIdentifier(config.AppDBName),
	)
	_, err = db.Exec(createDBQuery)
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}
	log.Printf("[MariaDB] Database '%s' created successfully", config.AppDBName)

	log.Printf("[MariaDB] Creating user '%s'...", config.AppDBUser)

	// Create the user
	// NOTE: DDL statements like CREATE USER don't support parameterized queries,
	// so we must escape the password manually to prevent SQL injection.
	createUserQuery := fmt.Sprintf(
		"CREATE USER %s@'%%' IDENTIFIED BY %s",
		quoteMariaDBIdentifier(config.AppDBUser),
		escapeMariaDBStringLiteral(config.AppDBPass),
	)
	_, err = db.Exec(createUserQuery)
	if err != nil {
		// Cleanup: try to drop the database if user creation fails
		cleanupQuery := fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoteMariaDBIdentifier(config.AppDBName))
		db.Exec(cleanupQuery)
		return fmt.Errorf("failed to create user: %w", err)
	}
	log.Printf("[MariaDB] User '%s' created successfully", config.AppDBUser)

	// SECURITY: Grant privileges ONLY on the specific database
	log.Printf("[MariaDB] Granting privileges on '%s' to '%s'...", config.AppDBName, config.AppDBUser)
	grantQuery := fmt.Sprintf(
		"GRANT ALL PRIVILEGES ON %s.* TO %s@'%%'",
		quoteMariaDBIdentifier(config.AppDBName),
		quoteMariaDBIdentifier(config.AppDBUser),
	)
	_, err = db.Exec(grantQuery)
	if err != nil {
		// Cleanup on failure
		dropUserQuery := fmt.Sprintf("DROP USER IF EXISTS %s@'%%'", quoteMariaDBIdentifier(config.AppDBUser))
		db.Exec(dropUserQuery)
		dropDBQuery := fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoteMariaDBIdentifier(config.AppDBName))
		db.Exec(dropDBQuery)
		return fmt.Errorf("failed to grant privileges: %w", err)
	}
	log.Printf("[MariaDB] Granted ALL PRIVILEGES on '%s.*' to '%s'", config.AppDBName, config.AppDBUser)

	// NOTE: No FLUSH PRIVILEGES is needed here. In MariaDB/MySQL, that statement is
	// only required when the grant tables are modified directly via INSERT/UPDATE/DELETE.
	// Account management statements (CREATE USER, GRANT) automatically reload the
	// in-memory grant tables, so privileges take effect immediately. Skipping the
	// statement also avoids a needless round-trip and a failure when the admin user
	// lacks the global RELOAD privilege.

	log.Println("[MariaDB] Provisioning completed successfully!")
	log.Printf("[MariaDB] Database: %s | User: %s | Host: %s:%s",
		config.AppDBName, config.AppDBUser, config.DBHost, config.DBPort)

	return nil
}

// quoteMariaDBIdentifier properly quotes a MariaDB identifier to prevent SQL injection.
func quoteMariaDBIdentifier(name string) string {
	// Escape backticks by doubling them and wrap in backticks
	escaped := strings.ReplaceAll(name, "`", "``")
	return "`" + escaped + "`"
}

// isValidMariaDBIdentifier checks if a string is a valid MariaDB identifier.
func isValidMariaDBIdentifier(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	// Allow alphanumeric characters and underscores only
	return mariaDBIdentifierRegex.MatchString(name)
}

// escapeMariaDBStringLiteral escapes a string for safe use as a MariaDB string literal.
// This is necessary for DDL statements like CREATE USER which don't support
// parameterized queries for the password field.
func escapeMariaDBStringLiteral(s string) string {
	// Escape single quotes by doubling them, and escape backslashes
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `'`, `''`)
	return "'" + escaped + "'"
}
