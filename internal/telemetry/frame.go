package telemetry

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

const MaxBatchFrames = 600

var (
	ErrMissingSessionID        = errors.New("sessionId is required")
	ErrSessionIDMismatch       = errors.New("sessionId must match the route parameter")
	ErrEmptyFrames             = errors.New("frames must contain at least one frame")
	ErrTooManyFrames           = errors.New("frames exceeds maximum batch size")
	ErrInvalidTimestamp        = errors.New("timestampUnixMs must be greater than zero")
	ErrInvalidSpeed            = errors.New("speedMps must be finite and zero or greater")
	ErrInvalidRPM              = errors.New("rpm must be finite and zero or greater")
	ErrInvalidThrottle         = errors.New("throttle must be between 0 and 1")
	ErrInvalidBrake            = errors.New("brake must be between 0 and 1")
	ErrInvalidSteering         = errors.New("steering must be finite")
	ErrInvalidFuel             = errors.New("fuelLiters must be finite and zero or greater")
	ErrInvalidPosition         = errors.New("position fields must be finite")
	ErrInvalidYaw              = errors.New("yaw fields must be finite when provided")
	ErrInvalidWheelSpeed       = errors.New("wheel speed fields must be finite and zero or greater when provided")
	ErrInvalidLapNumber        = errors.New("lapNumber must be zero or greater")
	ErrInvalidLapTime          = errors.New("lap time fields must be zero or greater")
	ErrNonMonotonicTimestamp   = errors.New("timestampUnixMs must increase within a batch")
	ErrLapNumberRegressed      = errors.New("lapNumber must not decrease within a batch")
	ErrCurrentLapTimeRegressed = errors.New("currentLapMs must not decrease within the same lap")
)

const (
	RejectionCategoryBatch       = "batch"
	RejectionCategoryFrame       = "frame"
	RejectionCategoryConsistency = "consistency"

	RejectionCodeMissingSessionID        = "session_id_required"
	RejectionCodeSessionIDMismatch       = "session_id_mismatch"
	RejectionCodeEmptyFrames             = "frames_empty"
	RejectionCodeTooManyFrames           = "batch_too_large"
	RejectionCodeInvalidTimestamp        = "invalid_timestamp"
	RejectionCodeInvalidSpeed            = "invalid_speed"
	RejectionCodeInvalidRPM              = "invalid_rpm"
	RejectionCodeInvalidThrottle         = "invalid_throttle"
	RejectionCodeInvalidBrake            = "invalid_brake"
	RejectionCodeInvalidSteering         = "invalid_steering"
	RejectionCodeInvalidFuel             = "invalid_fuel"
	RejectionCodeInvalidPosition         = "invalid_position"
	RejectionCodeInvalidYaw              = "invalid_yaw"
	RejectionCodeInvalidWheelSpeed       = "invalid_wheel_speed"
	RejectionCodeInvalidLapNumber        = "invalid_lap_number"
	RejectionCodeInvalidLapTime          = "invalid_lap_time"
	RejectionCodeNonMonotonicTimestamp   = "non_monotonic_timestamp"
	RejectionCodeLapNumberRegressed      = "lap_number_regressed"
	RejectionCodeCurrentLapTimeRegressed = "current_lap_time_regressed"
)

type RejectionError struct {
	Code       string
	Category   string
	Field      string
	FrameIndex *int
	Message    string
	Err        error
}

type RejectionDetails struct {
	RejectionCode string `json:"rejectionCode"`
	Category      string `json:"category"`
	Field         string `json:"field,omitempty"`
	FrameIndex    *int   `json:"frameIndex,omitempty"`
	MaxFrames     int    `json:"maxFrames,omitempty"`
}

func (e *RejectionError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Code
}

func (e *RejectionError) Unwrap() error {
	return e.Err
}

func (e *RejectionError) Details() RejectionDetails {
	details := RejectionDetails{
		RejectionCode: e.Code,
		Category:      e.Category,
		Field:         e.Field,
		FrameIndex:    e.FrameIndex,
	}
	if e.Code == RejectionCodeTooManyFrames {
		details.MaxFrames = MaxBatchFrames
	}

	return details
}

func SessionIDMismatchError() *RejectionError {
	return reject(RejectionCodeSessionIDMismatch, RejectionCategoryBatch, "sessionId", nil, ErrSessionIDMismatch)
}

