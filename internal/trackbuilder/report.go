package trackbuilder

import (
	"fmt"
	"math"
	"strings"
)

type Report struct {
	LayoutID                     string
	LayoutName                   string
	SourcePointCount             int
	SourceSegmentCount           int
	SourceSegmentLengthMinMeters float64
	SourceSegmentLengthP50Meters float64
	SourceSegmentLengthP95Meters float64
	SourceSegmentLengthMaxMeters float64
	SourceHeadingChangeDegrees   float64
	SourceHeadingChangePerMeter  float64
	SourceLocalChordWindowPoints int
	SourceLocalChordWindowCount  int
	SourceLocalChordRatioAvg     float64
	SourceLocalChordRatioP50     float64
	SourceLocalChordRatioP95     float64
	SourceLocalChordRatioMax     float64
	SourceLocalChordExcessAvg    float64
	SourceLocalChordExcessP50    float64
	SourceLocalChordExcessP95    float64
	SourceLocalChordExcessMax    float64
	SourcePathLengthMeters       float64
	SimplifiedPointCount         int
	SimplificationDroppedPoints  int
	GeneratedPointCount          int
	GeneratedPathLengthMeters    float64
	CatalogLengthMeters          float64
	DeltaMeters                  float64
	DeltaPct                     float64
	StartEndGapMeters            float64
	MeanDeviationMeters          float64
	MaxDeviationMeters           float64
}

func (r Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "trackbuilder closing report\n")
	fmt.Fprintf(&b, "layoutId: %s\n", r.LayoutID)
	fmt.Fprintf(&b, "layoutName: %s\n", r.LayoutName)
	fmt.Fprintf(&b, "source point count: %d\n", r.SourcePointCount)
	fmt.Fprintf(&b, "source segment count: %d\n", r.SourceSegmentCount)
	fmt.Fprintf(&b, "source segment length meters: min %.2fm p50 %.2fm p95 %.2fm max %.2fm\n", r.SourceSegmentLengthMinMeters, r.SourceSegmentLengthP50Meters, r.SourceSegmentLengthP95Meters, r.SourceSegmentLengthMaxMeters)
	fmt.Fprintf(&b, "source planar heading change (X/Z): %.2fdeg total, %.4fdeg/m\n", r.SourceHeadingChangeDegrees, r.SourceHeadingChangePerMeter)
	if r.SourceLocalChordWindowCount == 0 || math.IsNaN(r.SourceLocalChordRatioAvg) || math.IsNaN(r.SourceLocalChordExcessAvg) {
		fmt.Fprintf(&b, "source local chord excess (%d-point windows, %d windows): n/a\n", r.SourceLocalChordWindowPoints, r.SourceLocalChordWindowCount)
	} else {
		fmt.Fprintf(&b, "source local chord excess (%d-point windows, %d windows): ratio avg %.4f p50 %.4f p95 %.4f max %.4f; excess meters avg %.2fm p50 %.2fm p95 %.2fm max %.2fm\n", r.SourceLocalChordWindowPoints, r.SourceLocalChordWindowCount, r.SourceLocalChordRatioAvg, r.SourceLocalChordRatioP50, r.SourceLocalChordRatioP95, r.SourceLocalChordRatioMax, r.SourceLocalChordExcessAvg, r.SourceLocalChordExcessP50, r.SourceLocalChordExcessP95, r.SourceLocalChordExcessMax)
	}
	fmt.Fprintf(&b, "source path length meters: %.2fm\n", r.SourcePathLengthMeters)
	fmt.Fprintf(&b, "simplified point count: %d\n", r.SimplifiedPointCount)
	fmt.Fprintf(&b, "simplification dropped points: %d\n", r.SimplificationDroppedPoints)
	fmt.Fprintf(&b, "generated point count: %d\n", r.GeneratedPointCount)
	fmt.Fprintf(&b, "generated path length meters: %.2fm\n", r.GeneratedPathLengthMeters)
	fmt.Fprintf(&b, "catalogLengthMeters: %.2fm\n", r.CatalogLengthMeters)
	fmt.Fprintf(&b, "deltaMeters: %.2fm\n", r.DeltaMeters)
	fmt.Fprintf(&b, "deltaPct: %.2f%%\n", r.DeltaPct)
	fmt.Fprintf(&b, "start-end gap: %.2fm\n", r.StartEndGapMeters)
	if math.IsNaN(r.MeanDeviationMeters) || math.IsNaN(r.MaxDeviationMeters) {
		fmt.Fprintf(&b, "deviation vs catalog centerline: n/a\n")
	} else {
		fmt.Fprintf(&b, "deviation vs catalog centerline: mean %.2fm max %.2fm\n", r.MeanDeviationMeters, r.MaxDeviationMeters)
	}

	return b.String()
}
