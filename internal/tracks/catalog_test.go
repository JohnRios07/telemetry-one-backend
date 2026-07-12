package tracks

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"testing"
)

func TestCatalogValidateAcceptsValidCatalog(t *testing.T) {
	catalog := validCatalog()

	if err := catalog.Validate(); err != nil {
		t.Fatalf("expected valid catalog, got %v", err)
	}
}

func TestCatalogValidateVersionHandling(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr error
	}{
		{name: "missing", wantErr: ErrMissingCatalogVersion},
		{name: "unsupported", version: "telemetry-one.track-catalog.v2", wantErr: ErrUnsupportedCatalogVersion},
		{name: "supported", version: CatalogVersionV1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := validCatalog()
			catalog.CatalogVersion = tt.version

			err := catalog.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestCatalogValidateRejectsDuplicateIDs(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Catalog)
	}{
		{name: "track id", change: func(c *Catalog) {
			c.Tracks = append(c.Tracks, CatalogTrack{ID: "synthetic_dev_track", Name: "Other", Layouts: []CatalogLayout{validLayout("other_layout")}})
		}},
		{name: "layout id", change: func(c *Catalog) { c.Tracks[0].Layouts = append(c.Tracks[0].Layouts, validLayout("synthetic_dev_loop")) }},
		{name: "corner id", change: func(c *Catalog) { c.Tracks[0].Layouts[0].Corners[1].ID = "synthetic_dev_loop_t1" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := validCatalog()
			tt.change(&catalog)

			if err := catalog.Validate(); !errors.Is(err, ErrDuplicateID) {
				t.Fatalf("expected duplicate id error, got %v", err)
			}
		})
	}
}

func TestCatalogValidateRejectsInvalidDistanceRanges(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Catalog)
	}{
		{name: "sector inverted", change: func(c *Catalog) { c.Tracks[0].Layouts[0].Sectors[0].EndMeters = 0 }},
		{name: "sector past layout length", change: func(c *Catalog) { c.Tracks[0].Layouts[0].Sectors[2].EndMeters = 1301 }},
		{name: "corner apex before start", change: func(c *Catalog) { c.Tracks[0].Layouts[0].Corners[0].ApexMeters = 50 }},
		{name: "corner past layout length", change: func(c *Catalog) { c.Tracks[0].Layouts[0].Corners[1].EndMeters = 1301 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := validCatalog()
			tt.change(&catalog)

			if err := catalog.Validate(); !errors.Is(err, ErrInvalidDistanceRange) {
				t.Fatalf("expected invalid range error, got %v", err)
			}
		})
	}
}

func TestCatalogValidateAcceptsManualCatalogCornerDefinitions(t *testing.T) {
	catalog := validCatalog()
	catalog.Tracks[0].Layouts[0].Corners[0].DefinitionMode = CornerDefinitionCatalogManual

	if err := catalog.Validate(); err != nil {
		t.Fatalf("expected manual catalog corner definition to validate, got %v", err)
	}
}

func TestCatalogValidateRejectsFutureAutomaticCornerDefinitions(t *testing.T) {
	tests := []struct {
		name string
		mode CornerDefinitionMode
	}{
		{name: "missing"},
		{name: "future automatic", mode: CornerDefinitionAutoDetectedFuture},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := validCatalog()
			catalog.Tracks[0].Layouts[0].Corners[0].DefinitionMode = tt.mode

			if err := catalog.Validate(); !errors.Is(err, ErrUnsupportedCornerDefinitionMode) {
				t.Fatalf("expected unsupported corner definition mode error, got %v", err)
			}
		})
	}
}

func TestCatalogValidateRejectsOverlappingRanges(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Catalog)
	}{
		{name: "sectors overlap", change: func(c *Catalog) { c.Tracks[0].Layouts[0].Sectors[1].StartMeters = 300 }},
		{name: "corners overlap", change: func(c *Catalog) { c.Tracks[0].Layouts[0].Corners[1].StartMeters = 150 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := validCatalog()
			tt.change(&catalog)

			if err := catalog.Validate(); !errors.Is(err, ErrOverlappingDistanceRange) {
				t.Fatalf("expected overlapping range error, got %v", err)
			}
		})
	}
}

