package tracks

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
)

const (
	CapabilityStateAvailable   CapabilityState = "available"
	CapabilityStatePartial     CapabilityState = "partial"
	CapabilityStateUnavailable CapabilityState = "unavailable"

	CapabilityReasonLayoutNotFound             = "layout_not_found"
	CapabilityReasonApprovedGeometryMissing    = "approved_geometry_missing"
	CapabilityReasonApprovedGeometryInvalid    = "approved_geometry_invalid"
	CapabilityReasonApprovedGeometryLength     = "approved_geometry_length_mismatch"
	CapabilityReasonApprovedGeometryClosure    = "approved_geometry_not_closed"
	CapabilityReasonApprovedGeometryDensity    = "approved_geometry_insufficient_density"
	CapabilityReasonNoSpatialCornerDefinitions = "no_spatial_corner_definitions"
	CapabilityReasonEnumeratedCornersOnly      = "enumerated_corners_only"
)

const approvedGeometryValidationToleranceMeters = 1.0

// approvedGeometryFixtureJSON seeds the runtime-approved geometry overlay. The
// default bundle is intentionally empty until a curated layout is promoted.
//
//go:embed approved_geometry.json
var approvedGeometryFixtureJSON []byte

type CapabilityState string

type Capability struct {
	State  CapabilityState `json:"state"`
	Reason string          `json:"reason,omitempty"`
}

type LayoutCapabilities struct {
	Detection           Capability `json:"detection"`
	DistanceDelta       Capability `json:"distanceDelta"`
	MicroSectors        Capability `json:"microSectors"`
	CornerAnalysis      Capability `json:"cornerAnalysis"`
	OfficialCornerNames Capability `json:"officialCornerNames"`
}

type ApprovedGeometryManifest struct {
	Layouts []ApprovedGeometryLayout `json:"layouts,omitempty"`
}

type ApprovedGeometryLayout struct {
	TrackID      string  `json:"trackId"`
	LayoutID     string  `json:"layoutId"`
	LengthMeters float64 `json:"lengthMeters"`
	CenterLine   []Point `json:"centerLine"`
}

func defaultApprovedGeometryManifest() ApprovedGeometryManifest {
	manifest, err := loadApprovedGeometryManifest()
	if err != nil {
		panic(err)
	}

	return manifest
}

func loadApprovedGeometryManifest() (ApprovedGeometryManifest, error) {
	if len(approvedGeometryFixtureJSON) == 0 {
		return ApprovedGeometryManifest{}, nil
	}

	var manifest ApprovedGeometryManifest
	if err := json.Unmarshal(approvedGeometryFixtureJSON, &manifest); err != nil {
		return ApprovedGeometryManifest{}, fmt.Errorf("decode approved geometry manifest: %w", err)
	}

	return manifest, nil
}

func (m ApprovedGeometryManifest) Validate() error {
	ids := make(map[string]string)
	for i, layout := range m.Layouts {
		if err := validateApprovedGeometryLayout(fmt.Sprintf("layouts[%d]", i), layout, ids); err != nil {
			return err
		}
	}

	return nil
}

func (m ApprovedGeometryManifest) sanitized(skipInvalid bool) (ApprovedGeometryManifest, error) {
	if len(m.Layouts) == 0 {
		return m, nil
	}

	ids := make(map[string]string)
	out := ApprovedGeometryManifest{Layouts: make([]ApprovedGeometryLayout, 0, len(m.Layouts))}
	for i, layout := range m.Layouts {
		path := fmt.Sprintf("layouts[%d]", i)
		if err := validateApprovedGeometryLayout(path, layout, ids); err != nil {
			if skipInvalid {
				continue
			}
			return ApprovedGeometryManifest{}, err
		}
		out.Layouts = append(out.Layouts, layout)
	}

	return out, nil
}

