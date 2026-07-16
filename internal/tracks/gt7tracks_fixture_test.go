package tracks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"telemetry-one-backend/internal/telemetry"
)

func TestDetectTrackWithGT7TracksKnownFixture(t *testing.T) {
	request := loadGT7TracksFixture(t, "watkins_glen_length_only.json")

	result := DetectTrack(request.Frames, OfficialGT7SeedCatalog(), DetectionOptions{MinCompletedLapFrames: 4})

	if result.Status != DetectionStatusDetected {
		t.Fatalf("expected detected status, got %+v", result)
	}
	if result.TrackID == nil || *result.TrackID != "gt7_watkins_glen_international" || result.LayoutID == nil || *result.LayoutID != "gt7_layout_1240" {
		t.Fatalf("unexpected detected ids: %+v", result)
	}
}

func TestDetectTrackWithGT7TracksAmbiguousFixtureDoesNotFalsePositive(t *testing.T) {
	request := loadGT7TracksFixture(t, "ambiguous_3664_length_only.json")

	result := DetectTrack(request.Frames, OfficialGT7SeedCatalog(), DetectionOptions{MinCompletedLapFrames: 4})

	if result.Status != DetectionStatusAmbiguous {
		t.Fatalf("expected ambiguous status, got %+v", result)
	}
	if result.TrackID != nil || result.LayoutID != nil || result.TrackName != nil || result.LayoutName != nil {
		t.Fatalf("expected no selected ids or names for ambiguous fixture, got %+v", result)
	}
}

func loadGT7TracksFixture(t *testing.T, name string) telemetry.IngestBatchRequest {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "fixtures", "gt7tracks", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var request telemetry.IngestBatchRequest
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("fixture should validate: %v", err)
	}
	return request
}
