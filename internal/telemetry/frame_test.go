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

	_, err := request.Normalize()
	var rejection *RejectionError
	if !errors.As(err, &rejection) {
		t.Fatalf("expected RejectionError, got %T %v", err, err)
	}
	if rejection.Code != RejectionCodeInvalidThrottle || rejection.Category != RejectionCategoryFrame || rejection.Field != "throttle" {
		t.Fatalf("unexpected rejection: %+v", rejection)
	}
	if rejection.FrameIndex == nil || *rejection.FrameIndex != 0 {
		t.Fatalf("expected frame index 0, got %+v", rejection.FrameIndex)
	}
	if !errors.Is(err, ErrInvalidThrottle) {
		t.Fatalf("expected errors.Is to match ErrInvalidThrottle, got %v", err)
	}
}

func TestIngestBatchRequestRejectsBatchInconsistencies(t *testing.T) {
	tests := []struct {
		name    string
		frames  []Frame
		wantErr error
		code    string
		field   string
	}{
		{
			name:    "non-monotonic timestamp",
			frames:  []Frame{validFrame(), withFrame(func(frame *Frame) { frame.TimestampUnixMs = 1720656000000 })},
			wantErr: ErrNonMonotonicTimestamp,
			code:    RejectionCodeNonMonotonicTimestamp,
			field:   "timestampUnixMs",
		},
		{
			name:    "lap number regression",
			frames:  []Frame{withFrame(func(frame *Frame) { frame.LapNumber = 2 }), withFrame(func(frame *Frame) { frame.TimestampUnixMs++; frame.LapNumber = 1 })},
			wantErr: ErrLapNumberRegressed,
			code:    RejectionCodeLapNumberRegressed,
			field:   "lapNumber",
		},
		{
			name:    "current lap time regression within same lap",
			frames:  []Frame{validFrame(), withFrame(func(frame *Frame) { frame.TimestampUnixMs++; frame.CurrentLapMs-- })},
			wantErr: ErrCurrentLapTimeRegressed,
			code:    RejectionCodeCurrentLapTimeRegressed,
			field:   "currentLapMs",
		},
		{
			name: "current lap time may reset on next lap",
			frames: []Frame{
				withFrame(func(frame *Frame) { frame.LapNumber = 1; frame.CurrentLapMs = 90000 }),
				withFrame(func(frame *Frame) { frame.TimestampUnixMs++; frame.LapNumber = 2; frame.CurrentLapMs = 100 }),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := (IngestBatchRequest{SessionID: "session-1", Frames: tt.frames}).Normalize()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
			var rejection *RejectionError
			if !errors.As(err, &rejection) {
				t.Fatalf("expected RejectionError, got %T %v", err, err)
			}
			if rejection.Code != tt.code || rejection.Category != RejectionCategoryConsistency || rejection.Field != tt.field {
				t.Fatalf("unexpected rejection: %+v", rejection)
			}
			if rejection.FrameIndex == nil || *rejection.FrameIndex != 1 {
				t.Fatalf("expected frame index 1, got %+v", rejection.FrameIndex)
			}
		})
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