type Frame struct {
	TimestampUnixMs int64    `json:"timestampUnixMs"`
	SpeedMps        float64  `json:"speedMps"`
	RPM             float64  `json:"rpm"`
	Gear            int      `json:"gear"`
	Throttle        float64  `json:"throttle"`
	Brake           float64  `json:"brake"`
	Steering        float64  `json:"steering"`
	FuelLiters      float64  `json:"fuelLiters"`
	PositionX       float64  `json:"positionX"`
	PositionY       float64  `json:"positionY"`
	PositionZ       float64  `json:"positionZ"`
	YawRadians      *float64 `json:"yawRadians,omitempty"`
	YawRate         *float64 `json:"yawRate,omitempty"`
	WheelSpeedFL    *float64 `json:"wheelSpeedFL,omitempty"`
	WheelSpeedFR    *float64 `json:"wheelSpeedFR,omitempty"`
	WheelSpeedRL    *float64 `json:"wheelSpeedRL,omitempty"`
	WheelSpeedRR    *float64 `json:"wheelSpeedRR,omitempty"`
	LapNumber       int      `json:"lapNumber"`
	CurrentLapMs    int64    `json:"currentLapMs"`
	LastLapMs       *int64   `json:"lastLapMs,omitempty"`
	BestLapMs       *int64   `json:"bestLapMs,omitempty"`
	IsOnTrack       bool     `json:"isOnTrack"`
}

type IngestBatchRequest struct {
	SessionID string  `json:"sessionId"`
	Frames    []Frame `json:"frames"`
}

type RejectionReasonCount struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

type RejectionSummary struct {
	Reasons []RejectionReasonCount `json:"reasons"`
}

type IngestBatchResponse struct {
	SessionID          string            `json:"sessionId"`
	ReceivedFrames     int               `json:"receivedFrames"`
	AcceptedFrames     int               `json:"acceptedFrames"`
	RejectedFrames     int               `json:"rejectedFrames"`
	AcceptedFromUnixMs int64             `json:"acceptedFromUnixMs"`
	AcceptedToUnixMs   int64             `json:"acceptedToUnixMs"`
	Status             string            `json:"status"`
	RejectionSummary   *RejectionSummary `json:"rejectionSummary,omitempty"`
}

type NormalizeResult struct {
	Frames     []Frame
	Rejections []*RejectionError
}

func (r IngestBatchRequest) Validate() error {
	result, err := r.Normalize()
	if err != nil {
		return err
	}
	if len(result.Rejections) > 0 {
		return result.Rejections[0]
	}
	return nil
}

func (r IngestBatchRequest) Normalize() (NormalizeResult, error) {
	if r.SessionID == "" {
		return NormalizeResult{}, reject(RejectionCodeMissingSessionID, RejectionCategoryBatch, "sessionId", nil, ErrMissingSessionID)
	}
	if len(r.Frames) == 0 {
		return NormalizeResult{}, reject(RejectionCodeEmptyFrames, RejectionCategoryBatch, "frames", nil, ErrEmptyFrames)
	}
	if len(r.Frames) > MaxBatchFrames {
		return NormalizeResult{}, reject(RejectionCodeTooManyFrames, RejectionCategoryBatch, "frames", nil, ErrTooManyFrames)
	}

	var accepted []Frame
	var rejections []*RejectionError

	for index, frame := range r.Frames {
		normalizedFrame, err := frame.Normalize()
		if err != nil {
			var rejection *RejectionError
			if errors.As(err, &rejection) {
				frameIndex := index
				rej := &RejectionError{
					Code:       rejection.Code,
					Category:   rejection.Category,
					Field:      rejection.Field,
					FrameIndex: &frameIndex,
					Message:    fmt.Sprintf("frames[%d]: %s", index, rejection.Error()),
					Err:        rejection.Err,
				}
				rejections = append(rejections, rej)
				continue
			}
			return NormalizeResult{}, fmt.Errorf("frames[%d]: %w", index, err)
		}
		if err := validateBatchConsistency(accepted, normalizedFrame, index); err != nil {
			var rejection *RejectionError
			if errors.As(err, &rejection) {
				rejections = append(rejections, rejection)
				continue
			}
			return NormalizeResult{}, err
		}
		accepted = append(accepted, normalizedFrame)
	}

	return NormalizeResult{Frames: accepted, Rejections: rejections}, nil
}

func BuildRejectionSummary(rejections []*RejectionError) RejectionSummary {
	counts := make(map[string]int)
	for _, r := range rejections {
		counts[r.Code]++
	}
	reasons := make([]RejectionReasonCount, 0, len(counts))
	for code, count := range counts {
		reasons = append(reasons, RejectionReasonCount{Code: code, Count: count})
	}
	sort.SliceStable(reasons, func(i, j int) bool {
		if reasons[i].Count != reasons[j].Count {
			return reasons[i].Count > reasons[j].Count
		}
		return reasons[i].Code < reasons[j].Code
	})
	return RejectionSummary{Reasons: reasons}
}

func (f Frame) Validate() error {
	_, err := f.Normalize()
	return err
}

