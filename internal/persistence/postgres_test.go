package persistence

import (
	"os"
	"strings"
	"testing"
)

func TestDefaultStorageDecision(t *testing.T) {
	decision := DefaultStorageDecision()
	if decision.Driver != DriverPostgres {
		t.Fatalf("expected driver %q, got %q", DriverPostgres, decision.Driver)
	}
	if decision.Reason == "" {
		t.Fatal("expected a storage decision reason")
	}
}

func TestInitialMigrationDefinesPhase14Tables(t *testing.T) {
	content, err := os.ReadFile("../../migrations/000001_initial_persistence.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	migration := string(content)
	for _, table := range []string{"sessions", "tracks", "corners", "engineer_events"} {
		if !strings.Contains(migration, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("expected migration to define %s", table)
		}
	}
}
