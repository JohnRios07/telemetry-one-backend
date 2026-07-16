package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"telemetry-one-backend/internal/telemetry"
)

func TestParseAndConvertGT7TracksCSVToValidIngestRequest(t *testing.T) {
	csv := "track_id,x,z,y,speed,rpm,orientation,rotation_x,rotation_z,rotation_y\n1240,0,0,0,10,3000,0,0,0,1\n1240,10,0,0,20,4000,0,0,0,1\n"
	samples, err := parseGT7TracksCSV(strings.NewReader(csv), "1240")
	if err != nil {
		t.Fatalf("parseGT7TracksCSV returned error: %v", err)
	}

	request := convertSamples(samples, options{sessionID: "gt7-fixture-session", frequencyHz: 10, startUnixMs: defaultStartUnixMs})

	if request.SessionID != "gt7-fixture-session" {
		t.Fatalf("expected stable session id, got %q", request.SessionID)
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("converted request should validate: %v", err)
	}
	if len(request.Frames) != 2 || request.Frames[1].TimestampUnixMs != defaultStartUnixMs+100 {
		t.Fatalf("expected deterministic 10Hz timestamps, got %+v", request.Frames)
	}
	if request.Frames[1].PositionX != 10 || request.Frames[1].PositionY != 0 || request.Frames[1].PositionZ != 0 {
		t.Fatalf("expected raw x/z/y to map to positionX/Y/Z, got %+v", request.Frames[1])
	}
}

func TestParseGT7TracksCSVRejectsMalformedInput(t *testing.T) {
	_, err := parseGT7TracksCSV(strings.NewReader("track_id,x,z\n1240,1,2\n"), "")
	if !errors.Is(err, errMissingRequiredColumn) {
		t.Fatalf("expected missing required column error, got %v", err)
	}

	_, err = parseGT7TracksCSV(strings.NewReader("track_id,x,z,y\n1240,1,nope,3\n"), "")
	if err == nil || !strings.Contains(err.Error(), "invalid finite z") {
		t.Fatalf("expected clear malformed field error, got %v", err)
	}
}

func TestGeneratedJSONHasOnlyIngestBatchShape(t *testing.T) {
	var stdout bytes.Buffer
	err := run([]string{
		"-input", "testdata/minimal.csv",
		"-session", "shape-session",
		"-hz", "20",
	}, &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("output should be JSON: %v", err)
	}
	if len(raw) != 2 || raw["sessionId"] == nil || raw["frames"] == nil {
		t.Fatalf("expected only ingest batch fields, got keys %+v", raw)
	}
	for forbidden := range map[string]struct{}{"trackId": {}, "layoutId": {}, "corners": {}, "sectors": {}, "centerLine": {}} {
		if _, ok := raw[forbidden]; ok {
			t.Fatalf("forbidden metadata field %q found in output", forbidden)
		}
	}

	var request telemetry.IngestBatchRequest
	if err := json.Unmarshal(stdout.Bytes(), &request); err != nil {
		t.Fatalf("output should unmarshal as request: %v", err)
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("output should validate: %v", err)
	}
}
