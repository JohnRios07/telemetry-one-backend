package trackbuilder

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"telemetry-one-backend/internal/geometry"
	"telemetry-one-backend/internal/telemetry"
	"telemetry-one-backend/internal/tracks"
)

const curatedGeometrySourceType = "telemetry_one_curated_geometry"

var errLayoutNotFound = errors.New("requested layout was not found in seed catalog")

func Build(request telemetry.IngestBatchRequest, opts Options, seed tracks.Catalog) (tracks.Catalog, Report, error) {
	opts = opts.Normalize()
	if err := opts.Validate(); err != nil {
		return tracks.Catalog{}, Report{}, err
	}
	if err := validateBuildRequest(request); err != nil {
		return tracks.Catalog{}, Report{}, fmt.Errorf("validate ingest batch: %w", err)
	}

	track, layout, ok := findSeedLayout(seed, opts.LayoutID)
	if !ok {
		return tracks.Catalog{}, Report{}, fmt.Errorf("%w: %s", errLayoutNotFound, opts.LayoutID)
	}

	cleaned := cleanupTelemetryPoints(request.Frames)
	if len(cleaned) < 2 {
		return tracks.Catalog{}, Report{}, ErrInsufficientUsablePoint
	}
	sourcePathLengthMeters := polylineLength(cleaned, false)

	closed := isClosedLoop(cleaned)
	smoothed, err := smoothPoints(cleaned, opts.SmoothingWindow, closed)
	if err != nil {
		return tracks.Catalog{}, Report{}, err
	}
	simplified := rdpSimplify(smoothed, opts.EpsilonMeters)
	if len(simplified) < 2 {
		return tracks.Catalog{}, Report{}, ErrInsufficientUsablePoint
	}

	resampled, totalMeters, err := resamplePolyline(simplified, opts.ResampleStepMeters, closed)
	if err != nil {
		return tracks.Catalog{}, Report{}, err
	}

	centerlinePoints := make([]tracks.Point, 0, len(resampled))
	for index, sample := range resampled {
		centerlinePoints = append(centerlinePoints, tracks.Point{
			Index:             index,
			X:                 sample.point.X,
			Y:                 sample.point.Y,
			Z:                 sample.point.Z,
			AccumulatedMeters: sample.accumulated,
		})
	}

	if len(centerlinePoints) < 2 {
		return tracks.Catalog{}, Report{}, ErrInsufficientUsablePoint
	}
	retrievedAt := curatedSourceRetrievedAt(request.Frames)

	generatedCatalog := tracks.Catalog{
		CatalogVersion: tracks.CatalogVersionV1,
		GeneratedBy:    "trackbuilder curated geometry review tool",
		Notes:          "Non-official curated geometry generated from telemetry ingest. Human review required before runtime use.",
		Tracks: []tracks.CatalogTrack{{
			ID:      track.ID,
			Name:    track.Name,
			Country: track.Country,
			Sources: appendCuratedSource(track.Sources, retrievedAt),
			Layouts: []tracks.CatalogLayout{{
				ID:           layout.ID,
				Name:         layout.Name,
				LengthMeters: totalMeters,
				Sources:      appendCuratedSource(layout.Sources, retrievedAt),
				Sectors:      []tracks.Sector{},
				Corners:      []tracks.Corner{},
				CenterLine:   centerlinePoints,
			}},
		}},
	}

	if err := generatedCatalog.Validate(); err != nil {
		return tracks.Catalog{}, Report{}, fmt.Errorf("validate generated catalog: %w", err)
	}

	report := Report{
		LayoutID:                    layout.ID,
		LayoutName:                  layout.Name,
		SourcePointCount:            len(cleaned),
		SourcePathLengthMeters:      sourcePathLengthMeters,
		SimplifiedPointCount:        len(simplified),
		SimplificationDroppedPoints: len(cleaned) - len(simplified),
		GeneratedPointCount:         len(centerlinePoints),
		GeneratedPathLengthMeters:   totalMeters,
		CatalogLengthMeters:         layout.LengthMeters,
		DeltaMeters:                 totalMeters - layout.LengthMeters,
		DeltaPct:                    deltaPct(totalMeters, layout.LengthMeters),
		StartEndGapMeters:           geometry.Distance(toGeometryPoint(centerlinePoints[0]), toGeometryPoint(centerlinePoints[len(centerlinePoints)-1])),
		MeanDeviationMeters:         math.NaN(),
		MaxDeviationMeters:          math.NaN(),
	}
	if mean, max, ok := deviationAgainstBaseline(layout.CenterLine, centerlinePoints); ok {
		report.MeanDeviationMeters = mean
		report.MaxDeviationMeters = max
	}

	return generatedCatalog, report, nil
}

