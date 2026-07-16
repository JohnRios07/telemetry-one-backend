package corners

import (
	"errors"
	"math"

	"telemetry-one-backend/internal/tracks"
)

const (
	StatusInCorner = "in_corner"
	StatusNoCorner = "no_corner"

	ReasonCatalogHasNoCorners         = "catalog_has_no_corners"
	ReasonCatalogHasEnumeratedCorners = "catalog_has_enumerated_corners"
	ReasonOutsideCornerRange          = "outside_corner_range"
	ReasonUnsupportedCornerDefinition = "unsupported_corner_definition"
)

var (
	ErrInvalidDistance = errors.New("distanceFromStart must be finite")
	ErrInvalidLength   = errors.New("layout lengthMeters must be greater than zero")
)

type Resolution struct {
	Status      string  `json:"status"`
	Reason      string  `json:"reason,omitempty"`
	CornerID    string  `json:"cornerId,omitempty"`
	CornerName  string  `json:"cornerName,omitempty"`
	Number      int     `json:"number,omitempty"`
	StartMeters float64 `json:"startMeters,omitempty"`
	ApexMeters  float64 `json:"apexMeters,omitempty"`
	EndMeters   float64 `json:"endMeters,omitempty"`
}

func ResolveCurrent(layout tracks.CatalogLayout, distanceFromStart float64, closed bool) (Resolution, error) {
	if layout.LengthMeters <= 0 {
		return Resolution{}, ErrInvalidLength
	}
	if !isFinite(distanceFromStart) {
		return Resolution{}, ErrInvalidDistance
	}
	if len(layout.Corners) == 0 {
		return noCorner(ReasonCatalogHasNoCorners), nil
	}

	distance := distanceFromStart
	if closed {
		distance = normalizeDistance(distanceFromStart, layout.LengthMeters)
	} else if distance < 0 || distance >= layout.LengthMeters {
		return noCorner(ReasonOutsideCornerRange), nil
	}

	for _, corner := range layout.Corners {
		if corner.DefinitionMode != tracks.CornerDefinitionCatalogManual {
			continue
		}
		if containsDistance(corner, distance, closed) {
			return Resolution{
				Status:      StatusInCorner,
				CornerID:    corner.ID,
				CornerName:  corner.Name,
				Number:      corner.Number,
				StartMeters: corner.StartMeters,
				ApexMeters:  corner.ApexMeters,
				EndMeters:   corner.EndMeters,
			}, nil
		}
	}

	for _, corner := range layout.Corners {
		if corner.DefinitionMode == tracks.CornerDefinitionCatalogEnumerated {
			return noCorner(ReasonCatalogHasEnumeratedCorners), nil
		}
		if corner.DefinitionMode != tracks.CornerDefinitionCatalogManual {
			return noCorner(ReasonUnsupportedCornerDefinition), nil
		}
	}

	return noCorner(ReasonOutsideCornerRange), nil
}

func noCorner(reason string) Resolution {
	return Resolution{Status: StatusNoCorner, Reason: reason}
}

func containsDistance(corner tracks.Corner, distance float64, closed bool) bool {
	if corner.StartMeters < corner.EndMeters {
		return distance >= corner.StartMeters && distance < corner.EndMeters
	}
	if !closed || corner.StartMeters == corner.EndMeters {
		return false
	}

	return distance >= corner.StartMeters || distance < corner.EndMeters
}

func normalizeDistance(distance float64, totalMeters float64) float64 {
	normalized := math.Mod(distance, totalMeters)
	if normalized < 0 {
		normalized += totalMeters
	}

	return normalized
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
