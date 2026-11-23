//go:build e2e

package database

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/fullsend-ai/orgpulse/internal/database/migrations"
)

// TestE2E_Migrate_AppliesSchemaIdempotently proves Migrate both creates the
// full schema on a fresh database and is safe to run again (goose tracks
// applied versions in goose_db_version, so a second run must be a no-op, not
// an error) -- the property a pre-deploy Job re-running on a retry depends on.
func TestE2E_Migrate_AppliesSchemaIdempotently(t *testing.T) {
	host, port, user, password, dbName := e2eConfig(t)
	caBundle := filepath.Join(testdataTLSDir, "ca.pem")
	ctx := context.Background()

	if err := Migrate(ctx, host, port, user, password, dbName, caBundle); err != nil {
		t.Fatalf("first migrate run failed: %v", err)
	}
	if err := Migrate(ctx, host, port, user, password, dbName, caBundle); err != nil {
		t.Fatalf("second migrate run (idempotency check) failed: %v", err)
	}

	store, err := NewMySQLStore(host, port, user, password, dbName, caBundle)
	if err != nil {
		t.Fatalf("connect after migrate: %v", err)
	}
	defer store.Close()

	var count int
	if err := store.rawDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM orgs").Scan(&count); err != nil {
		t.Fatalf("expected orgs table to exist after migration, query failed: %v", err)
	}
	if err := store.rawDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM workflows").Scan(&count); err != nil {
		t.Fatalf("expected workflows table to exist after migration, query failed: %v", err)
	}
}

// TestE2E_Migrate_ReconcilesDirtyStateAtVersion2 proves that if migration 00008
// was partially applied (stored_procedures has workflow_id column while goose
// db version is 2), Migrate reconciles the dirty schema and completes all migrations.
func TestE2E_Migrate_ReconcilesDirtyStateAtVersion2(t *testing.T) {
	host, port, user, password, dbName := e2eConfig(t)
	caBundle := filepath.Join(testdataTLSDir, "ca.pem")
	ctx := context.Background()

	db, err := OpenDB(host, port, user, password, dbName, caBundle)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("mysql"); err != nil {
		t.Fatalf("set goose dialect: %v", err)
	}

	// Reset goose migrations down to 0, then migrate up to version 2.
	_ = goose.DownTo(db, ".", 0)
	if err := goose.UpToContext(ctx, db, ".", 2); err != nil {
		t.Fatalf("migrate up to version 2: %v", err)
	}

	// Simulate partially applied migration:
	// Add workflow_id column and index while version is 2.
	if _, err := db.ExecContext(ctx, "ALTER TABLE stored_procedures ADD COLUMN workflow_id INT NULL AFTER id, DROP INDEX name, ADD UNIQUE INDEX uq_sp_workflow_name (workflow_id, name)"); err != nil {
		t.Fatalf("simulate dirty state: %v", err)
	}

	// Call Migrate, which should reconcile the dirty state and apply all migrations to latest.
	if err := Migrate(ctx, host, port, user, password, dbName, caBundle); err != nil {
		t.Fatalf("migrate with dirty state failed: %v", err)
	}

	// Verify that database version is at latest.
	currentVer, err := goose.GetDBVersionContext(ctx, db)
	if err != nil {
		t.Fatalf("get db version: %v", err)
	}
	migrationsList, err := goose.CollectMigrations(".", 0, goose.MaxVersion)
	if err != nil {
		t.Fatalf("collect migrations: %v", err)
	}
	latestVer := migrationsList[len(migrationsList)-1].Version
	if currentVer != latestVer {
		t.Fatalf("expected version %d after migration, got %d", latestVer, currentVer)
	}

	// Verify stored_procedures table has workflow_id column.
	var colCount int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'stored_procedures' AND column_name = 'workflow_id'").
		Scan(&colCount); err != nil {
		t.Fatalf("check workflow_id column: %v", err)
	}
	if colCount != 1 {
		t.Fatalf("expected workflow_id column in stored_procedures, got %d", colCount)
	}
}
