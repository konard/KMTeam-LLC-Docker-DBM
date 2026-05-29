package provisioner

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// blackHoleListener starts a TCP listener that accepts connections but never
// writes any response, simulating a misconfigured network or a firewall that
// silently drops packets after the TCP handshake. A database driver attempting
// its protocol handshake against such an endpoint will block until a timeout
// fires. The listener is closed automatically when the test finishes.
func blackHoleListener(t *testing.T) (host string, port string) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start black-hole listener: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	// Accept connections and hold them open without ever replying.
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			// Keep the connection open until the test ends; never respond.
			t.Cleanup(func() { _ = conn.Close() })
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", strconv.Itoa(addr.Port)
}

// withShortPingTimeout temporarily shrinks the package-level pingTimeout so the
// tests exercise the timeout path quickly, restoring the original afterwards.
func withShortPingTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	original := pingTimeout
	pingTimeout = d
	t.Cleanup(func() { pingTimeout = original })
}

// runWithDeadline runs fn and fails the test if it does not return within the
// given deadline, which is how we detect a hanging db.Ping call.
func runWithDeadline(t *testing.T, deadline time.Duration, fn func() error) error {
	t.Helper()

	done := make(chan error, 1)
	go func() { done <- fn() }()

	select {
	case err := <-done:
		return err
	case <-time.After(deadline):
		t.Fatalf("Provision did not return within %v; the ping likely hung", deadline)
		return nil // unreachable
	}
}

// TestPostgresProvisionPingTimeout verifies that the PostgreSQL provisioner
// bounds its connectivity check with a timeout instead of hanging forever when
// the server accepts the TCP connection but never completes the handshake.
func TestPostgresProvisionPingTimeout(t *testing.T) {
	withShortPingTimeout(t, 500*time.Millisecond)
	host, port := blackHoleListener(t)

	prov := &PostgresProvisioner{}
	config := Config{
		DBType:    "postgres",
		DBHost:    host,
		DBPort:    port,
		AdminUser: "admin",
		AdminPass: "secret",
		AppDBName: "app_db",
		AppDBUser: "app_user",
		AppDBPass: "app_pass",
	}

	start := time.Now()
	err := runWithDeadline(t, 5*time.Second, func() error {
		return prov.Provision(config)
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error from ping timeout, got nil")
	}
	if !strings.Contains(err.Error(), "failed to ping PostgreSQL server") {
		t.Fatalf("expected a ping failure error, got: %v", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("ping took %v; expected it to be bounded by the timeout", elapsed)
	}
}

// TestMariaDBProvisionPingTimeout verifies the same timeout behaviour for the
// MariaDB provisioner.
func TestMariaDBProvisionPingTimeout(t *testing.T) {
	withShortPingTimeout(t, 500*time.Millisecond)
	host, port := blackHoleListener(t)

	prov := &MariaDBProvisioner{}
	config := Config{
		DBType:    "mariadb",
		DBHost:    host,
		DBPort:    port,
		AdminUser: "admin",
		AdminPass: "secret",
		AppDBName: "app_db",
		AppDBUser: "app_user",
		AppDBPass: "app_pass",
	}

	start := time.Now()
	err := runWithDeadline(t, 5*time.Second, func() error {
		return prov.Provision(config)
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error from ping timeout, got nil")
	}
	if !strings.Contains(err.Error(), "failed to ping MariaDB server") {
		t.Fatalf("expected a ping failure error, got: %v", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("ping took %v; expected it to be bounded by the timeout", elapsed)
	}
}

// TestPingTimeoutDefault documents the production default of a strict 5-second
// ping timeout as required by the issue's acceptance criteria.
func TestPingTimeoutDefault(t *testing.T) {
	if pingTimeout != 5*time.Second {
		t.Fatalf("expected default pingTimeout of 5s, got %v", pingTimeout)
	}
}
