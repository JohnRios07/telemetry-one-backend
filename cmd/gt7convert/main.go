package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"sort"
	"os"
	"strconv"
	"strings"

	"telemetry-one-backend/internal/telemetry"
)

const defaultStartUnixMs int64 = 1720656000000

var errMissingRequiredColumn = errors.New("missing required GT7Tracks CSV column")

type options struct {
	input       string
	output      string
	sessionID   string
	frequencyHz float64
	trackID     string
	layoutID    string
	startUnixMs int64
}

type rawSample struct {
	TrackID string
	X       float64
	Z       float64
	Y       float64
	Speed   *float64
	RPM     *float64
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "gt7convert: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("gt7convert", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: gt7convert -input raw.csv -output fixture.json -session SESSION -hz 60 [-track-id ID] [-layout-id ID]\n\n")
		fmt.Fprintln(flags.Output(), "Converts GT7Tracks raw CSV dumps into telemetry ingest JSON fixtures for dev/test only.")
		fmt.Fprintln(flags.Output(), "It does not create production geometry, centerlines, sectors, apexes, or corner metadata.")
		fmt.Fprintln(flags.Output(), "Expected GT7Tracks CSV schema: track_id,x,z,y,speed,rpm,orientation,rotation_x,rotation_z,rotation_y")
		flags.PrintDefaults()
	}

	opt := options{startUnixMs: defaultStartUnixMs}
	flags.StringVar(&opt.input, "input", "", "GT7Tracks raw CSV input path")
	flags.StringVar(&opt.output, "output", "", "telemetry ingest fixture JSON output path; stdout when empty")
	flags.StringVar(&opt.sessionID, "session", "", "fixture session id")
	flags.Float64Var(&opt.frequencyHz, "hz", 60, "synthetic fixture frame frequency used for timestamps")
	flags.StringVar(&opt.trackID, "track-id", "", "optional GT7Tracks track_id expected in every row")
	flags.StringVar(&opt.layoutID, "layout-id", "", "optional catalog layout id retained only in CLI provenance/docs, never emitted in JSON")
	flags.Int64Var(&opt.startUnixMs, "start-unix-ms", defaultStartUnixMs, "fixture start timestamp in Unix milliseconds")
	if err := flags.Parse(args); err != nil {
		return err
	}
	_ = opt.layoutID // fixture-only provenance hint; output contract forbids metadata envelope.

	request, err := convertFile(opt)
	if err != nil {
		return err
	}

	writer := stdout
	var output *os.File
	if opt.output != "" {
		output, err = os.Create(opt.output)
		if err != nil {
			return fmt.Errorf("create output: %w", err)
		}
		defer output.Close()
		writer = output
	}

	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(request); err != nil {
		return fmt.Errorf("encode fixture: %w", err)
	}

	return nil
}

func convertFile(opt options) (telemetry.IngestBatchRequest, error) {
	if strings.TrimSpace(opt.input) == "" {
		return telemetry.IngestBatchRequest{}, errors.New("-input is required")
	}
	if strings.TrimSpace(opt.sessionID) == "" {
		return telemetry.IngestBatchRequest{}, errors.New("-session is required")
	}
	if !isFinitePositive(opt.frequencyHz) {
		return telemetry.IngestBatchRequest{}, errors.New("-hz must be finite and greater than zero")
	}
	if opt.startUnixMs <= 0 {
		return telemetry.IngestBatchRequest{}, errors.New("-start-unix-ms must be greater than zero")
	}

	file, err := os.Open(opt.input)
	if err != nil {
		return telemetry.IngestBatchRequest{}, fmt.Errorf("open input: %w", err)
	}
	defer file.Close()

	samples, err := parseGT7TracksCSV(file, opt.trackID)
	if err != nil {
		return telemetry.IngestBatchRequest{}, err
	}
	return convertSamples(samples, opt), nil
}

func parseGT7TracksCSV(reader io.Reader, expectedTrackID string) ([]rawSample, error) {
	expectedTrackID = strings.TrimSpace(expectedTrackID)
	csvReader := csv.NewReader(reader)
	csvReader.TrimLeadingSpace = true
	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read GT7Tracks CSV: %w", err)
	}
	if len(records) < 2 {
		return nil, errors.New("GT7Tracks CSV must include a header and at least one data row")
	}

	columns := indexColumns(records[0])
	for _, column := range []string{"track_id", "x", "z", "y"} {
		if _, ok := columns[column]; !ok {
			return nil, fmt.Errorf("%w: %s", errMissingRequiredColumn, column)
		}
	}

	samples := make([]rawSample, 0, len(records)-1)
	trackIDs := make(map[string]struct{})
	for rowIndex, record := range records[1:] {
		sample, err := parseSample(rowIndex+2, record, columns)
		if err != nil {
			return nil, err
		}
		if expectedTrackID != "" && sample.TrackID != expectedTrackID {
			return nil, fmt.Errorf("row %d: track_id %q does not match expected %q", rowIndex+2, sample.TrackID, expectedTrackID)
		}
		trackIDs[sample.TrackID] = struct{}{}
		samples = append(samples, sample)
	}
	if expectedTrackID == "" {
		if len(trackIDs) != 1 {
			values := make([]string, 0, len(trackIDs))
			for trackID := range trackIDs {
				values = append(values, trackID)
			}
			sort.Strings(values)
			return nil, fmt.Errorf("GT7Tracks CSV must contain exactly one distinct track_id when -track-id is omitted; found %v", values)
		}
	}

	return samples, nil
}

