package sim

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestGenerateBatchIsDeterministicAndValid(t *testing.T) {
	options := Options{SessionID: "session-test", FrequencyHz: 20, LapDuration: time.Second}

	first, err := GenerateBatch(options)
	if err != nil {
		t.Fatalf("GenerateBatch returned error: %v", err)
	}
	second, err := GenerateBatch(options)
	if err != nil {
		t.Fatalf("GenerateBatch returned error: %v", err)
	}

	if err := first.Validate(); err != nil {
		t.Fatalf("generated batch should validate: %v", err)
	}
	if len(first.Frames) != 21 {
		t.Fatalf("expected 21 frames for one second at 20 Hz, got %d", len(first.Frames))
	}
	if !reflect.DeepEqual(first.Frames[10], second.Frames[10]) {
		t.Fatalf("expected deterministic frame at index 10")
	}
	if first.Frames[0].TimestampUnixMs != DefaultStartUnixMs {
		t.Fatalf("expected default start timestamp %d, got %d", DefaultStartUnixMs, first.Frames[0].TimestampUnixMs)
	}
	if first.Frames[len(first.Frames)-1].CurrentLapMs != int64(time.Second/time.Millisecond) {
		t.Fatalf("expected final lap time to match requested duration")
	}
}

func TestGenerateBatchRejectsUnsupportedFrequency(t *testing.T) {
	_, err := GenerateBatch(Options{FrequencyHz: 10})
	if err != ErrInvalidFrequency {
		t.Fatalf("expected ErrInvalidFrequency, got %v", err)
	}
}

func TestGenerateBatchRejectsPayloadsAboveBatchLimit(t *testing.T) {
	_, err := GenerateBatch(Options{FrequencyHz: 60, LapDuration: 10 * time.Second})
	if err == nil {
		t.Fatalf("expected duration above batch limit to return an error")
	}
}

func TestLoadFixtureValidatesSyntheticBatch(t *testing.T) {
	path := filepath.Join("..", "..", "..", "testdata", "fixtures", "synthetic_dev_loop_20hz.json")

	batch, err := LoadFixture(path)
	if err != nil {
		t.Fatalf("LoadFixture returned error: %v", err)
	}
	if batch.SessionID != DefaultSessionID {
		t.Fatalf("expected session id %q, got %q", DefaultSessionID, batch.SessionID)
	}
	if len(batch.Frames) == 0 {
		t.Fatalf("expected fixture frames")
	}
}
