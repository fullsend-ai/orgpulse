package database

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// generateTestCAPEM creates a real, valid self-signed CA certificate for
// tests -- generated fresh each run rather than a hand-typed PEM blob, so
// it's guaranteed to actually parse.
func generateTestCAPEM(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{Organization: []string{"orgpulse test CA"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestBuildDSN_NoCABundle_MatchesOriginalFormat(t *testing.T) {
	dsn, err := buildDSN("dbhost", 3306, "user", "pass", "mydb", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "user:pass@tcp(dbhost:3306)/mydb?parseTime=true&multiStatements=true"
	if dsn != want {
		t.Errorf("dsn = %q, want %q", dsn, want)
	}
	if strings.Contains(dsn, "tls=") {
		t.Errorf("dsn should have no tls param when caBundlePath is empty, got %q", dsn)
	}
}

func TestBuildDSN_MissingCABundleFile_ReturnsClearError(t *testing.T) {
	_, err := buildDSN("dbhost", 3306, "user", "pass", "mydb", "/does/not/exist.pem")
	if err == nil {
		t.Fatal("expected an error for a missing CA bundle file, got nil")
	}
	if !strings.Contains(err.Error(), "/does/not/exist.pem") {
		t.Errorf("error should name the missing path, got: %v", err)
	}
}

func TestBuildDSN_InvalidPEMContent_ReturnsClearError(t *testing.T) {
	dir := t.TempDir()
	badFile := writeTempFile(t, dir, "bad.pem", "not a certificate")

	_, err := buildDSN("dbhost", 3306, "user", "pass", "mydb", badFile)
	if err == nil {
		t.Fatal("expected an error for invalid PEM content, got nil")
	}
	if !strings.Contains(err.Error(), "no valid certificates") {
		t.Errorf("expected a 'no valid certificates' error, got: %v", err)
	}
}

func TestBuildDSN_ValidCABundle_RequestsRegisteredTLSConfig(t *testing.T) {
	dir := t.TempDir()
	caFile := writeTempFile(t, dir, "ca.pem", generateTestCAPEM(t))

	dsn, err := buildDSN("dbhost", 3306, "user", "pass", "mydb", caFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "user:pass@tcp(dbhost:3306)/mydb?parseTime=true&multiStatements=true&tls=" + tlsConfigName
	if dsn != want {
		t.Errorf("dsn = %q, want %q", dsn, want)
	}
}

func TestTranslateDuplicateEntry(t *testing.T) {
	duplicateErr := &mysql.MySQLError{Number: 1062, Message: "duplicate entry"}
	if err := translateDuplicateEntry(duplicateErr); !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("expected ErrDuplicateName, got %v", err)
	}

	otherErr := errors.New("other database error")
	if err := translateDuplicateEntry(otherErr); !errors.Is(err, otherErr) {
		t.Fatalf("expected original error, got %v", err)
	}
}

func TestMemStoreRejectsDuplicateEntityIDs(t *testing.T) {
	ctx := context.Background()
	store := NewMemStore()

	tests := []struct {
		name   string
		create func(string) error
	}{
		{name: "organization", create: func(id string) error {
			return store.CreateOrg(ctx, OrgRow{ID: id, Name: "Organization"})
		}},
		{name: "product", create: func(id string) error {
			return store.CreateProduct(ctx, ProductRow{ID: id, Name: "Product"})
		}},
		{name: "team", create: func(id string) error {
			return store.CreateTeam(ctx, TeamRow{ID: id, Name: "Team"})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.create("Existing-ID"); err != nil {
				t.Fatalf("create initial entity: %v", err)
			}
			if err := tt.create("existing-id"); !errors.Is(err, ErrDuplicateName) {
				t.Fatalf("expected ErrDuplicateName, got %v", err)
			}
		})
	}
}
