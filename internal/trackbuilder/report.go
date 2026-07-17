package trackbuilder

import (
	"fmt"
	"math"
	"strings"
)

type Report struct {
	LengthMeters        float64
	PointCount          int
	StartEndGapMeters   float64
	MeanDeviationMeters float64
	MaxDeviationMeters  float64
}

func (r Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "trackbuilder closing report\n")
	fmt.Fprintf(&b, "length: %.2fm\n", r.LengthMeters)
	fmt.Fprintf(&b, "points: %d\n", r.PointCount)
	fmt.Fprintf(&b, "start-end gap: %.2fm\n", r.StartEndGapMeters)
	if math.IsNaN(r.MeanDeviationMeters) || math.IsNaN(r.MaxDeviationMeters) {
		fmt.Fprintf(&b, "deviation vs catalog: n/a\n")
	} else {
		fmt.Fprintf(&b, "deviation vs catalog: mean %.2fm max %.2fm\n", r.MeanDeviationMeters, r.MaxDeviationMeters)
	}

	return b.String()
}