func validateApprovedGeometryLayout(path string, layout ApprovedGeometryLayout, ids map[string]string) error {
	if layout.TrackID == "" {
		return fmt.Errorf("%s.trackId: %w", path, ErrMissingTrackID)
	}
	if layout.LayoutID == "" {
		return fmt.Errorf("%s.layoutId: %w", path, ErrMissingLayoutID)
	}
	if layout.LengthMeters <= 0 {
		return fmt.Errorf("%s.lengthMeters: %w", path, ErrInvalidLength)
	}
	if err := rememberID(ids, approvedGeometryKey(layout.TrackID, layout.LayoutID), path); err != nil {
		return err
	}
	if len(layout.CenterLine) < 2 {
		return fmt.Errorf("%s.centerLine: %w", path, ErrInvalidCenterLine)
	}

	first := layout.CenterLine[0]
	if !isFinite(first.AccumulatedMeters) || first.AccumulatedMeters != 0 {
		return fmt.Errorf("%s.centerLine[0].accumulatedMeters: %w", path, ErrInvalidCenterLine)
	}
	if !pointFinite(first) {
		return fmt.Errorf("%s.centerLine[0]: %w", path, ErrInvalidCenterLine)
	}

	previous := first
	previousAccumulated := first.AccumulatedMeters
	for i := 1; i < len(layout.CenterLine); i++ {
		pointPath := fmt.Sprintf("%s.centerLine[%d]", path, i)
		point := layout.CenterLine[i]
		if !pointFinite(point) || !isFinite(point.AccumulatedMeters) {
			return fmt.Errorf("%s: %w", pointPath, ErrInvalidCenterLine)
		}
		if point.AccumulatedMeters <= previousAccumulated {
			return fmt.Errorf("%s.accumulatedMeters: %w", pointPath, ErrInvalidCenterLine)
		}
		delta := point.AccumulatedMeters - previousAccumulated
		segmentLength := pointDistance(previous, point)
		if segmentLength <= 0 || math.Abs(segmentLength-delta) > geometryTolerance(delta, segmentLength) {
			return fmt.Errorf("%s: %w", pointPath, ErrInvalidCenterLine)
		}
		previous = point
		previousAccumulated = point.AccumulatedMeters
	}

	if math.Abs(previousAccumulated-layout.LengthMeters) > geometryTolerance(layout.LengthMeters, previousAccumulated) {
		return fmt.Errorf("%s.centerLine[%d].accumulatedMeters: %w", path, len(layout.CenterLine)-1, ErrInvalidCenterLine)
	}
	if pointDistance(first, previous) > geometryTolerance(layout.LengthMeters, pointDistance(first, previous)) {
		return fmt.Errorf("%s.centerLine: %w", path, ErrInvalidCenterLine)
	}

	return nil
}

func geometryTolerance(a, b float64) float64 {
	max := math.Max(math.Abs(a), math.Abs(b))
	if max <= approvedGeometryValidationToleranceMeters {
		return approvedGeometryValidationToleranceMeters
	}

	return max * 0.01
}

func pointFinite(p Point) bool {
	return isFinite(p.X) && isFinite(p.Y) && isFinite(p.Z)
}

func approvedGeometryKey(trackID, layoutID string) string {
	return trackID + "::" + layoutID
}

func availableCapability() Capability {
	return Capability{State: CapabilityStateAvailable}
}

func partialCapability(reason string) Capability {
	return Capability{State: CapabilityStatePartial, Reason: reason}
}

func unavailableCapability(reason string) Capability {
	return Capability{State: CapabilityStateUnavailable, Reason: reason}
}