func TestCatalogValidateAcceptsFinalWrapAroundCorner(t *testing.T) {
	catalog := validCatalog()
	catalog.Tracks[0].Layouts[0].Corners = append(catalog.Tracks[0].Layouts[0].Corners, Corner{
		ID:             "synthetic_dev_loop_final",
		Number:         99,
		Name:           "Synthetic Final Wrap",
		DefinitionMode: CornerDefinitionCatalogManual,
		StartMeters:    1250,
		ApexMeters:     20,
		EndMeters:      80,
	})

	if err := catalog.Validate(); err != nil {
		t.Fatalf("expected final wrap-around corner to validate, got %v", err)
	}
}

func TestCatalogValidateRejectsInvalidWrapAroundCorners(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Catalog)
	}{
		{name: "wrap not last", change: func(c *Catalog) {
			c.Tracks[0].Layouts[0].Corners = append([]Corner{{ID: "synthetic_dev_loop_wrap", Number: 0, Name: "Synthetic Wrap", DefinitionMode: CornerDefinitionCatalogManual, StartMeters: 1250, ApexMeters: 20, EndMeters: 80}}, c.Tracks[0].Layouts[0].Corners...)
		}},
		{name: "wrap overlaps first corner", change: func(c *Catalog) {
			c.Tracks[0].Layouts[0].Corners = append(c.Tracks[0].Layouts[0].Corners, Corner{ID: "synthetic_dev_loop_wrap", Number: 99, Name: "Synthetic Wrap", DefinitionMode: CornerDefinitionCatalogManual, StartMeters: 1250, ApexMeters: 20, EndMeters: 120})
		}},
		{name: "wrap apex outside wrapped interval", change: func(c *Catalog) {
			c.Tracks[0].Layouts[0].Corners = append(c.Tracks[0].Layouts[0].Corners, Corner{ID: "synthetic_dev_loop_wrap", Number: 99, Name: "Synthetic Wrap", DefinitionMode: CornerDefinitionCatalogManual, StartMeters: 1250, ApexMeters: 500, EndMeters: 80})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := validCatalog()
			tt.change(&catalog)

			if err := catalog.Validate(); !errors.Is(err, ErrOverlappingDistanceRange) && !errors.Is(err, ErrInvalidDistanceRange) {
				t.Fatalf("expected wrap-around validation error, got %v", err)
			}
		})
	}
}

func TestCatalogValidateRejectsInvalidCenterLine(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Catalog)
	}{
		{name: "single point", change: func(c *Catalog) { c.Tracks[0].Layouts[0].CenterLine = []Point{{Index: 0}} }},
		{name: "non finite coordinate", change: func(c *Catalog) {
			c.Tracks[0].Layouts[0].CenterLine = []Point{{Index: 0, AccumulatedMeters: 0}, {Index: 1, X: math.Inf(1), AccumulatedMeters: 1}}
		}},
		{name: "non increasing distance", change: func(c *Catalog) {
			c.Tracks[0].Layouts[0].CenterLine = []Point{{Index: 0, AccumulatedMeters: 0}, {Index: 1, X: 1, AccumulatedMeters: 0}}
		}},
		{name: "distance past layout length", change: func(c *Catalog) {
			c.Tracks[0].Layouts[0].CenterLine = []Point{{Index: 0, AccumulatedMeters: 0}, {Index: 1, X: 1, AccumulatedMeters: 1301}}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := validCatalog()
			tt.change(&catalog)

			if err := catalog.Validate(); !errors.Is(err, ErrInvalidCenterLine) {
				t.Fatalf("expected invalid centerLine error, got %v", err)
			}
		})
	}
}

