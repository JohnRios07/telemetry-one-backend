package corners

import (
	"errors"
	"math"
	"testing"

	"telemetry-one-backend/internal/tracks"
)

func TestResolveCurrentReturnsCatalogCornerForDistanceInsideRange(t *testing.T) {
	got, err := ResolveCurrent(testLayout(), 125, true)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got.Status != StatusInCorner || got.CornerID != "synthetic_dev_loop_t1" || got.CornerName != "Synthetic Turn 1" {
		t.Fatalf("expected catalog-owned corner, got %+v", got)
	}
}

func TestResolveCurrentReturnsNoCornerOutsideRanges(t *testing.T) {
	got, err := ResolveCurrent(testLayout(), 250, true)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if got.Status != StatusNoCorner || got.Reason != ReasonOutsideCornerRange || got.CornerID != "" || got.CornerName != "" {
		t.Fatalf("expected explicit no_corner outside ranges, got %+v", got)
	}
}

func TestResolveCurrentUsesStartInclusiveEndExclusiveBoundaries(t *testing.T) {
	layout := testLayout()

	start, err := ResolveCurrent(layout, 100, true)
	if err != nil {
		t.Fatalf("expected no error at start, got %v", err)
	}
	if start.Status != StatusInCorner || start.CornerID != "synthetic_dev_loop_t1" {
		t.Fatalf("expected start boundary to be in corner, got %+v", start)
	}

	end, err := ResolveCurrent(layout, 190, true)
	if err != nil {
		t.Fatalf("expected no error at end, got %v", err)
	}
	if end.Status != StatusNoCorner || end.Reason != ReasonOutsideCornerRange {
		t.Fatalf("expected end boundary to be outside corner, got %+v", end)
	}
}

func TestResolveCurrentReturnsNoCornerWhenOfficialSeedHasNoCorners(t *testing.T) {
	catalog := tracks.OfficialGT7SeedCatalog()
	layout := catalog.Tracks[0].Layouts[0]

	got, err := ResolveCurrent(layout, 100, true)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Status != StatusNoCorner || got.Reason != ReasonCatalogHasNoCorners || got.CornerID != "" || got.CornerName != "" {
		t.Fatalf("expected official seed to resolve no_corner without names, got %+v", got)
	}
}

func TestResolveCurrentSupportsClosedLoopWrapAroundCorner(t *testing.T) {
	layout := testLayout()
	layout.Corners = append(layout.Corners, tracks.Corner{
		ID:             "synthetic_dev_loop_final",
		Number:         99,
		Name:           "Synthetic Final Wrap",
		DefinitionMode: tracks.CornerDefinitionCatalogManual,
		StartMeters:    1250,
		ApexMeters:     20,
		EndMeters:      80,
	})

	for _, distance := range []float64{1260, 20, 1320} {
		got, err := ResolveCurrent(layout, distance, true)
		if err != nil {
			t.Fatalf("expected no error for distance %.1f, got %v", distance, err)
		}
		if got.Status != StatusInCorner || got.CornerID != "synthetic_dev_loop_final" || got.CornerName != "Synthetic Final Wrap" {
			t.Fatalf("expected wrap-around corner for distance %.1f, got %+v", distance, got)
		}
	}

	got, err := ResolveCurrent(layout, 20, false)
	if err != nil {
		t.Fatalf("expected no error for open layout, got %v", err)
	}
	if got.Status != StatusNoCorner {
		t.Fatalf("expected open layout not to match wrap-around corner, got %+v", got)
	}
}

func TestResolveCurrentDoesNotUseFutureAutomaticCornerDefinitions(t *testing.T) {
	layout := testLayout()
	layout.Corners = []tracks.Corner{
		{ID: "synthetic_dev_loop_auto_t1", Number: 1, Name: "Synthetic Auto Turn 1", DefinitionMode: tracks.CornerDefinitionAutoDetectedFuture, StartMeters: 100, ApexMeters: 140, EndMeters: 190},
	}

	got, err := ResolveCurrent(layout, 125, true)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got.Status != StatusNoCorner || got.Reason != ReasonUnsupportedCornerDefinition || got.CornerID != "" || got.CornerName != "" {
		t.Fatalf("expected unsupported future automatic corner definition to stay non-authoritative, got %+v", got)
	}
}

func TestResolveCurrentRejectsInvalidInputs(t *testing.T) {
	if _, err := ResolveCurrent(tracks.CatalogLayout{LengthMeters: 0}, 10, true); !errors.Is(err, ErrInvalidLength) {
		t.Fatalf("expected invalid length error, got %v", err)
	}
	if _, err := ResolveCurrent(testLayout(), math.NaN(), true); !errors.Is(err, ErrInvalidDistance) {
		t.Fatalf("expected invalid distance error, got %v", err)
	}
}

func TestResolveCurrentLeavesProvenanceToCatalogSourceOfTruthValidation(t *testing.T) {
	catalog := tracks.Catalog{
		CatalogVersion: tracks.CatalogVersionV1,
		Tracks: []tracks.CatalogTrack{
			{
				ID:      "synthetic_dev_track",
				Name:    "Synthetic Dev Track",
				Layouts: []tracks.CatalogLayout{testLayout()},
			},
		},
	}

	if err := catalog.Validate(); err != nil {
		t.Fatalf("expected synthetic structural catalog to validate, got %v", err)
	}
	if err := catalog.ValidateSourceOfTruth(); !errors.Is(err, tracks.ErrMissingMetadataSource) {
		t.Fatalf("expected source-of-truth validation to require provenance, got %v", err)
	}
}

func testLayout() tracks.CatalogLayout {
	return tracks.CatalogLayout{
		ID:           "synthetic_dev_loop",
		Name:         "Synthetic Dev Loop",
		LengthMeters: 1300,
		Corners: []tracks.Corner{
			{ID: "synthetic_dev_loop_t1", Number: 1, Name: "Synthetic Turn 1", DefinitionMode: tracks.CornerDefinitionCatalogManual, StartMeters: 100, ApexMeters: 140, EndMeters: 190},
			{ID: "synthetic_dev_loop_t2", Number: 2, Name: "Synthetic Turn 2", DefinitionMode: tracks.CornerDefinitionCatalogManual, StartMeters: 520, ApexMeters: 570, EndMeters: 640},
		},
	}
}
