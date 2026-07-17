package laps

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"telemetry-one-backend/internal/telemetry"
)

const (
	DefaultSampleStepMeters            = 5
	LapSampleSourceExplicitLapDistance = "explicit_lap_distance"
)

var (
	ErrMissingLapID           = errors.New("lapID is required")
	ErrInvalidSampleDistance  = errors.New("distanceMeters must be zero or greater")
	ErrInvalidSampleSource    = errors.New("source is required")
	ErrInvalidSampleStep      = errors.New("stepMeters must be greater than zero")
	ErrInvalidSampleTimestamp = errors.New("timestampUnixMs must be greater than zero when provided")
	ErrInvalidSampleBucket    = errors.New("distanceMeters must align to the sample step")
)

type LapSample struct {
	ID              string
	LapID           string
	DistanceMeters  int
	Source          string
	TimestampUnixMs *int64
	SpeedMps        *float64
	RPM             *float64
	Gear            *int
	Throttle        *float64
	Brake           *float64
	Steering        *float64
	FuelLiters      *float64
	YawRadians      *float64
	YawRate         *float64
	WheelSpeedFL    *float64
	WheelSpeedFR    *float64
	WheelSpeedRL    *float64
	WheelSpeedRR    *float64
}

func NewLapSampleID(lapID string, distanceMeters int) string {
	return fmt.Sprintf("lap_sample_%s_%d", lapID, distanceMeters)
}

func (sample LapSample) Validate() error {
	if sample.LapID == "" {
		return ErrMissingLapID
	}
	if sample.DistanceMeters < 0 {
		return ErrInvalidSampleDistance
	}
	if sample.DistanceMeters%DefaultSampleStepMeters != 0 {
		return ErrInvalidSampleBucket
	}
	if sample.Source == "" {
		return ErrInvalidSampleSource
	}
	if sample.TimestampUnixMs != nil && *sample.TimestampUnixMs <= 0 {
		return ErrInvalidSampleTimestamp
	}
	return nil
}

func (sample LapSample) withDefaults() LapSample {
	if sample.ID == "" && sample.LapID != "" && sample.DistanceMeters >= 0 {
		sample.ID = NewLapSampleID(sample.LapID, sample.DistanceMeters)
	}
	sample.TimestampUnixMs = cloneInt64(sample.TimestampUnixMs)
	sample.SpeedMps = cloneFloat64(sample.SpeedMps)
	sample.RPM = cloneFloat64(sample.RPM)
	sample.Gear = cloneInt(sample.Gear)
	sample.Throttle = cloneFloat64(sample.Throttle)
	sample.Brake = cloneFloat64(sample.Brake)
	sample.Steering = cloneFloat64(sample.Steering)
	sample.FuelLiters = cloneFloat64(sample.FuelLiters)
	sample.YawRadians = cloneFloat64(sample.YawRadians)
	sample.YawRate = cloneFloat64(sample.YawRate)
	sample.WheelSpeedFL = cloneFloat64(sample.WheelSpeedFL)
	sample.WheelSpeedFR = cloneFloat64(sample.WheelSpeedFR)
	sample.WheelSpeedRL = cloneFloat64(sample.WheelSpeedRL)
	sample.WheelSpeedRR = cloneFloat64(sample.WheelSpeedRR)
	return sample
}

func BuildLapSamples(lap CompletedLap, frames []telemetry.Frame, stepMeters int) ([]LapSample, bool) {
	if stepMeters <= 0 || lap.ID == "" {
		return nil, false
	}

	lapFrames := make([]telemetry.Frame, 0, len(frames))
	for _, frame := range frames {
		if frame.LapNumber == lap.LapNumber {
			lapFrames = append(lapFrames, frame)
		}
	}
	if len(lapFrames) < 2 {
		return nil, false
	}
	sort.SliceStable(lapFrames, func(i, j int) bool { return lapFrames[i].TimestampUnixMs < lapFrames[j].TimestampUnixMs })
	for i, frame := range lapFrames {
		if frame.LapDistanceMeters == nil || math.IsNaN(*frame.LapDistanceMeters) || math.IsInf(*frame.LapDistanceMeters, 0) || *frame.LapDistanceMeters < 0 {
			return nil, false
		}
		if i > 0 && *frame.LapDistanceMeters < *lapFrames[i-1].LapDistanceMeters {
			return nil, false
		}
	}
	lapFrames = collapseDuplicateDistanceFrames(lapFrames)
	if len(lapFrames) < 2 {
		return nil, false
	}

	firstDistance := *lapFrames[0].LapDistanceMeters
	lastDistance := *lapFrames[len(lapFrames)-1].LapDistanceMeters
	if lastDistance <= firstDistance {
		return nil, false
	}

	samples := make([]LapSample, 0, int(lastDistance)/stepMeters+1)
	for bucket := 0; float64(bucket) <= lastDistance; bucket += stepMeters {
		if float64(bucket) < firstDistance {
			continue
		}
		left, right, ok := bracketFrames(lapFrames, float64(bucket))
		if !ok {
			continue
		}
		samples = append(samples, interpolateSample(lap.ID, bucket, left, right))
	}
	if len(samples) == 0 {
		return nil, false
	}
	return samples, true
}

