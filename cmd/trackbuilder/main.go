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
		fmt.Fprintf(flags.Output(), "Usage: trackbuilder -input ingest.json -layout-id gt7_layout_1240 [-output catalog.json] [-smoothing-window 3] [-epsilon 0.5]\n\n")
		fmt.Fprintln(flags.Output(), "Turns ordered telemetry.IngestBatchRequest JSON into a curated, single-layout track catalog for review only.")
		fmt.Fprintln(flags.Output(), "The generated catalog is not runtime truth and requires manual promotion before use.")
		flags.PrintDefaults()
	}

	opt := trackbuilder.DefaultOptions()
	flags.StringVar(&opt.InputPath, "input", "", "ordered telemetry.IngestBatchRequest JSON input path")
	flags.StringVar(&opt.LayoutID, "layout-id", "", "seed catalog layout id to curate")
	flags.StringVar(&opt.OutputPath, "output", "", "catalog JSON output path; stdout when empty")
	flags.IntVar(&opt.SmoothingWindow, "smoothing-window", trackbuilder.DefaultSmoothingWindow, "moving average window size; must be odd")
	flags.Float64Var(&opt.EpsilonMeters, "epsilon", trackbuilder.DefaultEpsilonMeters, "RDP simplification epsilon in meters")
	if err := flags.Parse(args); err != nil {
		return err
	}

	request, err := readIngestBatch(opt.InputPath)
	if err != nil {
		return err
	}

	seed := tracks.OfficialGT7SeedCatalog()
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

func readIngestBatch(path string) (telemetry.IngestBatchRequest, error) {
	if strings.TrimSpace(path) == "" {
		return telemetry.IngestBatchRequest{}, trackbuilder.ErrMissingInputPath
	}

	file, err := os.Open(path)
	if err != nil {
		return telemetry.IngestBatchRequest{}, fmt.Errorf("open input: %w", err)
	}
	defer file.Close()

	var request telemetry.IngestBatchRequest
	if err := json.NewDecoder(file).Decode(&request); err != nil {
		return telemetry.IngestBatchRequest{}, fmt.Errorf("decode ingest batch: %w", err)
	}

	return request, nil
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
