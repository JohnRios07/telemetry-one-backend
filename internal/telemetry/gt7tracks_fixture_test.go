package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGT7TracksFixturesValidateOffline(t *testing.T) {
	for _, name := range []string{"watkins_glen_length_only.json", "ambiguous_3664_length_only.json"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "..", "testdata", "fixtures", "gt7tracks", name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			var request IngestBatchRequest
			if err := json.Unmarshal(data, &request); err != nil {
				t.Fatalf("unmarshal fixture: %v", err)
			}
			if err := request.Validate(); err != nil {
				t.Fatalf("fixture should validate: %v", err)
			}
			if len(request.Frames) == 0 {
				t.Fatalf("fixture should contain frames")
			}
		})
	}
}
