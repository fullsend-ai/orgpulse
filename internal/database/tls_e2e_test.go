//go:build e2e

// End-to-end tests against a real MySQL server with require_secure_transport
// enabled, proving DB_TLS_CA_BUNDLE_PATH actually works (not just that
// buildDSN constructs the right string). "e2e" because it needs a real
// container spun up from inside the test run (nested containers) -- not
// runnable on the shared GitLab CI runners, hence its own build tag and
// make target (test-e2e) kept out of `make test`/CI. See docs/e2e-testing.md
// for how the fixture certs under testdata/tls/ were generated and how to
// run this locally.
package database

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

const testdataTLSDir = "testdata/tls"

func e2eConfig(t *testing.T) (host string, port int, user, password, dbName string) {
	t.Helper()
	host = os.Getenv("TEST_MYSQL_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	portStr := os.Getenv("TEST_MYSQL_PORT")
	if portStr == "" {
		portStr = "13306"
	}
	var err error
	port, err = strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("invalid TEST_MYSQL_PORT %q: %v", portStr, err)
	}
	user = envOrFatal(t, "TEST_MYSQL_USER")
	password = envOrFatal(t, "TEST_MYSQL_PASSWORD")
	dbName = envOrFatal(t, "TEST_MYSQL_DATABASE")
	return
}

func envOrFatal(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Fatalf("%s must be set to run e2e tests (see docs/e2e-testing.md)", key)
	}
	return v
}

// TestE2E_RequireSecureTransport_RejectsPlainConnection proves the test
// server genuinely enforces TLS -- if this fails, the test setup itself is
// wrong (not enforcing what we think it is), and the "succeeds with CA" test
// below would be meaningless.
func TestE2E_RequireSecureTransport_RejectsPlainConnection(t *testing.T) {
	host, port, user, password, dbName := e2eConfig(t)

	store, err := NewMySQLStore(host, port, user, password, dbName, "")
	if err == nil {
		store.Close()
		t.Fatal("expected connecting without TLS to fail against a require_secure_transport server, but it succeeded")
	}
}

// TestE2E_WithCABundle_ConnectsSuccessfully proves DB_TLS_CA_BUNDLE_PATH's
// certificate-verification path actually works against a real server
// enforcing TLS, not just that it changes the DSN string.
func TestE2E_WithCABundle_ConnectsSuccessfully(t *testing.T) {
	host, port, user, password, dbName := e2eConfig(t)

	store, err := NewMySQLStore(host, port, user, password, dbName, filepath.Join(testdataTLSDir, "ca.pem"))
	if err != nil {
		t.Fatalf("expected connecting with a valid CA bundle to succeed, got: %v", err)
	}
	defer store.Close()

	if err := store.rawDB.PingContext(context.Background()); err != nil {
		t.Fatalf("ping over the established TLS connection failed: %v", err)
	}
}

// TestE2E_WrongCABundle_FailsVerification proves this is real certificate
// verification, not tls=skip-verify with a CA bundle along for show -- a CA
// that didn't sign the server's cert must be rejected. Uses
// generateTestCAPEM (database_test.go) to make an unrelated CA on the fly --
// it must NOT match testdata/tls/ca.pem, the one that actually signed the
// test server's certificate.
func TestE2E_WrongCABundle_FailsVerification(t *testing.T) {
	host, port, user, password, dbName := e2eConfig(t)

	dir := t.TempDir()
	wrongCAPath := writeTempFile(t, dir, "wrong-ca.pem", generateTestCAPEM(t))

	store, err := NewMySQLStore(host, port, user, password, dbName, wrongCAPath)
	if err == nil {
		store.Close()
		t.Fatal("expected connecting with the wrong CA to fail certificate verification, but it succeeded")
	}
}
