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

func TestSessionsLifecycleMigrationIsIdempotent(t *testing.T) {
	content, err := os.ReadFile("../../migrations/000002_sessions_lifecycle.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	migration := string(content)
	for _, expected := range []string{
		"CREATE TABLE IF NOT EXISTS sessions",
		"CREATE INDEX IF NOT EXISTS sessions_track_id_idx",
		"CREATE INDEX IF NOT EXISTS sessions_started_at_idx",
		"CREATE INDEX IF NOT EXISTS sessions_status_idx",
	} {
		if !strings.Contains(migration, expected) {
			t.Fatalf("expected migration to contain %q", expected)
		}
	}
}

func TestRuntimePersistenceMigrationIsIdempotentAndDoesNotRequireBackendSession(t *testing.T) {
	content, err := os.ReadFile("../../migrations/000003_runtime_persistence.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	migration := string(content)
	for _, expected := range []string{
		"CREATE TABLE IF NOT EXISTS frame_batches",
		"CREATE UNIQUE INDEX IF NOT EXISTS frame_batches_session_order_idx",
		"CREATE TABLE IF NOT EXISTS ai_audit_logs",
		"CREATE INDEX IF NOT EXISTS ai_audit_logs_session_id_idx",
		"ALTER TABLE engineer_events DROP CONSTRAINT IF EXISTS engineer_events_session_id_fkey",
	} {
		if !strings.Contains(migration, expected) {
			t.Fatalf("expected migration to contain %q", expected)
		}
	}

	frameBatchesStart := strings.Index(migration, "CREATE TABLE IF NOT EXISTS frame_batches")
	frameBatchesEnd := strings.Index(migration, "CREATE INDEX IF NOT EXISTS frame_batches_session_id_idx")
	if frameBatchesStart < 0 || frameBatchesEnd < 0 || frameBatchesEnd <= frameBatchesStart {
		t.Fatalf("expected frame_batches table definition before indexes")
	}
	frameBatchesDDL := migration[frameBatchesStart:frameBatchesEnd]
	if strings.Contains(frameBatchesDDL, "REFERENCES sessions") {
		t.Fatalf("frame_batches must not enforce backend session existence before Flutter session mapping")
	}
}
