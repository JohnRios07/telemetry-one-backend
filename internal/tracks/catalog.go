package tracks

import (
	"errors"
	"fmt"
	"math"
)

const CatalogVersionV1 = "telemetry-one.track-catalog.v1"

var (
	ErrUnsupportedCatalogVersion       = errors.New("unsupported track catalog version")
	ErrMissingCatalogVersion           = errors.New("catalogVersion is required")
	ErrMissingTrackID                  = errors.New("track id is required")
	ErrMissingTrackName                = errors.New("track name is required")
	ErrMissingLayoutID                 = errors.New("layout id is required")
	ErrMissingLayoutName               = errors.New("layout name is required")
	ErrMissingSectorName               = errors.New("sector name is required")
	ErrMissingCornerID                 = errors.New("corner id is required")
	ErrMissingCornerName               = errors.New("corner name is required")
	ErrDuplicateID                     = errors.New("catalog id must be unique")
	ErrInvalidLength                   = errors.New("layout lengthMeters must be greater than zero")
	ErrInvalidDistanceRange            = errors.New("distance range must be ordered and within layout length")
	ErrOverlappingDistanceRange        = errors.New("distance ranges must not overlap")
	ErrMissingSourceURL                = errors.New("source url is required")
	ErrMissingSourceType               = errors.New("source type is required")
	ErrMissingSourceRetrievedAt        = errors.New("source retrievedAt is required")
	ErrMissingMetadataSource           = errors.New("metadata source is required")
	ErrInvalidCenterLine               = errors.New("centerLine must contain finite, ordered, non-zero geometry when present")
	ErrUnsupportedCornerDefinitionMode = errors.New("corner definitionMode must be catalog_manual in catalog v1")
)

type Catalog struct {
	CatalogVersion string         `json:"catalogVersion"`
	GeneratedBy    string         `json:"generatedBy,omitempty"`
	Notes          string         `json:"notes,omitempty"`
	Tracks         []CatalogTrack `json:"tracks"`
}

type CatalogTrack struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Country string          `json:"country,omitempty"`
	Sources []Source        `json:"sources,omitempty"`
	Layouts []CatalogLayout `json:"layouts"`
}

type CatalogLayout struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	LengthMeters float64     `json:"lengthMeters"`
	Sources      []Source    `json:"sources,omitempty"`
	Sectors      []Sector    `json:"sectors"`
	Corners      []Corner    `json:"corners"`
	CenterLine   []Point     `json:"centerLine,omitempty"`
	Fingerprint  Fingerprint `json:"fingerprint,omitempty"`
}

type Source struct {
	URL         string `json:"url"`
	SourceType  string `json:"sourceType"`
	RetrievedAt string `json:"retrievedAt"`
	Note        string `json:"note,omitempty"`
}