func (f Frame) Normalize() (Frame, error) {
	if f.TimestampUnixMs <= 0 {
		return Frame{}, reject(RejectionCodeInvalidTimestamp, RejectionCategoryFrame, "timestampUnixMs", nil, ErrInvalidTimestamp)
	}
	if !isFiniteNonNegative(f.SpeedMps) {
		return Frame{}, reject(RejectionCodeInvalidSpeed, RejectionCategoryFrame, "speedMps", nil, ErrInvalidSpeed)
	}
	if !isFiniteNonNegative(f.RPM) {
		return Frame{}, reject(RejectionCodeInvalidRPM, RejectionCategoryFrame, "rpm", nil, ErrInvalidRPM)
	}
	if !isFinite(f.Throttle) || f.Throttle < 0 || f.Throttle > 1 {
		return Frame{}, reject(RejectionCodeInvalidThrottle, RejectionCategoryFrame, "throttle", nil, ErrInvalidThrottle)
	}
	if !isFinite(f.Brake) || f.Brake < 0 || f.Brake > 1 {
		return Frame{}, reject(RejectionCodeInvalidBrake, RejectionCategoryFrame, "brake", nil, ErrInvalidBrake)
	}
	if !isFinite(f.Steering) {
		return Frame{}, reject(RejectionCodeInvalidSteering, RejectionCategoryFrame, "steering", nil, ErrInvalidSteering)
	}
	if !isFiniteNonNegative(f.FuelLiters) {
		return Frame{}, reject(RejectionCodeInvalidFuel, RejectionCategoryFrame, "fuelLiters", nil, ErrInvalidFuel)
	}
	if !isFinite(f.PositionX) || !isFinite(f.PositionY) || !isFinite(f.PositionZ) {
		return Frame{}, reject(RejectionCodeInvalidPosition, RejectionCategoryFrame, "position", nil, ErrInvalidPosition)
	}
	if !isOptionalFinite(f.YawRadians) || !isOptionalFinite(f.YawRate) {
		return Frame{}, reject(RejectionCodeInvalidYaw, RejectionCategoryFrame, "yaw", nil, ErrInvalidYaw)
	}
	if !isOptionalFiniteNonNegative(f.WheelSpeedFL) || !isOptionalFiniteNonNegative(f.WheelSpeedFR) || !isOptionalFiniteNonNegative(f.WheelSpeedRL) || !isOptionalFiniteNonNegative(f.WheelSpeedRR) {
		return Frame{}, reject(RejectionCodeInvalidWheelSpeed, RejectionCategoryFrame, "wheelSpeed", nil, ErrInvalidWheelSpeed)
	}
	if f.LapNumber < 0 {
		return Frame{}, reject(RejectionCodeInvalidLapNumber, RejectionCategoryFrame, "lapNumber", nil, ErrInvalidLapNumber)
	}
	if f.CurrentLapMs < 0 || !isOptionalInt64NonNegative(f.LastLapMs) || !isOptionalInt64NonNegative(f.BestLapMs) {
		return Frame{}, reject(RejectionCodeInvalidLapTime, RejectionCategoryFrame, "lapTime", nil, ErrInvalidLapTime)
	}

	return f, nil
}

func validateBatchConsistency(previous []Frame, current Frame, index int) error {
	if len(previous) == 0 {
		return nil
	}

	last := previous[len(previous)-1]
	if current.TimestampUnixMs <= last.TimestampUnixMs {
		return reject(RejectionCodeNonMonotonicTimestamp, RejectionCategoryConsistency, "timestampUnixMs", &index, ErrNonMonotonicTimestamp)
	}
	if current.LapNumber < last.LapNumber {
		return reject(RejectionCodeLapNumberRegressed, RejectionCategoryConsistency, "lapNumber", &index, ErrLapNumberRegressed)
	}
	if current.LapNumber == last.LapNumber && current.CurrentLapMs < last.CurrentLapMs {
		return reject(RejectionCodeCurrentLapTimeRegressed, RejectionCategoryConsistency, "currentLapMs", &index, ErrCurrentLapTimeRegressed)
	}

	return nil
}

func reject(code string, category string, field string, frameIndex *int, err error) *RejectionError {
	return &RejectionError{Code: code, Category: category, Field: field, FrameIndex: frameIndex, Message: err.Error(), Err: err}
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func isFiniteNonNegative(value float64) bool {
	return isFinite(value) && value >= 0
}

func isOptionalFinite(value *float64) bool {
	return value == nil || isFinite(*value)
}

func isOptionalFiniteNonNegative(value *float64) bool {
	return value == nil || isFiniteNonNegative(*value)
}

func isOptionalInt64NonNegative(value *int64) bool {
	return value == nil || *value >= 0
}