func validateBuildRequest(request telemetry.IngestBatchRequest) error {
	if strings.TrimSpace(request.SessionID) == "" {
		return telemetry.ErrMissingSessionID
	}
	if len(request.Frames) == 0 {
		return telemetry.ErrEmptyFrames
	}

	var previousTimestamp int64
	for index, frame := range request.Frames {
		if frame.TimestampUnixMs <= 0 {
			return fmt.Errorf("frames[%d]: %w", index, telemetry.ErrInvalidTimestamp)
		}
		if index > 0 && frame.TimestampUnixMs <= previousTimestamp {
			return fmt.Errorf("frames[%d]: %w", index, telemetry.ErrNonMonotonicTimestamp)
		}
		previousTimestamp = frame.TimestampUnixMs
	}

	return nil
}

func findSeedLayout(catalog tracks.Catalog, layoutID string) (tracks.CatalogTrack, tracks.CatalogLayout, bool) {
	for _, track := range catalog.Tracks {
		for _, layout := range track.Layouts {
			if layout.ID == layoutID {
				return track, layout, true
			}
		}
	}

	return tracks.CatalogTrack{}, tracks.CatalogLayout{}, false
}

func curatedSourceRetrievedAt(frames []telemetry.Frame) string {
	return time.UnixMilli(frames[0].TimestampUnixMs).UTC().Format("2006-01-02")
}

func appendCuratedSource(sources []tracks.Source, retrievedAt string) []tracks.Source {
	cloned := append([]tracks.Source(nil), sources...)
	cloned = append(cloned, tracks.Source{
		URL:         "urn:telemetry-one:trackbuilder:curated-geometry",
		SourceType:  curatedGeometrySourceType,
		RetrievedAt: retrievedAt,
		Note:        "Curated geometry generated from telemetry ingest; review-only and not runtime approved.",
	})

	return cloned
}

func toGeometryPoint(point tracks.Point) geometry.Point {
	return geometry.Point{X: point.X, Y: point.Y, Z: point.Z}
}

func deviationAgainstBaseline(baseline []tracks.Point, generated []tracks.Point) (float64, float64, bool) {
	if len(baseline) < 2 || len(generated) < 2 {
		return math.NaN(), math.NaN(), false
	}
	baselinePoints := trackPointsToGeometry(baseline)
	baselineClosed := isClosedLoop(baselinePoints)
	if baselineClosed && len(baselinePoints) > 2 && samePoint(baselinePoints[0], baselinePoints[len(baselinePoints)-1]) {
		baselinePoints = append([]geometry.Point(nil), baselinePoints[:len(baselinePoints)-1]...)
	}
	centerline, err := geometry.NewCenterline(baselinePoints, baselineClosed)
	if err != nil {
		return math.NaN(), math.NaN(), false
	}

	var sum float64
	var count int
	max := 0.0
	for _, point := range generated {
		projection := centerline.Project(toGeometryPoint(point))
		if !isFinite(projection.OffCenterlineMeters) {
			continue
		}
		sum += projection.OffCenterlineMeters
		count++
		if projection.OffCenterlineMeters > max {
			max = projection.OffCenterlineMeters
		}
	}
	if count == 0 {
		return math.NaN(), math.NaN(), false
	}

	return sum / float64(count), max, true
}

func trackPointsToGeometry(points []tracks.Point) []geometry.Point {
	converted := make([]geometry.Point, 0, len(points))
	for _, point := range points {
		converted = append(converted, toGeometryPoint(point))
	}

	return converted
}

func deltaPct(telemetryPathMeters, catalogLengthMeters float64) float64 {
	if catalogLengthMeters <= 0 || !isFinite(catalogLengthMeters) {
		return math.NaN()
	}
	return (telemetryPathMeters - catalogLengthMeters) / catalogLengthMeters * 100
}

func EncodeCatalog(catalog tracks.Catalog) ([]byte, error) {
	encoded, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return nil, err
	}

	return append(encoded, '\n'), nil
}
