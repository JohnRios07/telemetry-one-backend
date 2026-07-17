package tracks

import "testing"

func TestBuildCatalogTrackLayoutSummaryFiltersNonProductionSources(t *testing.T) {
	prod := Source{URL: "https://example.com/official.js", SourceType: SourceTypeOfficialGranTurismoTracklistAsset, RetrievedAt: "2026-07-17"}
	gt7info := Source{URL: "https://example.com/course.csv", SourceType: SourceTypeGT7InfoCourseCSV, RetrievedAt: "2026-07-17"}
	fixture := Source{URL: "https://example.com/fixture.csv", SourceType: SourceTypeGT7TracksFixtureRawDump, RetrievedAt: "2026-07-17"}

	catalog := Catalog{
		CatalogVersion: CatalogVersionV1,
		Tracks: []CatalogTrack{
			{
				ID:      "track-a",
				Name:    "Track A",
				Sources: []Source{fixture},
				Layouts: []CatalogLayout{{ID: "layout-a", Name: "Layout A", LengthMeters: 1234, Sources: []Source{fixture}, Sectors: []Sector{}, Corners: []Corner{}}},
			},
			{
				ID:      "track-b",
				Name:    "Track B",
				Sources: []Source{prod, fixture},
				Layouts: []CatalogLayout{
					{ID: "layout-b1", Name: "Layout B1", LengthMeters: 3210, Sources: []Source{gt7info, fixture}, Sectors: []Sector{}, Corners: []Corner{}},
					{ID: "layout-b2", Name: "Layout B2", LengthMeters: 6543, Sources: []Source{fixture}, Sectors: []Sector{}, Corners: []Corner{}},
				},
			},
		},
	}

	summary := BuildCatalogTrackLayoutSummary(catalog)
	if summary.CatalogVersion != CatalogVersionV1 {
		t.Fatalf("expected catalog version %q, got %q", CatalogVersionV1, summary.CatalogVersion)
	}
	if len(summary.Tracks) != 1 {
		t.Fatalf("expected only production-backed track, got %+v", summary.Tracks)
	}
	if summary.Tracks[0].ID != "track-b" {
		t.Fatalf("expected track-b, got %+v", summary.Tracks[0])
	}
	if len(summary.Tracks[0].Sources) != 1 || summary.Tracks[0].Sources[0].SourceType != SourceTypeOfficialGranTurismoTracklistAsset {
		t.Fatalf("expected filtered production sources, got %+v", summary.Tracks[0].Sources)
	}
	if len(summary.Tracks[0].Layouts) != 1 || summary.Tracks[0].Layouts[0].ID != "layout-b1" {
		t.Fatalf("expected only production-backed layout, got %+v", summary.Tracks[0].Layouts)
	}
	if len(summary.Tracks[0].Layouts[0].Sources) != 1 || summary.Tracks[0].Layouts[0].Sources[0].SourceType != SourceTypeGT7InfoCourseCSV {
		t.Fatalf("expected layout sources to be filtered to runtime-safe provenance, got %+v", summary.Tracks[0].Layouts[0].Sources)
	}
}

func TestFindSelectableTrackLayoutRejectsNonSelectableLayouts(t *testing.T) {
	catalog := OfficialGT7SeedCatalog()
	track, layout, ok := FindSelectableTrackLayout(catalog, "gt7_watkins_glen_international", "gt7_layout_1240")
	if !ok {
		t.Fatal("expected official seed layout to be selectable")
	}
	if track.ID != "gt7_watkins_glen_international" || layout.ID != "gt7_layout_1240" {
		t.Fatalf("unexpected track/layout match: %+v %+v", track, layout)
	}

	fixture := Source{URL: "https://example.com/fixture.csv", SourceType: SourceTypeGT7TracksFixtureRawDump, RetrievedAt: "2026-07-17"}
	mutated := catalog
	mutated.Tracks[0].Sources = []Source{fixture}
	if _, _, ok := FindSelectableTrackLayout(mutated, mutated.Tracks[0].ID, mutated.Tracks[0].Layouts[0].ID); ok {
		t.Fatal("expected fixture-only track/layout to be rejected")
	}
}
