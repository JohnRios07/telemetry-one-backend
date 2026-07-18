package sessionexport

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"telemetry-one-backend/internal/telemetry"
)

type failingWriteCloser struct {
	bytes.Buffer
	closeErr error
}

func (w *failingWriteCloser) Close() error {
	return w.closeErr
}

func TestWriteRequestReturnsCloseError(t *testing.T) {
	errClose := errors.New("close failed")
	writer := &failingWriteCloser{closeErr: errClose}

	err := writeRequest(writer, telemetry.IngestBatchRequest{SessionID: "session-1"})

	if !errors.Is(err, errClose) {
		t.Fatalf("expected close error, got %v", err)
	}
	if writer.Len() == 0 {
		t.Fatal("expected request bytes to be written before close failure")
	}
}

func TestWriteRequestFileWritesJSONAtomically(t *testing.T) {
	output := filepath.Join(t.TempDir(), "export.json")
	request := telemetry.IngestBatchRequest{SessionID: "session-1", Frames: []telemetry.Frame{{TimestampUnixMs: 1, SpeedMps: 1, RPM: 1, Gear: 1, Throttle: 0, Brake: 0, Steering: 0, FuelLiters: 1, PositionX: 0, PositionY: 0, PositionZ: 0, LapNumber: 1, CurrentLapMs: 0, IsOnTrack: true}}}

	if err := WriteRequestFile(output, request); err != nil {
		t.Fatalf("write request file: %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	if string(data) != `{"sessionId":"session-1","frames":[{"timestampUnixMs":1,"speedMps":1,"rpm":1,"gear":1,"throttle":0,"brake":0,"steering":0,"fuelLiters":1,"positionX":0,"positionY":0,"positionZ":0,"lapNumber":1,"currentLapMs":0,"isOnTrack":true}]}` {
		t.Fatalf("unexpected JSON output: %s", string(data))
	}
}
