package tracks

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"telemetry-one-backend/internal/telemetry"
)

func TestDetectTrackUnknownWhenInsufficientFrames(t *testing.T) {
	result := DetectTrack([]telemetry.Frame{trackFrame(1, 0, 0)}, OfficialGT7SeedCatalog(), DetectionOptions{})

	if result.Status != DetectionStatusPending {
		t.Fatalf("expected pending status, got %+v", result)
	}
	if result.TrackID != nil || result.LayoutID != nil || result.Confidence != 0 {
		t.Fatalf("expected empty ids and zero confidence for unknown result, got %+v", result)
	}
	if len(result.Evidence) != 1 || result.Evidence[0].Reason != DetectionReasonInsufficientData || result.NextAction != DetectionNextActionCollectMoreFrames {
		t.Fatalf("expected insufficient frame evidence, got %+v", result.Evidence)
	}
}

func TestDetectTrackPendingWhenNoCompletedLap(t *testing.T) {
	frames := straightIncompleteLapFrames(5423, 25)

	result := DetectTrack(frames, OfficialGT7SeedCatalog(), DetectionOptions{})

	if result.Status != DetectionStatusPending || result.NextAction != DetectionNextActionWaitCompletedLap {
		t.Fatalf("expected pending wait-for-lap response, got %+v", result)
	}
	if len(result.Reasons) != 1 || result.Reasons[0] != DetectionReasonNoCompletedLap {
		t.Fatalf("expected no completed lap reason, got %+v", result.Reasons)
	}
}

func TestDetectTrackMatchesObservedLapLengthWithinTolerance(t *testing.T) {
	frames := straightCompletedLapFrames(5423, 32)

	result := DetectTrack(frames, OfficialGT7SeedCatalog(), DetectionOptions{})

	if result.Status != DetectionStatusDetected {
		t.Fatalf("expected detected status, got %+v", result)
	}
	if result.TrackID == nil || *result.TrackID != "gt7_watkins_glen_international" || result.LayoutID == nil || *result.LayoutID != "gt7_watkins_glen_long_course" {
		t.Fatalf("unexpected match ids: %+v", result)
	}
	if result.TrackName == nil || *result.TrackName != "Watkins Glen International" || result.LayoutName == nil || *result.LayoutName != "Watkins Glen Long Course" {
		t.Fatalf("expected names to come from catalog, got %+v", result)
	}
	if len(result.Reasons) != 1 || result.Reasons[0] != DetectionReasonLengthMatch || result.NextAction != DetectionNextActionUseDetectedLayout {
		t.Fatalf("expected stable detected reason and next action, got %+v", result)
	}
	if result.Confidence < 0.84 || result.Confidence > 0.851 {
		t.Fatalf("expected conservative length-only confidence, got %f", result.Confidence)
	}
}

func TestDetectTrackSelectedNamesAreResolvedFromProvenanceBackedCatalog(t *testing.T) {
	catalog := Catalog{
		CatalogVersion: CatalogVersionV1,
		Tracks: []CatalogTrack{{
			ID:      "catalog_track",
			Name:    "Catalog Track Name",
			Sources: []Source{officialNewsSource("https://www.gran-turismo.com/us/news/00_5302315.html")},
			Layouts: []CatalogLayout{{
				ID:           "catalog_layout",
				Name:         "Catalog Layout Name",
				LengthMeters: 1000,
				Sources:      []Source{officialNewsSource("https://www.gran-turismo.com/us/news/00_5302315.html")},
				Sectors:      []Sector{},
				Corners:      []Corner{},
			}},
		}},
	}

	result := DetectTrack(straightCompletedLapFrames(1000, 25), catalog, DetectionOptions{})

	if result.Status != DetectionStatusDetected {
		t.Fatalf("expected detected status, got %+v", result)
	}
	if result.TrackName == nil || *result.TrackName != "Catalog Track Name" || result.LayoutName == nil || *result.LayoutName != "Catalog Layout Name" {
		t.Fatalf("expected selected names to resolve from catalog metadata, got %+v", result)
	}
}