func bracketFrames(frames []telemetry.Frame, distance float64) (telemetry.Frame, telemetry.Frame, bool) {
	for i := 0; i < len(frames)-1; i++ {
		left := frames[i]
		right := frames[i+1]
		ld := *left.LapDistanceMeters
		rd := *right.LapDistanceMeters
		if distance == ld {
			return left, left, true
		}
		if distance >= ld && distance <= rd && rd > ld {
			return left, right, true
		}
	}
	last := frames[len(frames)-1]
	if distance == *last.LapDistanceMeters {
		return last, last, true
	}
	return telemetry.Frame{}, telemetry.Frame{}, false
}

func interpolateSample(lapID string, distanceMeters int, left telemetry.Frame, right telemetry.Frame) LapSample {
	ratio := interpolationRatio(float64(distanceMeters), left, right)
	return LapSample{
		LapID:           lapID,
		DistanceMeters:  distanceMeters,
		Source:          LapSampleSourceExplicitLapDistance,
		TimestampUnixMs: interpolateInt64Ptr(left.TimestampUnixMs, right.TimestampUnixMs, ratio),
		SpeedMps:        float64Ptr(interpolateFloat64(left.SpeedMps, right.SpeedMps, ratio)),
		RPM:             float64Ptr(interpolateFloat64(left.RPM, right.RPM, ratio)),
		Gear:            intPtr(nearestGear(left.Gear, right.Gear, ratio)),
		Throttle:        float64Ptr(interpolateFloat64(left.Throttle, right.Throttle, ratio)),
		Brake:           float64Ptr(interpolateFloat64(left.Brake, right.Brake, ratio)),
		Steering:        float64Ptr(interpolateFloat64(left.Steering, right.Steering, ratio)),
		FuelLiters:      float64Ptr(interpolateFloat64(left.FuelLiters, right.FuelLiters, ratio)),
		YawRadians:      interpolateOptionalFloat64(left.YawRadians, right.YawRadians, ratio),
		YawRate:         interpolateOptionalFloat64(left.YawRate, right.YawRate, ratio),
		WheelSpeedFL:    interpolateOptionalFloat64(left.WheelSpeedFL, right.WheelSpeedFL, ratio),
		WheelSpeedFR:    interpolateOptionalFloat64(left.WheelSpeedFR, right.WheelSpeedFR, ratio),
		WheelSpeedRL:    interpolateOptionalFloat64(left.WheelSpeedRL, right.WheelSpeedRL, ratio),
		WheelSpeedRR:    interpolateOptionalFloat64(left.WheelSpeedRR, right.WheelSpeedRR, ratio),
	}.withDefaults()
}

func interpolationRatio(distance float64, left telemetry.Frame, right telemetry.Frame) float64 {
	ld := *left.LapDistanceMeters
	rd := *right.LapDistanceMeters
	if rd == ld {
		return 0
	}
	return (distance - ld) / (rd - ld)
}

func interpolateFloat64(left, right, ratio float64) float64 { return left + (right-left)*ratio }

func interpolateOptionalFloat64(left, right *float64, ratio float64) *float64 {
	if ratio <= 0 {
		return left
	}
	if ratio >= 1 {
		return right
	}
	if left == nil || right == nil {
		return nil
	}
	return float64Ptr(interpolateFloat64(*left, *right, ratio))
}

func interpolateInt64Ptr(left, right int64, ratio float64) *int64 {
	return int64Ptr(int64(math.Round(float64(left) + (float64(right)-float64(left))*ratio)))
}

func nearestGear(left, right int, ratio float64) int {
	if ratio < 0.5 {
		return left
	}
	return right
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func float64Ptr(value float64) *float64 { return &value }
func int64Ptr(value int64) *int64       { return &value }
func intPtr(value int) *int             { return &value }

func cloneSamples(input []LapSample) []LapSample {
	cloned := make([]LapSample, len(input))
	for i, sample := range input {
		cloned[i] = sample.withDefaults()
	}
	return cloned
}

func sortByDistance(samples []LapSample) {
	sort.Slice(samples, func(i, j int) bool {
		if samples[i].DistanceMeters == samples[j].DistanceMeters {
			return samples[i].ID < samples[j].ID
		}
		return samples[i].DistanceMeters < samples[j].DistanceMeters
	})
}

func collapseDuplicateDistanceFrames(frames []telemetry.Frame) []telemetry.Frame {
	if len(frames) == 0 {
		return frames
	}

	collapsed := frames[:1]
	for i := 1; i < len(frames); i++ {
		last := collapsed[len(collapsed)-1]
		if *frames[i].LapDistanceMeters == *last.LapDistanceMeters {
			collapsed[len(collapsed)-1] = frames[i]
			continue
		}
		collapsed = append(collapsed, frames[i])
	}
	return collapsed
}
