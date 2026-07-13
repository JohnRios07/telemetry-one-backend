package migrations

import "testing"

func TestUpFilesIncludesSessionsLifecycleMigrationInOrder(t *testing.T) {
	files, err := UpFiles()
	if err != nil {
		t.Fatalf("expected up files: %v", err)
	}

	want := []string{"000001_initial_persistence.up.sql", "000002_sessions_lifecycle.up.sql"}
	if len(files) != len(want) {
		t.Fatalf("expected %d files, got %d: %v", len(want), len(files), files)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Fatalf("expected file %d to be %q, got %q", i, want[i], files[i])
		}
	}
}