func TestCatalogValidateRequiresCatalogNames(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*Catalog)
		wantErr error
	}{
		{name: "track name", change: func(c *Catalog) { c.Tracks[0].Name = "" }, wantErr: ErrMissingTrackName},
		{name: "layout name", change: func(c *Catalog) { c.Tracks[0].Layouts[0].Name = "" }, wantErr: ErrMissingLayoutName},
		{name: "sector name", change: func(c *Catalog) { c.Tracks[0].Layouts[0].Sectors[0].Name = "" }, wantErr: ErrMissingSectorName},
		{name: "corner name", change: func(c *Catalog) { c.Tracks[0].Layouts[0].Corners[0].Name = "" }, wantErr: ErrMissingCornerName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := validCatalog()
			tt.change(&catalog)

			if err := catalog.Validate(); !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestCatalogValidateRequiresCompleteSourcesWhenPresent(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*Catalog)
		wantErr error
	}{
		{name: "url", change: func(c *Catalog) { c.Tracks[0].Sources = []Source{{SourceType: "official", RetrievedAt: "2026-07-12"}} }, wantErr: ErrMissingSourceURL},
		{name: "source type", change: func(c *Catalog) {
			c.Tracks[0].Sources = []Source{{URL: "https://www.gran-turismo.com/us/gt7/tracklist/", RetrievedAt: "2026-07-12"}}
		}, wantErr: ErrMissingSourceType},
		{name: "retrieved at", change: func(c *Catalog) {
			c.Tracks[0].Sources = []Source{{URL: "https://www.gran-turismo.com/us/gt7/tracklist/", SourceType: "official"}}
		}, wantErr: ErrMissingSourceRetrievedAt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := validCatalog()
			tt.change(&catalog)

			if err := catalog.Validate(); !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestCatalogValidateSourceOfTruthAcceptsProvenanceBackedCatalog(t *testing.T) {
	catalog := sourcedCatalog()

	if err := catalog.ValidateSourceOfTruth(); err != nil {
		t.Fatalf("expected source-of-truth catalog to validate, got %v", err)
	}
}

func TestCatalogValidateSourceOfTruthRequiresProvenanceForNamedMetadata(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Catalog)
	}{
		{name: "track source", change: func(c *Catalog) { c.Tracks[0].Sources = nil }},
		{name: "layout source", change: func(c *Catalog) { c.Tracks[0].Layouts[0].Sources = nil }},
		{name: "sector source", change: func(c *Catalog) { c.Tracks[0].Layouts[0].Sectors[0].Sources = nil }},
		{name: "corner source", change: func(c *Catalog) { c.Tracks[0].Layouts[0].Corners[0].Sources = nil }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := sourcedCatalog()
			tt.change(&catalog)

			if err := catalog.ValidateSourceOfTruth(); !errors.Is(err, ErrMissingMetadataSource) {
				t.Fatalf("expected missing metadata source error, got %v", err)
			}
		})
	}
}

func TestGT7OfficialSeedCatalogSourceOfTruthValidatesWithoutSectorsOrCorners(t *testing.T) {
	catalog := OfficialGT7SeedCatalog()

	if err := catalog.ValidateSourceOfTruth(); err != nil {
		t.Fatalf("expected official seed source-of-truth metadata to validate, got %v", err)
	}
	for _, track := range catalog.Tracks {
		for _, layout := range track.Layouts {
			if len(layout.Sectors) != 0 || len(layout.Corners) != 0 {
				t.Fatalf("expected official seed layout %q to omit unsourced sector/corner names", layout.ID)
			}
		}
	}
}

func TestSyntheticDevCatalogFixtureValidates(t *testing.T) {
	catalog := loadCatalogFixture(t, "../../testdata/catalogs/synthetic_dev_catalog.json")

	if err := catalog.Validate(); err != nil {
		t.Fatalf("expected fixture to validate, got %v", err)
	}
}

