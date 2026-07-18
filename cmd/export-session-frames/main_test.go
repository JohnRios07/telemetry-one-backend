package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"telemetry-one-backend/internal/sessionexport"
	"telemetry-one-backend/internal/sessions"
	"telemetry-one-backend/internal/telemetry"
)

func TestRunWritesOnlyExportJSON(t *testing.T) {
	output := filepath.Join(t.TempDir(), "export.json")
	origOpen := openDatabasePool
	origExport := exportSession
	origWrite := writeExport
	defer func() {
		openDatabasePool = origOpen
		exportSession = origExport
		writeExport = origWrite
	}()

	openDatabasePool = func(_ context.Context, _ string) (*pgxpool.Pool, error) { return nil, nil }
	exportSession = func(_ context.Context, _ *pgxpool.Pool, _ string, _ bool, lapNumber *int) (sessionexport.Result, error) {
		if lapNumber != nil {
			t.Fatalf("expected no lap filter, got %d", *lapNumber)
		}
		return sessionexport.Result{Request: telemetry.IngestBatchRequest{SessionID: "session-1", Frames: []telemetry.Frame{{TimestampUnixMs: 1, SpeedMps: 1, RPM: 1, Gear: 1, Throttle: 0, Brake: 0, Steering: 0, FuelLiters: 1, PositionX: 0, PositionY: 0, PositionZ: 0, LapNumber: 1, CurrentLapMs: 0, IsOnTrack: true}}}, Session: sessions.Session{ID: "session-1", StartedAt: time.UnixMilli(1).UTC(), EndedAt: ptrTime(time.UnixMilli(2).UTC())}}, nil
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"-session-id", "session-1", "-output", output, "-database-url", "postgres://example"}, &stdout, &stderr); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no stdout, got %q", stdout.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var decoded telemetry.IngestBatchRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if decoded.SessionID != "session-1" || len(decoded.Frames) != 1 {
		t.Fatalf("unexpected export payload: %#v", decoded)
	}
}

func TestRunPassesLapNumberFlag(t *testing.T) {
	origOpen := openDatabasePool
	origExport := exportSession
	origWrite := writeExport
	defer func() {
		openDatabasePool = origOpen
		exportSession = origExport
		writeExport = origWrite
	}()

	openDatabasePool = func(_ context.Context, _ string) (*pgxpool.Pool, error) { return nil, nil }
	exportSession = func(_ context.Context, _ *pgxpool.Pool, _ string, _ bool, lapNumber *int) (sessionexport.Result, error) {
		if lapNumber == nil || *lapNumber != 3 {
			t.Fatalf("expected lap number 3, got %#v", lapNumber)
		}
		return sessionexport.Result{Request: telemetry.IngestBatchRequest{SessionID: "session-1", Frames: []telemetry.Frame{{TimestampUnixMs: 1, SpeedMps: 1, RPM: 1, Gear: 1, Throttle: 0, Brake: 0, Steering: 0, FuelLiters: 1, PositionX: 0, PositionY: 0, PositionZ: 0, LapNumber: 3, CurrentLapMs: 0, IsOnTrack: true}}}, Session: sessions.Session{ID: "session-1", StartedAt: time.UnixMilli(1).UTC(), EndedAt: ptrTime(time.UnixMilli(2).UTC())}}, nil
	}
	writeExport = func(_ string, _ telemetry.IngestBatchRequest) error { return nil }

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"-session-id", "session-1", "-output", filepath.Join(t.TempDir(), "ignored.json"), "-database-url", "postgres://example", "-lap-number", "3"}, &stdout, &stderr); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
}

func TestRunWarnsForActiveSessionWithOptIn(t *testing.T) {
	origOpen := openDatabasePool
	origExport := exportSession
	origWrite := writeExport
	defer func() {
		openDatabasePool = origOpen
		exportSession = origExport
		writeExport = origWrite
	}()

	openDatabasePool = func(_ context.Context, _ string) (*pgxpool.Pool, error) { return nil, nil }
	exportSession = func(_ context.Context, _ *pgxpool.Pool, _ string, _ bool, _ *int) (sessionexport.Result, error) {
		return sessionexport.Result{Request: telemetry.IngestBatchRequest{SessionID: "session-1", Frames: []telemetry.Frame{{TimestampUnixMs: 1, SpeedMps: 1, RPM: 1, Gear: 1, Throttle: 0, Brake: 0, Steering: 0, FuelLiters: 1, PositionX: 0, PositionY: 0, PositionZ: 0, LapNumber: 1, CurrentLapMs: 0, IsOnTrack: true}}}, Session: sessions.Session{ID: "session-1", StartedAt: time.UnixMilli(1).UTC()}}, nil
	}
	writeExport = func(_ string, _ telemetry.IngestBatchRequest) error { return nil }

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"-session-id", "session-1", "-output", filepath.Join(t.TempDir(), "ignored.json"), "-database-url", "postgres://example", "-allow-active-session"}, &stdout, &stderr); err != nil || !strings.Contains(stderr.String(), "warning: exporting active session snapshot") {
		t.Fatalf("expected warning for active session, got err=%v stderr=%q", err, stderr.String())
	}
}

func TestRunRejectsNegativeLapNumber(t *testing.T) {
	origOpen := openDatabasePool
	origExport := exportSession
	origWrite := writeExport
	defer func() {
		openDatabasePool = origOpen
		exportSession = origExport
		writeExport = origWrite
	}()

	openDatabasePool = func(_ context.Context, _ string) (*pgxpool.Pool, error) { return nil, nil }
	exportSession = func(_ context.Context, _ *pgxpool.Pool, _ string, _ bool, _ *int) (sessionexport.Result, error) {
		t.Fatal("exportSession should not run for invalid lap number")
		return sessionexport.Result{}, nil
	}
	writeExport = func(_ string, _ telemetry.IngestBatchRequest) error { return nil }

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run([]string{"-session-id", "session-1", "-output", filepath.Join(t.TempDir(), "ignored.json"), "-database-url", "postgres://example", "-lap-number", "-1"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "lap-number must be zero or greater") {
		t.Fatalf("expected invalid lap-number error, got %v", err)
	}
}

func ptrTime(v time.Time) *time.Time { return &v }
