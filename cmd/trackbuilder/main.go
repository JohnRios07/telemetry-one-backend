package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"telemetry-one-backend/internal/telemetry"
	"telemetry-one-backend/internal/trackbuilder"
	"telemetry-one-backend/internal/tracks"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "trackbuilder: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("trackbuilder", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: trackbuilder -input ingest.json -layout-id gt7_layout_1240 [-output catalog.json] [-smoothing-window 3] [-epsilon 0.5]\n")
		fmt.Fprintf(flags.Output(), "   or: trackbuilder -baseline -input lap1.json -input lap2.json -layout-id gt7_layout_1240 [-output baseline.json]\n\n")
		fmt.Fprintln(flags.Output(), "Turns ordered telemetry.IngestBatchRequest JSON into a curated, single-layout track catalog for review only.")
		fmt.Fprintln(flags.Output(), "The generated catalog is not runtime truth and requires manual promotion before use.")
		fmt.Fprintln(flags.Output(), "Baseline mode emits an observed telemetry baseline only; it does not correct or approve geometry.")
		flags.PrintDefaults()
	}

	opt := trackbuilder.DefaultOptions()
	var inputPaths multiStringFlag
	flags.Var(&inputPaths, "input", "ordered telemetry.IngestBatchRequest JSON input path; repeat for baseline mode")
	baselineMode := flags.Bool("baseline", false, "emit an observed telemetry baseline report instead of a curated catalog")
	flags.StringVar(&opt.LayoutID, "layout-id", "", "seed catalog layout id to curate")
	flags.StringVar(&opt.OutputPath, "output", "", "catalog JSON output path; stdout when empty")
	flags.IntVar(&opt.SmoothingWindow, "smoothing-window", trackbuilder.DefaultSmoothingWindow, "moving average window size; must be odd")
	flags.Float64Var(&opt.EpsilonMeters, "epsilon", trackbuilder.DefaultEpsilonMeters, "RDP simplification epsilon in meters")
	if err := flags.Parse(args); err != nil {
		return err
	}

	seed := tracks.OfficialGT7SeedCatalog()
	if *baselineMode {
		if len(inputPaths) == 0 {
			return trackbuilder.ErrMissingInputPath
		}
		requests, err := readIngestBatches(inputPaths)
		if err != nil {
			return err
		}
		report, err := trackbuilder.BuildTelemetryBaseline(requests, opt.LayoutID, seed)
		if err != nil {
			return err
		}
		if err := writeTelemetryBaseline(stdout, opt.OutputPath, report); err != nil {
			return err
		}
		_, _ = fmt.Fprint(stderr, report.String())
		return nil
	}

	if len(inputPaths) != 1 {
		if len(inputPaths) == 0 {
			return trackbuilder.ErrMissingInputPath
		}
		return fmt.Errorf("build mode accepts exactly one -input")
	}
	request, err := readIngestBatch(inputPaths[0])
	if err != nil {
		return err
	}
	if err := opt.Validate(); err != nil {
		return err
	}

	catalog, report, err := trackbuilder.Build(request, opt, seed)
	if err != nil {
		return err
	}

	if err := writeCatalog(stdout, opt.OutputPath, catalog); err != nil {
		return err
	}

	_, _ = fmt.Fprint(stderr, report.String())
	return nil
}

type multiStringFlag []string

func (m *multiStringFlag) String() string {
	return strings.Join(*m, ",")
}

func (m *multiStringFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func readIngestBatches(paths []string) ([]telemetry.IngestBatchRequest, error) {
	requests := make([]telemetry.IngestBatchRequest, 0, len(paths))
	for _, path := range paths {
		request, err := readIngestBatch(path)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}

	return requests, nil
}

func readIngestBatch(path string) (telemetry.IngestBatchRequest, error) {
	if strings.TrimSpace(path) == "" {
		return telemetry.IngestBatchRequest{}, trackbuilder.ErrMissingInputPath
	}

	file, err := os.Open(path)
	if err != nil {
		return telemetry.IngestBatchRequest{}, fmt.Errorf("open input %s: %w", path, err)
	}
	defer file.Close()

	var request telemetry.IngestBatchRequest
	if err := json.NewDecoder(file).Decode(&request); err != nil {
		return telemetry.IngestBatchRequest{}, fmt.Errorf("decode ingest batch %s: %w", path, err)
	}

	return request, nil
}

func writeTelemetryBaseline(stdout io.Writer, outputPath string, report trackbuilder.TelemetryBaselineReport) error {
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode telemetry baseline: %w", err)
	}
	if strings.TrimSpace(outputPath) == "" {
		if _, err := stdout.Write(encoded); err != nil {
			return fmt.Errorf("write telemetry baseline: %w", err)
		}
		if _, err := stdout.Write([]byte("\n")); err != nil {
			return fmt.Errorf("write telemetry baseline: %w", err)
		}

		return nil
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(encoded); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	if _, err := file.Write([]byte("\n")); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	return nil
}

func writeCatalog(stdout io.Writer, outputPath string, catalog tracks.Catalog) error {
	encoded, err := trackbuilder.EncodeCatalog(catalog)
	if err != nil {
		return fmt.Errorf("encode catalog: %w", err)
	}
	if strings.TrimSpace(outputPath) == "" {
		if _, err := stdout.Write(encoded); err != nil {
			return fmt.Errorf("write catalog: %w", err)
		}

		return nil
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(encoded); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	return nil
}
