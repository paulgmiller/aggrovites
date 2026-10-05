package main

import (
	"path/filepath"
	"testing"
)

func TestLocalSQLiteDestination(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("SQLLITE_FILE", filepath.Join(t.TempDir(), "local.db"))
	db, err := openDestination()
	if err != nil {
		t.Fatal(err)
	}
	if db.Dialector.Name() != "sqlite" {
		t.Fatalf("expected SQLite, got %s", db.Dialector.Name())
	}
	if err := migrateSchema(db); err != nil {
		t.Fatal(err)
	}
}