func TestDetectTrackDoesNotSelectUnsourcedCatalogNames(t *testing.T) {
	catalog := Catalog{
		CatalogVersion: CatalogVersionV1,
		Tracks: []CatalogTrack{{
			ID:   "unsourced_track",
			Name: "Unsourced Track Name",
			Layouts: []CatalogLayout{{
				ID:           "unsourced_layout",
				Name:         "Unsourced Layout Name",
				LengthMeters: 1000,
				Sectors:      []Sector{},
				Corners:      []Corner{},
			}},
		}},
	}

	result := DetectTrack(straightCompletedLapFrames(1000, 25), catalog, DetectionOptions{})

	if result.Status != DetectionStatusUnknown {
		t.Fatalf("expected unsourced catalog metadata not to be selected, got %+v", result)
	}
	if result.TrackID != nil || result.LayoutID != nil || result.TrackName != nil || result.LayoutName != nil {
		t.Fatalf("expected no selected ids or names for unsourced metadata, got %+v", result)
	}
}

func TestDetectTrackDoesNotEmitSectorOrCornerNamesWhenCatalogLacksThem(t *testing.T) {
	result := DetectTrack(straightCompletedLapFrames(5423, 32), OfficialGT7SeedCatalog(), DetectionOptions{})
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("failed to marshal detection result: %v", err)
	}

	body := string(encoded)
	if strings.Contains(body, "sectorName") || strings.Contains(body, "cornerName") || strings.Contains(body, "sectors") || strings.Contains(body, "corners") {
		t.Fatalf("expected detection result not to emit unsourced sector/corner names, got %s", body)
	}
}

func TestDetectTrackReturnsAmbiguousStatusForSimilarLengths(t *testing.T) {
	source := officialNewsSource("https://www.gran-turismo.com/us/news/00_5302315.html")
	catalog := Catalog{
		CatalogVersion: CatalogVersionV1,
		Tracks: []CatalogTrack{
			{ID: "track_a", Name: "Track A", Sources: []Source{source}, Layouts: []CatalogLayout{{ID: "layout_a", Name: "Layout A", LengthMeters: 1000, Sources: []Source{source}, Sectors: []Sector{}, Corners: []Corner{}}}},
			{ID: "track_b", Name: "Track B", Sources: []Source{source}, Layouts: []CatalogLayout{{ID: "layout_b", Name: "Layout B", LengthMeters: 1001, Sources: []Source{source}, Sectors: []Sector{}, Corners: []Corner{}}}},
		},
	}

	result := DetectTrack(straightCompletedLapFrames(1000, 25), catalog, DetectionOptions{})

	if result.Status != DetectionStatusAmbiguous {
		t.Fatalf("expected ambiguous result, got %+v", result)
	}
	if result.TrackID != nil || result.LayoutID != nil || result.Confidence != 0 || result.NextAction != DetectionNextActionManualSelection {
		t.Fatalf("expected no selected ids for ambiguous unknown, got %+v", result)
	}
	if result.TrackName != nil || result.LayoutName != nil {
		t.Fatalf("expected candidate evidence not to become selected names, got %+v", result)
	}
	if len(result.Evidence) != 1 || result.Evidence[0].Reason != DetectionReasonAmbiguousLength {
		t.Fatalf("expected ambiguous length evidence, got %+v", result.Evidence)
	}
}

