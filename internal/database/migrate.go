package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"

	"github.com/fullsend-ai/orgpulse/internal/database/migrations"
)

// Migrate opens a connection with the same parameters as NewMySQLStore and
// applies all pending goose migrations from internal/database/migrations.
// This is the only place schema changes are applied. NewMySQLStore assumes
// the schema is already current -- Migrate must run (via cmd/migrate, as a
// pre-deploy Job) before any app instance starts against a new schema
// version.
func Migrate(ctx context.Context, host string, port int, user, password, dbName, tlsCABundlePath string) error {
	db, err := OpenDB(host, port, user, password, dbName, tlsCABundlePath)
	if err != nil {
		return err
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("mysql"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}

	// If the database predates Goose (baseline tables exist but no
	// goose_db_version table), stamp migration 00001 as already applied
	// so only newer migrations run.
	if err := stampBaselineIfNeeded(ctx, db); err != nil {
		return fmt.Errorf("stamp baseline: %w", err)
	}

	if err := reconcileDirtyStateIfNeeded(ctx, db); err != nil {
		return fmt.Errorf("reconcile dirty state: %w", err)
	}

	if err := goose.UpContext(ctx, db, "."); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

func reconcileDirtyStateIfNeeded(ctx context.Context, db *sql.DB) error {
	ver, err := goose.GetDBVersionContext(ctx, db)
	if err != nil {
		return fmt.Errorf("get goose db version: %w", err)
	}
	if ver != 2 {
		return nil
	}

	// Check whether stored_procedures table exists.
	var tableCount int
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'stored_procedures'").
		Scan(&tableCount)
	if err != nil {
		return fmt.Errorf("check stored_procedures table: %w", err)
	}
	if tableCount == 0 {
		return nil
	}

	// Check whether workflow_id column exists.
	var colCount int
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'stored_procedures' AND column_name = 'workflow_id'").
		Scan(&colCount)
	if err != nil {
		return fmt.Errorf("check workflow_id column: %w", err)
	}
	if colCount == 0 {
		return nil
	}

	// Drop foreign key fk_sp_workflow if it exists.
	var fkCount int
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM information_schema.table_constraints WHERE table_schema = DATABASE() AND table_name = 'stored_procedures' AND constraint_name = 'fk_sp_workflow'").
		Scan(&fkCount)
	if err != nil {
		return fmt.Errorf("check fk_sp_workflow: %w", err)
	}
	if fkCount > 0 {
		if _, err := db.ExecContext(ctx, "ALTER TABLE stored_procedures DROP FOREIGN KEY fk_sp_workflow"); err != nil {
			return fmt.Errorf("drop fk_sp_workflow: %w", err)
		}
	}

	// Drop composite unique index uq_sp_workflow_name if it exists.
	var uqIdxCount int
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'stored_procedures' AND index_name = 'uq_sp_workflow_name'").
		Scan(&uqIdxCount)
	if err != nil {
		return fmt.Errorf("check uq_sp_workflow_name index: %w", err)
	}
	if uqIdxCount > 0 {
		if _, err := db.ExecContext(ctx, "ALTER TABLE stored_procedures DROP INDEX uq_sp_workflow_name"); err != nil {
			return fmt.Errorf("drop uq_sp_workflow_name index: %w", err)
		}
	}

	// Drop column workflow_id.
	if _, err := db.ExecContext(ctx, "ALTER TABLE stored_procedures DROP COLUMN workflow_id"); err != nil {
		return fmt.Errorf("drop workflow_id column: %w", err)
	}

	// Ensure unique index 'name' exists if it was dropped during a partial migration.
	var nameIdxCount int
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = 'stored_procedures' AND index_name = 'name'").
		Scan(&nameIdxCount)
	if err != nil {
		return fmt.Errorf("check name index: %w", err)
	}
	if nameIdxCount == 0 {
		if _, err := db.ExecContext(ctx, "ALTER TABLE stored_procedures ADD UNIQUE INDEX name (name)"); err != nil {
			return fmt.Errorf("restore name index: %w", err)
		}
	}

	return nil
}

func stampBaselineIfNeeded(ctx context.Context, db *sql.DB) error {
	ver, err := goose.EnsureDBVersionContext(ctx, db)
	if err != nil {
		return fmt.Errorf("ensure goose version table: %w", err)
	}

	// Check whether the baseline schema is present (pick any table from 00001).
	var hasOrgs int
	err = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'orgs'").
		Scan(&hasOrgs)
	if err != nil {
		return err
	}
	if hasOrgs == 0 {
		return nil // Fresh database; let Goose run everything normally.
	}

	// Detect the highest migration already applied in the schema by
	// probing for artifacts introduced by each migration. This handles
	// databases whose schema was applied outside of Goose.
	probes := []struct {
		version int64
		query   string
	}{
		{1, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'orgs'"},
		{2, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'stored_procedures'"},
		{3, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'canned_queries'"},
		{4, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'canned_scripts'"},
		{5, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'workflows'"},
		{6, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'workflow_queries'"},
		{7, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'workflow_versions'"},
		{8, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'stored_procedures' AND column_name = 'workflow_id'"},
		{9, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'workflows' AND column_name = 'tests'"},
		{10, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'workflow_versions' AND column_name = 'tests'"},
		{11, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'workflows' AND column_name = 'archived'"},
		{12, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'json_schemas'"},
		{13, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'api_keys' AND column_name = 'scoped_user'"},
		{14, "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'workitem_projects' AND column_name = 'board_id'"},
	}

	var highestPresent int64
	for _, p := range probes {
		var n int
		if err := db.QueryRowContext(ctx, p.query).Scan(&n); err != nil {
			return fmt.Errorf("probe migration %d: %w", p.version, err)
		}
		if n > 0 {
			highestPresent = p.version
		} else {
			break
		}
	}

	if highestPresent <= ver {
		return nil // Goose is already caught up.
	}

	for v := ver + 1; v <= highestPresent; v++ {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, true)", v); err != nil {
			return fmt.Errorf("stamp version %d: %w", v, err)
		}
	}
	return nil
}
