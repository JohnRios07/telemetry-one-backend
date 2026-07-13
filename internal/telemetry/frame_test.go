package telemetry

import (
	"errors"
	"math"
	"testing"
)

func TestIngestBatchRequestValidate(t *testing.T) {
	validFrame := validFrame()

	tests := []struct {
		name    string
		request IngestBatchRequest
		wantErr error
	}{
		{name: "valid", request: IngestBatchRequest{SessionID: "session-1", Frames: []Frame{validFrame}}},
		{name: "missing session", request: IngestBatchRequest{Frames: []Frame{validFrame}}, wantErr: ErrMissingSessionID},
		{name: "empty frames", request: IngestBatchRequest{SessionID: "session-1"}, wantErr: ErrEmptyFrames},
		{name: "invalid frame", request: IngestBatchRequest{SessionID: "session-1", Frames: []Frame{{Throttle: 0.5}}}, wantErr: ErrInvalidTimestamp},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.request.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestFrameValidateRejectsControlRanges(t *testing.T) {
	tests := []struct {
		name    string
		frame   Frame
		wantErr error
	}{
		{name: "valid", frame: validFrame()},
		{name: "bad throttle", frame: withFrame(func(frame *Frame) { frame.Throttle = 1.1 }), wantErr: ErrInvalidThrottle},
		{name: "bad brake", frame: withFrame(func(frame *Frame) { frame.Brake = -0.1 }), wantErr: ErrInvalidBrake},
		{name: "bad lap", frame: withFrame(func(frame *Frame) { frame.LapNumber = -1 }), wantErr: ErrInvalidLapNumber},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.frame.Validate()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestIngestBatchRequestReturnsTypedRejectionErrors(t *testing.T) {
	request := IngestBatchRequest{SessionID: "session-1", Frames: []Frame{withFrame(func(frame *Frame) { frame.Throttle = 1.1 })}}

	result, err := request.Normalize()
	if err != nil {
		t.Fatalf("expected no batch-level error, got %v", err)
	}
	if len(result.Frames) != 0 {
		t.Fatalf("expected 0 accepted frames, got %d", len(result.Frames))
	}
	if len(result.Rejections) != 1 {
		t.Fatalf("expected 1 rejection, got %d", len(result.Rejections))
	}
	rejection := result.Rejections[0]
	if rejection.Code != RejectionCodeInvalidThrottle || rejection.Category != RejectionCategoryFrame || rejection.Field != "throttle" {
		t.Fatalf("unexpected rejection: %+v", rejection)
	}
	if rejection.FrameIndex == nil || *rejection.FrameIndex != 0 {
		t.Fatalf("expected frame index 0, got %+v", rejection.FrameIndex)
	}
	if !errors.Is(rejection.Err, ErrInvalidThrottle) {
		t.Fatalf("expected errors.Is to match ErrInvalidThrottle, got %v", rejection.Err)
	}
}

func TestIngestBatchRequestRejectsBatchInconsistencies(t *testing.T) {
	tests := []struct {
		name       string
		frames     []Frame
		wantReject bool
		code       string
		field      string
		wantFrames int
	}{
		{
			name:       "non-monotonic timestamp",
			frames:     []Frame{validFrame(), withFrame(func(frame *Frame) { frame.TimestampUnixMs = 1720656000000 })},
			wantReject: true,
			code:       RejectionCodeNonMonotonicTimestamp,
			field:      "timestampUnixMs",
			wantFrames: 1,
		},
		{
			name:       "lap number regression",
			frames:     []Frame{withFrame(func(frame *Frame) { frame.LapNumber = 2 }), withFrame(func(frame *Frame) { frame.TimestampUnixMs++; frame.LapNumber = 1 })},
			wantReject: true,
			code:       RejectionCodeLapNumberRegressed,
			field:      "lapNumber",
			wantFrames: 1,
		},
		{
			name:       "current lap time regression within same lap",
			frames:     []Frame{validFrame(), withFrame(func(frame *Frame) { frame.TimestampUnixMs++; frame.CurrentLapMs-- })},
			wantReject: true,
			code:       RejectionCodeCurrentLapTimeRegressed,
			field:      "currentLapMs",
			wantFrames: 1,
		},
		{
			name: "current lap time may reset on next lap",
			frames: []Frame{
				withFrame(func(frame *Frame) { frame.LapNumber = 1; frame.CurrentLapMs = 90000 }),
				withFrame(func(frame *Frame) { frame.TimestampUnixMs++; frame.LapNumber = 2; frame.CurrentLapMs = 100 }),
			},
			wantFrames: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := (IngestBatchRequest{SessionID: "session-1", Frames: tt.frames}).Normalize()
			if err != nil {
				t.Fatalf("expected no batch-level error, got %v", err)
			}
			if len(result.Frames) != tt.wantFrames {
				t.Fatalf("expected %d accepted frames, got %d", tt.wantFrames, len(result.Frames))
			}
			if !tt.wantReject {
				if len(result.Rejections) != 0 {
					t.Fatalf("expected no rejections, got %d", len(result.Rejections))
				}
				return
			}
			if len(result.Rejections) != 1 {
				t.Fatalf("expected 1 rejection, got %d", len(result.Rejections))
			}
			rejection := result.Rejections[0]
			if rejection.Code != tt.code || rejection.Field != tt.field {
				t.Fatalf("unexpected rejection: %+v", rejection)
			}
			if rejection.FrameIndex == nil || *rejection.FrameIndex != 1 {
				t.Fatalf("expected frame index 1, got %+v", rejection.FrameIndex)
			}
		})
	}
}

func TestBuildRejectionSummaryGroupsByCode(t *testing.T) {
	rejections := []*RejectionError{
		{Code: "invalid_throttle"},
		{Code: "invalid_speed"},
		{Code: "invalid_throttle"},
	}
	summary := BuildRejectionSummary(rejections)
	if len(summary.Reasons) != 2 {
		t.Fatalf("expected 2 unique reasons, got %d", len(summary.Reasons))
	}
	for _, r := range summary.Reasons {
		switch r.Code {
		case "invalid_throttle":
			if r.Count != 2 {
				t.Fatalf("expected count 2 for invalid_throttle, got %d", r.Count)
			}
		case "invalid_speed":
			if r.Count != 1 {
				t.Fatalf("expected count 1 for invalid_speed, got %d", r.Count)
			}
		default:
			t.Fatalf("unexpected reason code: %s", r.Code)
		}
	}
}

func TestBuildRejectionSummaryEmpty(t *testing.T) {
	summary := BuildRejectionSummary(nil)
	if len(summary.Reasons) != 0 {
		t.Fatalf("expected empty summary, got %+v", summary.Reasons)
	}
}

func TestNormalizeValidatesCanonicalFrameRules(t *testing.T) {
	tests := []struct {
		name    string
		frame   Frame
		wantErr error
	}{
		{name: "valid", frame: validFrame()},
		{name: "speed must be finite", frame: withFrame(func(frame *Frame) { frame.SpeedMps = math.NaN() }), wantErr: ErrInvalidSpeed},
		{name: "speed cannot be negative", frame: withFrame(func(frame *Frame) { frame.SpeedMps = -0.01 }), wantErr: ErrInvalidSpeed},
		{name: "rpm cannot be negative", frame: withFrame(func(frame *Frame) { frame.RPM = -1 }), wantErr: ErrInvalidRPM},
		{name: "fuel cannot be negative", frame: withFrame(func(frame *Frame) { frame.FuelLiters = -1 }), wantErr: ErrInvalidFuel},
		{name: "position must be finite", frame: withFrame(func(frame *Frame) { frame.PositionZ = math.Inf(1) }), wantErr: ErrInvalidPosition},
		{name: "current lap time cannot be negative", frame: withFrame(func(frame *Frame) { frame.CurrentLapMs = -1 }), wantErr: ErrInvalidLapTime},
		{name: "last lap time cannot be negative", frame: withFrame(func(frame *Frame) { frame.LastLapMs = ptrInt64(-1) }), wantErr: ErrInvalidLapTime},
		{name: "best lap time cannot be negative", frame: withFrame(func(frame *Frame) { frame.BestLapMs = ptrInt64(-1) }), wantErr: ErrInvalidLapTime},
		{name: "yaw must be finite when present", frame: withFrame(func(frame *Frame) { frame.YawRadians = ptrFloat64(math.Inf(1)) }), wantErr: ErrInvalidYaw},
		{name: "yaw rate must be finite when present", frame: withFrame(func(frame *Frame) { frame.YawRate = ptrFloat64(math.NaN()) }), wantErr: ErrInvalidYaw},
		{name: "wheel speed cannot be negative when present", frame: withFrame(func(frame *Frame) { frame.WheelSpeedRL = ptrFloat64(-0.1) }), wantErr: ErrInvalidWheelSpeed},
		{name: "steering must be finite", frame: withFrame(func(frame *Frame) { frame.Steering = math.NaN() }), wantErr: ErrInvalidSteering},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.frame.Normalize()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestNormalizePreservesMissingOptionalFields(t *testing.T) {
	frame := validFrame()
	frame.YawRadians = nil
	frame.YawRate = nil
	frame.WheelSpeedFL = nil
	frame.WheelSpeedFR = nil
	frame.WheelSpeedRL = nil
	frame.WheelSpeedRR = nil
	frame.LastLapMs = nil
	frame.BestLapMs = nil

	normalized, err := frame.Normalize()
	if err != nil {
		t.Fatalf("Normalize returned error: %v", err)
	}
	if normalized.YawRadians != nil || normalized.YawRate != nil {
		t.Fatalf("expected missing yaw fields to stay nil")
	}
	if normalized.WheelSpeedFL != nil || normalized.WheelSpeedFR != nil || normalized.WheelSpeedRL != nil || normalized.WheelSpeedRR != nil {
		t.Fatalf("expected missing wheel speeds to stay nil")
	}
	if normalized.LastLapMs != nil || normalized.BestLapMs != nil {
		t.Fatalf("expected missing lap history to stay nil")
	}
}

func TestNormalizeDoesNotClampControls(t *testing.T) {
	_, err := withFrame(func(frame *Frame) { frame.Throttle = 1.01 }).Normalize()
	if !errors.Is(err, ErrInvalidThrottle) {
		t.Fatalf("expected throttle above range to be rejected, got %v", err)
	}

	_, err = withFrame(func(frame *Frame) { frame.Brake = -0.01 }).Normalize()
	if !errors.Is(err, ErrInvalidBrake) {
		t.Fatalf("expected brake below range to be rejected, got %v", err)
	}
}

func validFrame() Frame {
	return Frame{
		TimestampUnixMs: 1720656000000,
		SpeedMps:        58.33,
		RPM:             7100,
		Gear:            4,
		Throttle:        0.7,
		Brake:           0.2,
		Steering:        -0.12,
		FuelLiters:      38.4,
		PositionX:       123.4,
		PositionY:       5.6,
		PositionZ:       789.1,
		YawRadians:      ptrFloat64(1.57),
		YawRate:         ptrFloat64(0.03),
		WheelSpeedFL:    ptrFloat64(58.1),
		WheelSpeedFR:    ptrFloat64(58.2),
		WheelSpeedRL:    ptrFloat64(58.4),
		WheelSpeedRR:    ptrFloat64(58.3),
		LapNumber:       1,
		CurrentLapMs:    81234,
		LastLapMs:       ptrInt64(91345),
		BestLapMs:       ptrInt64(90210),
		IsOnTrack:       true,
	}
}

func withFrame(change func(*Frame)) Frame {
	frame := validFrame()
	change(&frame)
	return frame
}

func ptrFloat64(value float64) *float64 {
	return &value
}

func ptrInt64(value int64) *int64 {
	return &value
}