func TestDetectTrackReturnsLowConfidenceFallbackBelowMinimum(t *testing.T) {
	result := DetectTrack(straightCompletedLapFrames(5423, 32), OfficialGT7SeedCatalog(), DetectionOptions{MinDetectedConfidence: 0.86})

	if result.Status != DetectionStatusLowConfidence {
		t.Fatalf("expected low confidence result, got %+v", result)
	}
	if result.TrackID != nil || result.LayoutID != nil || result.Confidence < 0.84 || result.NextAction != DetectionNextActionManualSelection {
		t.Fatalf("expected unselected low confidence fallback preserving evidence confidence, got %+v", result)
	}
	if len(result.Reasons) != 1 || result.Reasons[0] != DetectionReasonLowConfidence {
		t.Fatalf("expected low confidence reason, got %+v", result.Reasons)
	}
}

func TestDetectTrackDoesNotInventNamesForUnknown(t *testing.T) {
	result := DetectTrack(straightCompletedLapFrames(7000, 32), OfficialGT7SeedCatalog(), DetectionOptions{})

	if result.Status != DetectionStatusUnknown {
		t.Fatalf("expected unknown status, got %+v", result)
	}
	if result.TrackName != nil || result.LayoutName != nil {
		t.Fatalf("expected unknown result not to include fake names, got %+v", result)
	}
}

func TestDetectTrackOutputIsDeterministic(t *testing.T) {
	frames := straightCompletedLapFrames(5281, 32)
	first := DetectTrack(frames, OfficialGT7SeedCatalog(), DetectionOptions{})
	second := DetectTrack(frames, OfficialGT7SeedCatalog(), DetectionOptions{})

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected deterministic result, first=%+v second=%+v", first, second)
	}
}

func TestOfficialGT7SeedCatalogMatchesFixture(t *testing.T) {
	fixture := loadCatalogFixture(t, "../../testdata/catalogs/gt7_official_seed_catalog.json")
	seed := OfficialGT7SeedCatalog()

	if err := seed.Validate(); err != nil {
		t.Fatalf("expected embedded official seed to validate, got %v", err)
	}
	if len(seed.Tracks) != len(fixture.Tracks) {
		t.Fatalf("expected seed track count to match fixture")
	}
	for i, track := range seed.Tracks {
		fixtureTrack := fixture.Tracks[i]
		if track.ID != fixtureTrack.ID || track.Name != fixtureTrack.Name || track.Country != fixtureTrack.Country || len(track.Layouts) != len(fixtureTrack.Layouts) {
			t.Fatalf("seed track differs from fixture: got %+v want %+v", track, fixtureTrack)
		}
		for j, layout := range track.Layouts {
			fixtureLayout := fixtureTrack.Layouts[j]
			if layout.ID != fixtureLayout.ID || layout.Name != fixtureLayout.Name || layout.LengthMeters != fixtureLayout.LengthMeters || len(layout.Sectors) != 0 || len(layout.Corners) != 0 {
				t.Fatalf("seed layout differs from fixture: got %+v want %+v", layout, fixtureLayout)
			}
		}
	}
}

func straightCompletedLapFrames(lengthMeters float64, count int) []telemetry.Frame {
	frames := make([]telemetry.Frame, 0, count)
	lastIndex := count - 1
	for i := 0; i < count; i++ {
		lapNumber := 1
		if i == lastIndex {
			lapNumber = 2
		}
		positionX := lengthMeters * float64(i) / float64(lastIndex)
		frames = append(frames, trackFrame(lapNumber, int64(i), positionX))
	}

	return frames
}

func straightIncompleteLapFrames(lengthMeters float64, count int) []telemetry.Frame {
	frames := make([]telemetry.Frame, 0, count)
	lastIndex := count - 1
	for i := 0; i < count; i++ {
		positionX := lengthMeters * float64(i) / float64(lastIndex)
		frames = append(frames, trackFrame(1, int64(i), positionX))
	}

	return frames
}

func trackFrame(lapNumber int, timestampUnixMs int64, positionX float64) telemetry.Frame {
	return telemetry.Frame{
		TimestampUnixMs: timestampUnixMs + 1,
		PositionX:       positionX,
		LapNumber:       lapNumber,
		IsOnTrack:       true,
	}
}
