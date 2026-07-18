package trackbuilder

import (
	"fmt"
	"math"
	"strings"
)

type Report struct {
	LayoutID                  string
	LayoutName                string
	TelemetryPathLengthMeters float64
	CatalogLengthMeters       float64
	DeltaMeters               float64
	DeltaPct                  float64
	PointCount                int
	StartEndGapMeters         float64
	MeanDeviationMeters       float64
	MaxDeviationMeters        float64
}

func (r Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "trackbuilder closing report\n")
	fmt.Fprintf(&b, "layoutId: %s\n", r.LayoutID)
	fmt.Fprintf(&b, "layoutName: %s\n", r.LayoutName)
	fmt.Fprintf(&b, "telemetry/generated path length meters: %.2fm\n", r.TelemetryPathLengthMeters)
	fmt.Fprintf(&b, "catalogLengthMeters: %.2fm\n", r.CatalogLengthMeters)
	fmt.Fprintf(&b, "deltaMeters: %.2fm\n", r.DeltaMeters)
	fmt.Fprintf(&b, "deltaPct: %.2f%%\n", r.DeltaPct)
	fmt.Fprintf(&b, "points: %d\n", r.PointCount)
	fmt.Fprintf(&b, "start-end gap: %.2fm\n", r.StartEndGapMeters)
	if math.IsNaN(r.MeanDeviationMeters) || math.IsNaN(r.MaxDeviationMeters) {
		fmt.Fprintf(&b, "deviation vs catalog centerline: n/a\n")
	} else {
		fmt.Fprintf(&b, "deviation vs catalog centerline: mean %.2fm max %.2fm\n", r.MeanDeviationMeters, r.MaxDeviationMeters)
	}

	return b.String()
}
