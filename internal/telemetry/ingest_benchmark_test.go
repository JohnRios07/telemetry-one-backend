package telemetry

import (
	"context"
	"strconv"
	"testing"
)

func BenchmarkFrameNormalize(b *testing.B) {
	frame := benchmarkFrame(0)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := frame.Normalize(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIngestBatchNormalize(b *testing.B) {
	for _, size := range []int{20, 60, 300, MaxBatchFrames} {
		b.Run(strconv.Itoa(size)+"_frames", func(b *testing.B) {
			request := IngestBatchRequest{SessionID: "session-bench", Frames: benchmarkFrames(size)}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := request.Normalize()
				if err != nil {
					b.Fatal(err)
				}
				if len(result.Frames) != size {
					b.Fatalf("expected %d frames, got %d", size, len(result.Frames))
				}
			}
		})
	}
}

func BenchmarkFrameStoreAppend(b *testing.B) {
	for _, size := range []int{20, 60, 300, MaxBatchFrames} {
		b.Run(strconv.Itoa(size)+"_frames", func(b *testing.B) {
			store := NewFrameStore(12000)
			frames := benchmarkFrames(size)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := store.Append(context.Background(), "session-bench", frames); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkFrameStoreAppendAtRetentionLimit(b *testing.B) {
	store := NewFrameStore(12000)
	seed := benchmarkFrames(12000)
	if err := store.Append(context.Background(), "session-bench", seed); err != nil {
		b.Fatal(err)
	}
	frames := benchmarkFrames(MaxBatchFrames)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := store.Append(context.Background(), "session-bench", frames); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkFrames(count int) []Frame {
	frames := make([]Frame, count)
	for i := 0; i < count; i++ {
		frames[i] = benchmarkFrame(i)
	}
	return frames
}

func benchmarkFrame(index int) Frame {
	yaw := 1.57
	yawRate := 0.03
	wheelSpeedFL := 58.1
	wheelSpeedFR := 58.2
	wheelSpeedRL := 58.4
	wheelSpeedRR := 58.3
	lastLapMs := int64(91345)
	bestLapMs := int64(90210)

	return Frame{
		TimestampUnixMs: 1720656000000 + int64(index),
		SpeedMps:        58.33,
		RPM:             7100,
		Gear:            4,
		Throttle:        0.7,
		Brake:           0.2,
		Steering:        -0.12,
		FuelLiters:      38.4,
		PositionX:       123.4 + float64(index),
		PositionY:       5.6,
		PositionZ:       789.1,
		YawRadians:      &yaw,
		YawRate:         &yawRate,
		WheelSpeedFL:    &wheelSpeedFL,
		WheelSpeedFR:    &wheelSpeedFR,
		WheelSpeedRL:    &wheelSpeedRL,
		WheelSpeedRR:    &wheelSpeedRR,
		LapNumber:       1,
		CurrentLapMs:    81234 + int64(index),
		LastLapMs:       &lastLapMs,
		BestLapMs:       &bestLapMs,
		IsOnTrack:       true,
	}
}
