package tracks

import (
	"testing"

	"telemetry-one-backend/internal/telemetry"
)

func TestDetectTrackAllFramesOffTrack(t *testing.T) {
	frames := make([]telemetry.Frame, 25)
	lastIndex := 24
	for i := 0; i < 25; i++ {
		lapNumber := 1
		if i == lastIndex {
			lapNumber = 2
		}
		frames[i] = trackFrame(lapNumber, int64(i), float64(i))
		frames[i].IsOnTrack = false
	}

	result := DetectTrack(frames, OfficialGT7SeedCatalog(), DetectionOptions{})

	if result.Status != DetectionStatusPending {
		t.Fatalf("expected pending status for all off-track frames, got %+v", result)
	}
	if len(result.Reasons) != 1 || result.Reasons[0] != DetectionReasonNoCompletedLap {
		t.Fatalf("expected no_completed_lap reason, got %+v", result.Reasons)
	}
	if result.NextAction != DetectionNextActionWaitCompletedLap {
		t.Fatalf("expected wait_for_completed_lap next action, got %q", result.NextAction)
	}
}

func TestDetectTrackMinCompletedLapFramesBoundary(t *testing.T) {
	frames := straightCompletedLapFrames(5423, 20)

	result := DetectTrack(frames, OfficialGT7SeedCatalog(), DetectionOptions{})

	if result.Status != DetectionStatusDetected {
		t.Fatalf("expected detected at exact min frames boundary, got %+v", result)
	}
	if result.TrackID == nil || *result.TrackID != "gt7_watkins_glen_international" {
		t.Fatalf("expected watkins glen match at boundary, got %+v", result.TrackID)
	}
}

func TestDetectTrackJustBelowMinCompletedLapFrames(t *testing.T) {
	frames := straightIncompleteLapFrames(5423, 18)
	// Add one more frame that changes lap to simulate almost completing
	// Total = 19 frames, just below the default MinCompletedLapFrames (20)
	frames = append(frames, trackFrame(2, 18, 5423))

	result := DetectTrack(frames, OfficialGT7SeedCatalog(), DetectionOptions{})

	if result.Status != DetectionStatusPending {
		t.Fatalf("expected pending status below min frames, got %+v", result)
	}
	if len(result.Reasons) != 1 || result.Reasons[0] != DetectionReasonInsufficientData {
		t.Fatalf("expected insufficient_data reason, got %+v", result.Reasons)
	}
	if result.NextAction != DetectionNextActionCollectMoreFrames {
		t.Fatalf("expected collect_more_frames next action, got %q", result.NextAction)
	}
}

func TestDetectTrackCustomDetectionOptions(t *testing.T) {
	source := officialNewsSource("https://www.gran-turismo.com/us/news/00_5302315.html")
	catalog := Catalog{
		CatalogVersion: CatalogVersionV1,
		Tracks: []CatalogTrack{
			{
				ID:      "custom_track",
				Name:    "Custom Track",
				Sources: []Source{source},
				Layouts: []CatalogLayout{
					{
						ID:           "custom_layout",
						Name:         "Custom Layout",
						LengthMeters: 1000,
						Sources:      []Source{source},
						Sectors:      []Sector{},
						Corners:      []Corner{},
					},
				},
			},
		},
	}

	// 10 frames with a lap completion, but default min is 20
	// With custom MinCompletedLapFrames=5 it should detect
	frames := straightCompletedLapFrames(1000, 10)

	opts := DetectionOptions{
		MinCompletedLapFrames: 5,
	}
	result := DetectTrack(frames, catalog, opts)

	if result.Status != DetectionStatusDetected {
		t.Fatalf("expected detected with custom options, got status=%q reasons=%+v", result.Status, result.Reasons)
	}
	if result.TrackID == nil || *result.TrackID != "custom_track" {
		t.Fatalf("expected custom_track, got %+v", result.TrackID)
	}
	if result.LayoutID == nil || *result.LayoutID != "custom_layout" {
		t.Fatalf("expected custom_layout, got %+v", result.LayoutID)
	}
}

func TestDetectTrackAllSamePosition(t *testing.T) {
	frames := make([]telemetry.Frame, 25)
	lastIndex := 24
	for i := 0; i < 25; i++ {
		lapNumber := 1
		if i == lastIndex {
			lapNumber = 2
		}
		frames[i] = trackFrame(lapNumber, int64(i), 0)
	}

	result := DetectTrack(frames, OfficialGT7SeedCatalog(), DetectionOptions{})

	if result.Status != DetectionStatusPending {
		t.Fatalf("expected pending status for no-distance frames, got %+v", result)
	}
	if len(result.Reasons) != 1 || result.Reasons[0] != DetectionReasonNoCompletedLap {
		t.Fatalf("expected no_completed_lap reason when all same position, got %+v", result.Reasons)
	}
	if result.NextAction != DetectionNextActionWaitCompletedLap {
		t.Fatalf("expected wait_for_completed_lap next action, got %q", result.NextAction)
	}
}
