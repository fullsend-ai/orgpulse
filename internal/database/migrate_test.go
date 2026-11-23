package database

import (
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/fullsend-ai/orgpulse/internal/database/migrations"
)

func TestMigrations_SequentialAndValid(t *testing.T) {
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("mysql"); err != nil {
		t.Fatalf("set dialect: %v", err)
	}

	migrationsList, err := goose.CollectMigrations(".", 0, goose.MaxVersion)
	if err != nil {
		t.Fatalf("collect migrations failed: %v", err)
	}

	if len(migrationsList) != 20 {
		t.Fatalf("expected 20 migrations, got %d", len(migrationsList))
	}

	for i, m := range migrationsList {
		expectedVersion := int64(i + 1)
		if m.Version != expectedVersion {
			t.Errorf("migration %d has version %d, expected %d", i, m.Version, expectedVersion)
		}
		if m.UseTx {
			t.Errorf("migration %d (%s) should have NO TRANSACTION (+goose NO TRANSACTION)", i, m.Source)
		}
	}
}
