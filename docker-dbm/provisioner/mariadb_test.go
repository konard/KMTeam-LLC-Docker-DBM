package provisioner

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"strings"
	"sync"
	"testing"
)

// The tests below exercise MariaDBProvisioner.provision against a custom
// recording database driver. Unlike a mock that only checks a fixed set of
// expectations, this driver captures EVERY statement the provisioner sends,
// which lets us assert that no FLUSH PRIVILEGES statement is ever issued
// (the subject of issue #3).

// recorder collects every statement executed through the fake driver.
type recorder struct {
	mu    sync.Mutex
	stmts []string
}

func (r *recorder) add(query string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stmts = append(r.stmts, query)
}

func (r *recorder) statements() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.stmts))
	copy(out, r.stmts)
	return out
}

// activeRecorder is the recorder used by connections opened through the fake
// driver. Tests set it (guarded by registerOnce) before opening a connection.
var (
	activeRecorder *recorder
	registerOnce   sync.Once
)

const fakeDriverName = "docker-dbm-recorder"

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) {
	return &fakeConn{rec: activeRecorder}, nil
}

type fakeConn struct{ rec *recorder }

// Prepare is required by driver.Conn but is unused: implementing the
// *Context interfaces makes database/sql bypass the prepared-statement path.
func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c *fakeConn) Close() error                        { return nil }
func (c *fakeConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }

func (c *fakeConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.rec.add(query)
	return driver.RowsAffected(1), nil
}

func (c *fakeConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.rec.add(query)
	// All queries in the provisioner are COUNT(*) existence checks; return 0
	// so the provisioner proceeds as if nothing exists yet.
	return &fakeRows{cols: []string{"count"}, vals: [][]driver.Value{{int64(0)}}}, nil
}

type fakeRows struct {
	cols []string
	vals [][]driver.Value
	pos  int
}

func (r *fakeRows) Columns() []string { return r.cols }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.vals) {
		return io.EOF
	}
	copy(dest, r.vals[r.pos])
	r.pos++
	return nil
}

// newRecordingDB opens a *sql.DB backed by the recording driver and returns it
// together with the recorder capturing its statements.
func newRecordingDB(t *testing.T) (*sql.DB, *recorder) {
	t.Helper()
	registerOnce.Do(func() {
		sql.Register(fakeDriverName, fakeDriver{})
	})
	rec := &recorder{}
	activeRecorder = rec
	db, err := sql.Open(fakeDriverName, "")
	if err != nil {
		t.Fatalf("failed to open recording db: %v", err)
	}
	return db, rec
}

func testConfig() Config {
	return Config{
		DBType:    "mariadb",
		DBHost:    "localhost",
		DBPort:    "3306",
		AdminUser: "admin",
		AdminPass: "adminpass",
		AppDBName: "appdb",
		AppDBUser: "appuser",
		AppDBPass: "apppass",
	}
}

// TestProvisionNeverFlushesPrivileges is the regression test for issue #3:
// the provisioner must NOT issue a FLUSH PRIVILEGES statement, since
// CREATE USER / GRANT update the in-memory grant tables automatically.
func TestProvisionNeverFlushesPrivileges(t *testing.T) {
	db, rec := newRecordingDB(t)
	defer db.Close()

	m := &MariaDBProvisioner{}
	if err := m.provision(db, testConfig()); err != nil {
		t.Fatalf("provision returned unexpected error: %v", err)
	}

	for _, stmt := range rec.statements() {
		if strings.Contains(strings.ToUpper(stmt), "FLUSH PRIVILEGES") {
			t.Errorf("provision executed a forbidden FLUSH PRIVILEGES statement: %q", stmt)
		}
	}
}

// TestProvisionExecutesExpectedStatements verifies that, after removing the
// flush, the provisioner still performs the expected work: existence checks,
// database creation, user creation, and a database-scoped grant.
func TestProvisionExecutesExpectedStatements(t *testing.T) {
	db, rec := newRecordingDB(t)
	defer db.Close()

	m := &MariaDBProvisioner{}
	if err := m.provision(db, testConfig()); err != nil {
		t.Fatalf("provision returned unexpected error: %v", err)
	}

	stmts := rec.statements()
	requireStatement(t, stmts, "INFORMATION_SCHEMA.SCHEMATA")
	requireStatement(t, stmts, "FROM mysql.user")
	requireStatement(t, stmts, "CREATE DATABASE `appdb`")
	requireStatement(t, stmts, "CREATE USER `appuser`@'%'")
	requireStatement(t, stmts, "GRANT ALL PRIVILEGES ON `appdb`.* TO `appuser`@'%'")
}

func requireStatement(t *testing.T, stmts []string, substr string) {
	t.Helper()
	for _, s := range stmts {
		if strings.Contains(s, substr) {
			return
		}
	}
	t.Errorf("expected a statement containing %q, executed statements were: %v", substr, stmts)
}