func (c Catalog) Validate() error {
	if c.CatalogVersion == "" {
		return ErrMissingCatalogVersion
	}
	if c.CatalogVersion != CatalogVersionV1 {
		return fmt.Errorf("%w: %s", ErrUnsupportedCatalogVersion, c.CatalogVersion)
	}

	ids := make(map[string]string)
	for trackIndex, track := range c.Tracks {
		trackPath := fmt.Sprintf("tracks[%d]", trackIndex)
		if track.ID == "" {
			return fmt.Errorf("%s.id: %w", trackPath, ErrMissingTrackID)
		}
		if track.Name == "" {
			return fmt.Errorf("%s.name: %w", trackPath, ErrMissingTrackName)
		}
		if err := rememberID(ids, track.ID, trackPath); err != nil {
			return err
		}
		if err := validateSources(trackPath+".sources", track.Sources); err != nil {
			return err
		}

		for layoutIndex, layout := range track.Layouts {
			layoutPath := fmt.Sprintf("%s.layouts[%d]", trackPath, layoutIndex)
			if err := layout.validate(layoutPath, ids); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c Catalog) ValidateSourceOfTruth() error {
	if err := c.Validate(); err != nil {
		return err
	}

	for trackIndex, track := range c.Tracks {
		trackPath := fmt.Sprintf("tracks[%d]", trackIndex)
		if err := requireSources(trackPath+".sources", track.Sources); err != nil {
			return err
		}

		for layoutIndex, layout := range track.Layouts {
			layoutPath := fmt.Sprintf("%s.layouts[%d]", trackPath, layoutIndex)
			if err := requireSources(layoutPath+".sources", layout.Sources); err != nil {
				return err
			}
			for sectorIndex, sector := range layout.Sectors {
				sectorPath := fmt.Sprintf("%s.sectors[%d]", layoutPath, sectorIndex)
				if err := requireSources(sectorPath+".sources", sector.Sources); err != nil {
					return err
				}
			}
			for cornerIndex, corner := range layout.Corners {
				cornerPath := fmt.Sprintf("%s.corners[%d]", layoutPath, cornerIndex)
				if err := requireSources(cornerPath+".sources", corner.Sources); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (l CatalogLayout) validate(path string, ids map[string]string) error {
	if l.ID == "" {
		return fmt.Errorf("%s.id: %w", path, ErrMissingLayoutID)
	}
	if l.Name == "" {
		return fmt.Errorf("%s.name: %w", path, ErrMissingLayoutName)
	}
	if err := rememberID(ids, l.ID, path); err != nil {
		return err
	}
	if l.LengthMeters <= 0 {
		return fmt.Errorf("%s.lengthMeters: %w", path, ErrInvalidLength)
	}
	if err := validateSources(path+".sources", l.Sources); err != nil {
		return err
	}

	if err := validateSectors(path+".sectors", l.Sectors, l.LengthMeters); err != nil {
		return err
	}
	if err := validateCorners(path+".corners", l.Corners, l.LengthMeters, ids); err != nil {
		return err
	}
	if err := validateCenterLine(path+".centerLine", l.CenterLine, l.LengthMeters); err != nil {
		return err
	}

	return nil
}

func validateSources(path string, sources []Source) error {
	for i, source := range sources {
		sourcePath := fmt.Sprintf("%s[%d]", path, i)
		if source.URL == "" {
			return fmt.Errorf("%s.url: %w", sourcePath, ErrMissingSourceURL)
		}
		if source.SourceType == "" {
			return fmt.Errorf("%s.sourceType: %w", sourcePath, ErrMissingSourceType)
		}
		if source.RetrievedAt == "" {
			return fmt.Errorf("%s.retrievedAt: %w", sourcePath, ErrMissingSourceRetrievedAt)
		}
	}

	return nil
}

func requireSources(path string, sources []Source) error {
	if len(sources) == 0 {
		return fmt.Errorf("%s: %w", path, ErrMissingMetadataSource)
	}

	return validateSources(path, sources)
}

func validateSectors(path string, sectors []Sector, lengthMeters float64) error {
	var previousEnd float64
	for i, sector := range sectors {
		sectorPath := fmt.Sprintf("%s[%d]", path, i)
		if sector.Name == "" {
			return fmt.Errorf("%s.name: %w", sectorPath, ErrMissingSectorName)
		}
		if err := validateSources(sectorPath+".sources", sector.Sources); err != nil {
			return err
		}
		if sector.StartMeters < 0 || sector.StartMeters >= sector.EndMeters || sector.EndMeters > lengthMeters {
			return fmt.Errorf("%s: %w", sectorPath, ErrInvalidDistanceRange)
		}
		if i > 0 && sector.StartMeters < previousEnd {
			return fmt.Errorf("%s: %w", sectorPath, ErrOverlappingDistanceRange)
		}
		previousEnd = sector.EndMeters
	}

	return nil
}

func validateCorners(path string, corners []Corner, lengthMeters float64, ids map[string]string) error {
	var previousEnd float64
	var firstStart float64
	hasWrapAroundCorner := false
	for i, corner := range corners {
		cornerPath := fmt.Sprintf("%s[%d]", path, i)
		if i == 0 {
			firstStart = corner.StartMeters
		}
		if corner.ID == "" {
			return fmt.Errorf("%s.id: %w", cornerPath, ErrMissingCornerID)
		}
		if corner.Name == "" {
			return fmt.Errorf("%s.name: %w", cornerPath, ErrMissingCornerName)
		}
		if corner.DefinitionMode != CornerDefinitionCatalogManual {
			return fmt.Errorf("%s.definitionMode: %w", cornerPath, ErrUnsupportedCornerDefinitionMode)
		}
		if err := rememberID(ids, corner.ID, cornerPath); err != nil {
			return err
		}
		if err := validateSources(cornerPath+".sources", corner.Sources); err != nil {
			return err
		}
		wrapAround, err := validateCornerRange(corner, lengthMeters)
		if err != nil {
			return fmt.Errorf("%s: %w", cornerPath, ErrInvalidDistanceRange)
		}
		if hasWrapAroundCorner || (wrapAround && i != len(corners)-1) {
			return fmt.Errorf("%s: %w", cornerPath, ErrOverlappingDistanceRange)
		}
		if i > 0 && corner.StartMeters < previousEnd {
			return fmt.Errorf("%s: %w", cornerPath, ErrOverlappingDistanceRange)
		}
		if wrapAround && len(corners) > 1 && corner.EndMeters > firstStart {
			return fmt.Errorf("%s: %w", cornerPath, ErrOverlappingDistanceRange)
		}
		if wrapAround {
			hasWrapAroundCorner = true
			previousEnd = lengthMeters
			continue
		}
		previousEnd = corner.EndMeters
	}

	return nil
}

func validateCornerRange(corner Corner, lengthMeters float64) (bool, error) {
	if !isFinite(corner.StartMeters) || !isFinite(corner.ApexMeters) || !isFinite(corner.EndMeters) {
		return false, ErrInvalidDistanceRange
	}
	if corner.StartMeters < 0 || corner.ApexMeters < 0 || corner.EndMeters < 0 || corner.StartMeters > lengthMeters || corner.ApexMeters > lengthMeters || corner.EndMeters > lengthMeters {
		return false, ErrInvalidDistanceRange
	}
	if corner.StartMeters < corner.EndMeters {
		if corner.StartMeters >= corner.ApexMeters || corner.ApexMeters >= corner.EndMeters {
			return false, ErrInvalidDistanceRange
		}

		return false, nil
	}
	if corner.StartMeters == corner.EndMeters {
		return false, ErrInvalidDistanceRange
	}
	if corner.ApexMeters < corner.StartMeters && corner.ApexMeters >= corner.EndMeters {
		return false, ErrInvalidDistanceRange
	}

	return true, nil
}

func validateCenterLine(path string, centerLine []Point, lengthMeters float64) error {
	if len(centerLine) == 0 {
		return nil
	}
	if len(centerLine) < 2 {
		return fmt.Errorf("%s: %w", path, ErrInvalidCenterLine)
	}

	previousAccumulated := -1.0
	for i, point := range centerLine {
		pointPath := fmt.Sprintf("%s[%d]", path, i)
		if !isFinite(point.X) || !isFinite(point.Y) || !isFinite(point.Z) || !isFinite(point.AccumulatedMeters) {
			return fmt.Errorf("%s: %w", pointPath, ErrInvalidCenterLine)
		}
		if point.AccumulatedMeters < 0 || point.AccumulatedMeters > lengthMeters {
			return fmt.Errorf("%s.accumulatedMeters: %w", pointPath, ErrInvalidCenterLine)
		}
		if point.AccumulatedMeters <= previousAccumulated {
			return fmt.Errorf("%s.accumulatedMeters: %w", pointPath, ErrInvalidCenterLine)
		}
		if i > 0 && pointDistance(centerLine[i-1], point) == 0 {
			return fmt.Errorf("%s: %w", pointPath, ErrInvalidCenterLine)
		}
		previousAccumulated = point.AccumulatedMeters
	}
	if centerLine[0].AccumulatedMeters != 0 {
		return fmt.Errorf("%s[0].accumulatedMeters: %w", path, ErrInvalidCenterLine)
	}

	return nil
}

func rememberID(ids map[string]string, id string, path string) error {
	if existingPath, ok := ids[id]; ok {
		return fmt.Errorf("%s duplicates %s: %w", path, existingPath, ErrDuplicateID)
	}
	ids[id] = path

	return nil
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func pointDistance(a Point, b Point) float64 {
	dx := b.X - a.X
	dy := b.Y - a.Y
	dz := b.Z - a.Z

	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}