func (c Catalog) LayoutCapabilities(trackID, layoutID string) LayoutCapabilities {
	track, layout, ok := c.layoutByID(trackID, layoutID)
	if !ok {
		return LayoutCapabilities{
			Detection:           unavailableCapability(CapabilityReasonLayoutNotFound),
			DistanceDelta:       unavailableCapability(CapabilityReasonLayoutNotFound),
			MicroSectors:        unavailableCapability(CapabilityReasonLayoutNotFound),
			CornerAnalysis:      unavailableCapability(CapabilityReasonLayoutNotFound),
			OfficialCornerNames: unavailableCapability(CapabilityReasonLayoutNotFound),
		}
	}

	caps := LayoutCapabilities{Detection: availableCapability()}
	approved, approvedState := c.approvedGeometryFor(track.ID, layout.ID)
	switch approvedState {
	case approvedGeometryStateMissing:
		caps.DistanceDelta = unavailableCapability(CapabilityReasonApprovedGeometryMissing)
		caps.MicroSectors = unavailableCapability(CapabilityReasonApprovedGeometryMissing)
		caps.CornerAnalysis = unavailableCapability(CapabilityReasonApprovedGeometryMissing)
		caps.OfficialCornerNames = unavailableCapability(CapabilityReasonApprovedGeometryMissing)
		return caps
	case approvedGeometryStateInvalid:
		caps.DistanceDelta = unavailableCapability(CapabilityReasonApprovedGeometryInvalid)
		caps.MicroSectors = unavailableCapability(CapabilityReasonApprovedGeometryInvalid)
		caps.CornerAnalysis = unavailableCapability(CapabilityReasonApprovedGeometryInvalid)
		caps.OfficialCornerNames = unavailableCapability(CapabilityReasonApprovedGeometryInvalid)
		return caps
	}

	if math.Abs(approved.LengthMeters-layout.LengthMeters) > geometryTolerance(layout.LengthMeters, approved.LengthMeters) {
		caps.DistanceDelta = unavailableCapability(CapabilityReasonApprovedGeometryLength)
	} else {
		caps.DistanceDelta = availableCapability()
	}

	if len(approved.CenterLine) >= 8 {
		caps.MicroSectors = availableCapability()
	} else {
		caps.MicroSectors = partialCapability(CapabilityReasonApprovedGeometryDensity)
	}

	if hasSpatialCornerDefinitions(layout) {
		caps.CornerAnalysis = availableCapability()
	} else {
		caps.CornerAnalysis = partialCapability(CapabilityReasonNoSpatialCornerDefinitions)
	}

	if hasOfficialCornerNames(layout) {
		caps.OfficialCornerNames = availableCapability()
	} else {
		caps.OfficialCornerNames = partialCapability(CapabilityReasonEnumeratedCornersOnly)
	}

	return caps
}

func (c Catalog) approvedGeometryFor(trackID, layoutID string) (ApprovedGeometryLayout, approvedGeometryState) {
	for _, approved := range c.ApprovedGeometry.Layouts {
		if approved.TrackID == trackID && approved.LayoutID == layoutID {
			if err := validateApprovedGeometryLayout("approvedGeometry", approved, map[string]string{}); err != nil {
				return ApprovedGeometryLayout{}, approvedGeometryStateInvalid
			}

			return approved, approvedGeometryStateValid
		}
	}

	return ApprovedGeometryLayout{}, approvedGeometryStateMissing
}

func (c Catalog) layoutByID(trackID, layoutID string) (CatalogTrack, CatalogLayout, bool) {
	for _, track := range c.Tracks {
		if track.ID != trackID {
			continue
		}
		for _, layout := range track.Layouts {
			if layout.ID == layoutID {
				return track, layout, true
			}
		}
	}

	return CatalogTrack{}, CatalogLayout{}, false
}

func hasSpatialCornerDefinitions(layout CatalogLayout) bool {
	for _, corner := range layout.Corners {
		if corner.DefinitionMode == CornerDefinitionCatalogManual {
			return true
		}
	}

	return false
}

func hasOfficialCornerNames(layout CatalogLayout) bool {
	for _, corner := range layout.Corners {
		if corner.DefinitionMode == CornerDefinitionCatalogManual && corner.Name != "" {
			return true
		}
	}

	return false
}

type approvedGeometryState string

const (
	approvedGeometryStateMissing approvedGeometryState = "missing"
	approvedGeometryStateValid   approvedGeometryState = "valid"
	approvedGeometryStateInvalid approvedGeometryState = "invalid"
)

func (c Catalog) PrepareApprovedGeometry(skipInvalid bool) (Catalog, error) {
	clone := c
	manifest, err := clone.ApprovedGeometry.sanitized(skipInvalid)
	if err != nil {
		return Catalog{}, err
	}
	clone.ApprovedGeometry = manifest

	return clone, nil
}