func TestGT7OfficialSeedCatalogFixtureValidates(t *testing.T) {
	catalog := loadCatalogFixture(t, "../../testdata/catalogs/gt7_official_seed_catalog.json")

	if err := catalog.Validate(); err != nil {
		t.Fatalf("expected fixture to validate, got %v", err)
	}
	for _, track := range catalog.Tracks {
		if len(track.Sources) == 0 {
			t.Fatalf("expected track %q to include provenance sources", track.ID)
		}
		for _, layout := range track.Layouts {
			if len(layout.Sources) == 0 {
				t.Fatalf("expected layout %q to include provenance sources", layout.ID)
			}
			if len(layout.Sectors) != 0 {
				t.Fatalf("expected official seed layout %q to omit sectors until official boundaries are sourced", layout.ID)
			}
			if len(layout.Corners) != 0 {
				t.Fatalf("expected official seed layout %q to omit corners until official corner metadata is sourced", layout.ID)
			}
		}
	}
}

func loadCatalogFixture(t *testing.T, path string) Catalog {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	var catalog Catalog
	if err := json.Unmarshal(contents, &catalog); err != nil {
		t.Fatalf("failed to decode fixture: %v", err)
	}

	return catalog
}

func validCatalog() Catalog {
	return Catalog{
		CatalogVersion: CatalogVersionV1,
		GeneratedBy:    "test",
		Notes:          "Synthetic catalog for validation tests only.",
		Tracks: []CatalogTrack{
			{
				ID:      "synthetic_dev_track",
				Name:    "Synthetic Dev Track",
				Country: "N/A",
				Layouts: []CatalogLayout{validLayout("synthetic_dev_loop")},
			},
		},
	}
}

func sourcedCatalog() Catalog {
	catalog := validCatalog()
	source := officialNewsSource("https://www.gran-turismo.com/us/news/00_5302315.html")
	catalog.Tracks[0].Sources = []Source{source}
	catalog.Tracks[0].Layouts[0].Sources = []Source{source}
	for i := range catalog.Tracks[0].Layouts[0].Sectors {
		catalog.Tracks[0].Layouts[0].Sectors[i].Sources = []Source{source}
	}
	for i := range catalog.Tracks[0].Layouts[0].Corners {
		catalog.Tracks[0].Layouts[0].Corners[i].Sources = []Source{source}
	}

	return catalog
}

func validLayout(id string) CatalogLayout {
	return CatalogLayout{
		ID:           id,
		Name:         "Synthetic Dev Loop",
		LengthMeters: 1300,
		CenterLine: []Point{
			{Index: 0, X: 0, Y: 0, Z: 0, AccumulatedMeters: 0},
			{Index: 1, X: 400, Y: 0, Z: 0, AccumulatedMeters: 400},
			{Index: 2, X: 400, Y: 250, Z: 0, AccumulatedMeters: 650},
			{Index: 3, X: 0, Y: 250, Z: 0, AccumulatedMeters: 1050},
			{Index: 4, X: 0, Y: 0, Z: 0, AccumulatedMeters: 1300},
		},
		Sectors: []Sector{
			{Number: 1, Name: "Synthetic Sector 1", StartMeters: 0, EndMeters: 430},
			{Number: 2, Name: "Synthetic Sector 2", StartMeters: 430, EndMeters: 860},
			{Number: 3, Name: "Synthetic Sector 3", StartMeters: 860, EndMeters: 1300},
		},
		Corners: []Corner{
			{ID: "synthetic_dev_loop_t1", Number: 1, Name: "Synthetic Turn 1", DefinitionMode: CornerDefinitionCatalogManual, StartMeters: 100, ApexMeters: 140, EndMeters: 190, Direction: "right", Severity: "medium"},
			{ID: "synthetic_dev_loop_t2", Number: 2, Name: "Synthetic Turn 2", DefinitionMode: CornerDefinitionCatalogManual, StartMeters: 520, ApexMeters: 570, EndMeters: 640, Direction: "left", Severity: "fast"},
		},
	}
}
