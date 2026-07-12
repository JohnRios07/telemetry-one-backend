package sim

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"time"

	"telemetry-one-backend/internal/telemetry"
)

const (
	DefaultSessionID   = "synthetic-session-001"
	DefaultStartUnixMs = int64(1720656000000)
	DefaultLapDuration = 9 * time.Second
)

var (
	ErrInvalidFrequency = errors.New("frequencyHz must be one of 20, 30, or 60")
	ErrInvalidDuration  = errors.New("lap duration must be greater than zero")
)

type Options struct {
	SessionID   string
	FrequencyHz int
	StartUnixMs int64
	LapDuration time.Duration
}

func GenerateBatch(options Options) (telemetry.IngestBatchRequest, error) {
	if options.SessionID == "" {
		options.SessionID = DefaultSessionID
	}
	if options.StartUnixMs == 0 {
		options.StartUnixMs = DefaultStartUnixMs
	}
	if options.LapDuration == 0 {
		options.LapDuration = DefaultLapDuration
	}
	if options.LapDuration < 0 {
		return telemetry.IngestBatchRequest{}, ErrInvalidDuration
	}
	if !isSupportedFrequency(options.FrequencyHz) {
		return telemetry.IngestBatchRequest{}, ErrInvalidFrequency
	}

	interval := time.Second / time.Duration(options.FrequencyHz)
	frameCount := int(options.LapDuration/interval) + 1
	frames := make([]telemetry.Frame, 0, frameCount)

	for i := 0; i < frameCount; i++ {
		elapsed := time.Duration(i) * interval
		if elapsed > options.LapDuration {
			elapsed = options.LapDuration
		}

		progress := elapsed.Seconds() / options.LapDuration.Seconds()
		frames = append(frames, syntheticFrame(options.StartUnixMs, elapsed, progress))
	}

	request := telemetry.IngestBatchRequest{SessionID: options.SessionID, Frames: frames}
	if err := request.Validate(); err != nil {
		return telemetry.IngestBatchRequest{}, err
	}

	return request, nil
}

func LoadFixture(path string) (telemetry.IngestBatchRequest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return telemetry.IngestBatchRequest{}, err
	}

	var request telemetry.IngestBatchRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return telemetry.IngestBatchRequest{}, err
	}
	if err := request.Validate(); err != nil {
		return telemetry.IngestBatchRequest{}, err
	}

	return request, nil
}

func isSupportedFrequency(frequencyHz int) bool {
	switch frequencyHz {
	case 20, 30, 60:
		return true
	default:
		return false
	}
}

func syntheticFrame(startUnixMs int64, elapsed time.Duration, progress float64) telemetry.Frame {
	theta := 2 * math.Pi * progress
	brake := brakingForProgress(progress)
	throttle := throttleForProgress(progress, brake)
	speed := speedForProgress(progress, brake)
	wheelSpeed := speed * (1 + 0.01*math.Sin(4*theta))

	return telemetry.Frame{
		TimestampUnixMs: startUnixMs + elapsed.Milliseconds(),
		SpeedMps:        round(speed, 3),
		RPM:             round(2500+throttle*5200-brake*800, 1),
		Gear:            gearForSpeed(speed),
		Throttle:        round(throttle, 3),
		Brake:           round(brake, 3),
		Steering:        round(0.34*math.Sin(theta)+0.08*math.Sin(3*theta), 3),
		FuelLiters:      round(40-progress*2.4, 3),
		PositionX:       round(420*math.Cos(theta), 3),
		PositionY:       round(3*math.Sin(2*theta), 3),
		PositionZ:       round(260*math.Sin(theta), 3),
		YawRadians:      ptrFloat64(round(math.Atan2(260*math.Cos(theta), -420*math.Sin(theta)), 3)),
		YawRate:         ptrFloat64(round(0.08*math.Cos(theta), 3)),
		WheelSpeedFL:    ptrFloat64(round(wheelSpeed*(1-0.004*brake), 3)),
		WheelSpeedFR:    ptrFloat64(round(wheelSpeed*(1-0.003*brake), 3)),
		WheelSpeedRL:    ptrFloat64(round(wheelSpeed*(1+0.004*throttle), 3)),
		WheelSpeedRR:    ptrFloat64(round(wheelSpeed*(1+0.005*throttle), 3)),
		LapNumber:       1,
		CurrentLapMs:    elapsed.Milliseconds(),
		LastLapMs:       ptrInt64(0),
		BestLapMs:       ptrInt64(int64(DefaultLapDuration / time.Millisecond)),
		IsOnTrack:       true,
	}
}

func brakingForProgress(progress float64) float64 {
	if inWindow(progress, 0.18, 0.24) || inWindow(progress, 0.58, 0.64) || inWindow(progress, 0.82, 0.87) {
		return 0.75
	}
	return 0
}

func throttleForProgress(progress, brake float64) float64 {
	if brake > 0 {
		return 0.08
	}
	return 0.62 + 0.28*math.Max(0, math.Sin(2*math.Pi*progress))
}

func speedForProgress(progress, brake float64) float64 {
	base := 45 + 18*math.Sin(2*math.Pi*progress+0.4)
	if brake > 0 {
		base -= 18
	}
	if base < 12 {
		return 12
	}
	return base
}

func inWindow(value, start, end float64) bool {
	return value >= start && value <= end
}

func gearForSpeed(speedMps float64) int {
	switch {
	case speedMps < 12:
		return 1
	case speedMps < 22:
		return 2
	case speedMps < 32:
		return 3
	case speedMps < 44:
		return 4
	case speedMps < 56:
		return 5
	default:
		return 6
	}
}

func round(value float64, precision int) float64 {
	scale := math.Pow10(precision)
	return math.Round(value*scale) / scale
}

func ptrFloat64(value float64) *float64 {
	return &value
}

func ptrInt64(value int64) *int64 {
	return &value
}