func indexColumns(header []string) map[string]int {
	columns := make(map[string]int, len(header))
	for index, column := range header {
		columns[strings.ToLower(strings.TrimSpace(column))] = index
	}
	return columns
}

func parseSample(row int, record []string, columns map[string]int) (rawSample, error) {
	read := func(column string) (string, error) {
		index, ok := columns[column]
		if !ok || index >= len(record) {
			return "", fmt.Errorf("row %d: missing %s", row, column)
		}
		value := strings.TrimSpace(record[index])
		if value == "" {
			return "", fmt.Errorf("row %d: empty %s", row, column)
		}
		return value, nil
	}
	parseRequiredFloat := func(column string) (float64, error) {
		value, err := read(column)
		if err != nil {
			return 0, err
		}
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || !isFinite(parsed) {
			return 0, fmt.Errorf("row %d: invalid finite %s", row, column)
		}
		return parsed, nil
	}
	parseOptionalNonNegativeFloat := func(column string) (*float64, error) {
		index, ok := columns[column]
		if !ok || index >= len(record) || strings.TrimSpace(record[index]) == "" {
			return nil, nil
		}
		parsed, err := strconv.ParseFloat(strings.TrimSpace(record[index]), 64)
		if err != nil || !isFinite(parsed) || parsed < 0 {
			return nil, fmt.Errorf("row %d: invalid finite non-negative %s", row, column)
		}
		return &parsed, nil
	}

	trackID, err := read("track_id")
	if err != nil {
		return rawSample{}, err
	}
	x, err := parseRequiredFloat("x")
	if err != nil {
		return rawSample{}, err
	}
	z, err := parseRequiredFloat("z")
	if err != nil {
		return rawSample{}, err
	}
	y, err := parseRequiredFloat("y")
	if err != nil {
		return rawSample{}, err
	}
	speed, err := parseOptionalNonNegativeFloat("speed")
	if err != nil {
		return rawSample{}, err
	}
	rpm, err := parseOptionalNonNegativeFloat("rpm")
	if err != nil {
		return rawSample{}, err
	}

	return rawSample{TrackID: trackID, X: x, Z: z, Y: y, Speed: speed, RPM: rpm}, nil
}

func convertSamples(samples []rawSample, opt options) telemetry.IngestBatchRequest {
	frames := make([]telemetry.Frame, 0, len(samples))
	intervalMs := int64(math.Round(1000 / opt.frequencyHz))
	if intervalMs <= 0 {
		intervalMs = 1
	}
	for index, sample := range samples {
		speed := 0.0
		if sample.Speed != nil {
			speed = *sample.Speed
		} else if index > 0 {
			speed = distance(samples[index-1], sample) / (float64(intervalMs) / 1000)
		}
		rpm := 1000.0
		if sample.RPM != nil {
			rpm = *sample.RPM
		}
		frames = append(frames, telemetry.Frame{
			TimestampUnixMs: opt.startUnixMs + int64(index)*intervalMs,
			SpeedMps:        round(speed, 3),
			RPM:             round(rpm, 1),
			Gear:            gearForSpeed(speed),
			Throttle:        0,
			Brake:           0,
			Steering:        0,
			FuelLiters:      0,
			PositionX:       round(sample.X, 3),
			PositionY:       round(sample.Y, 3),
			PositionZ:       round(sample.Z, 3),
			LapNumber:       1,
			CurrentLapMs:    int64(index) * intervalMs,
			IsOnTrack:       true,
		})
	}

	return telemetry.IngestBatchRequest{SessionID: opt.sessionID, Frames: frames}
}

func distance(a rawSample, b rawSample) float64 {
	dx := b.X - a.X
	dy := b.Y - a.Y
	dz := b.Z - a.Z
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

func gearForSpeed(speed float64) int {
	switch {
	case speed < 12:
		return 1
	case speed < 22:
		return 2
	case speed < 32:
		return 3
	case speed < 44:
		return 4
	case speed < 56:
		return 5
	default:
		return 6
	}
}

func round(value float64, precision int) float64 {
	scale := math.Pow10(precision)
	return math.Round(value*scale) / scale
}

func isFinitePositive(value float64) bool {
	return isFinite(value) && value > 0
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
