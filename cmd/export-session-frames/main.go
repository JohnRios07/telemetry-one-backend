package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"telemetry-one-backend/internal/sessionexport"
	"telemetry-one-backend/internal/sessions"
	"telemetry-one-backend/internal/telemetry"
)

const (
	exitUsageOrConfig = 2
	exitMissingData   = 3
	exitActiveBlocked = 4
	exitTimestamp     = 5
	exitIO            = 6
)

var (
	openDatabasePool = openReadOnlyPool
	exportSession    = exportSessionFrames
	writeExport      = sessionexport.WriteRequestFile
)

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		code := classifyExitCode(err)
		if code != 0 {
			fmt.Fprintf(os.Stderr, "export-session-frames: %v\n", err)
			os.Exit(code)
		}
	}
}

func run(args []string, stdout io.Writer, stderr io.Writer) error {
	flags := flag.NewFlagSet("export-session-frames", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: export-session-frames -session-id <id> -output <path> [-database-url <url>] [-allow-active-session] [-lap-number <n>]\n\n")
		fmt.Fprintln(flags.Output(), "Exports persisted session frames as telemetry.IngestBatchRequest JSON for cmd/trackbuilder.")
		fmt.Fprintln(flags.Output(), "The command is read-only and fails closed for active sessions unless explicitly opted in.")
		flags.PrintDefaults()
	}

	var sessionID string
	var outputPath string
	var databaseURL string
	var allowActive bool
	var lapNumber int
	flags.StringVar(&sessionID, "session-id", "", "session id to export")
	flags.StringVar(&outputPath, "output", "", "output JSON path")
	flags.StringVar(&databaseURL, "database-url", "", "postgres connection string; defaults to TELEMETRY_ONE_DATABASE_URL")
	flags.BoolVar(&allowActive, "allow-active-session", false, "allow exporting an active session snapshot")
	flags.IntVar(&lapNumber, "lap-number", -1, "optional lap number to export; omit to export all frames")
	flags.IntVar(&lapNumber, "lapNumber", -1, "alias for -lap-number")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return newExitError(exitUsageOrConfig, err)
	}

	lapNumberProvided := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "lap-number" || f.Name == "lapNumber" {
			lapNumberProvided = true
		}
	})
	if lapNumberProvided && lapNumber < 0 {
		return newExitError(exitUsageOrConfig, fmt.Errorf("lap-number must be zero or greater"))
	}

	var lapFilter *int
	if lapNumberProvided {
		lapFilter = &lapNumber
	}

	if strings.TrimSpace(sessionID) == "" {
		return newExitError(exitUsageOrConfig, fmt.Errorf("session-id is required"))
	}
	if strings.TrimSpace(outputPath) == "" {
		return newExitError(exitUsageOrConfig, fmt.Errorf("output is required"))
	}
	databaseURL = resolveDatabaseURL(databaseURL)
	if strings.TrimSpace(databaseURL) == "" {
		return newExitError(exitUsageOrConfig, fmt.Errorf("database-url is required or TELEMETRY_ONE_DATABASE_URL must be set"))
	}

	ctx := context.Background()
	pool, err := openDatabasePool(ctx, databaseURL)
	if err != nil {
		return newExitError(exitIO, err)
	}
	if pool != nil {
		defer pool.Close()
	}

	result, err := exportSession(ctx, pool, sessionID, allowActive, lapFilter)
	if err != nil {
		return classifyExportError(err)
	}
	if result.Session.Status() == sessions.StatusActive && allowActive {
		_, _ = fmt.Fprintln(stderr, "warning: exporting active session snapshot; output may be incomplete")
	}

	if err := writeExport(outputPath, result.Request); err != nil {
		return newExitError(exitIO, err)
	}
	return nil
}

func resolveDatabaseURL(flagValue string) string {
	if strings.TrimSpace(flagValue) != "" {
		return flagValue
	}
	return os.Getenv("TELEMETRY_ONE_DATABASE_URL")
}

func openReadOnlyPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = map[string]string{}
	}
	config.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	config.ConnConfig.RuntimeParams["application_name"] = "export-session-frames"

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}

func exportSessionFrames(ctx context.Context, pool *pgxpool.Pool, sessionID string, allowActive bool, lapNumber *int) (sessionexport.Result, error) {
	exporter := sessionexport.Exporter{
		SessionReader:      sessions.NewPostgresRepository(pool),
		FrameReader:        telemetry.NewPostgresStore(pool),
		AllowActiveSession: allowActive,
	}
	return exporter.Export(ctx, sessionID, lapNumber)
}

func classifyExitCode(err error) int {
	var exitErr *exitError
	if errors.As(err, &exitErr) {
		return exitErr.code
	}
	return 1
}

func newExitError(code int, err error) error {
	return &exitError{code: code, err: err}
}

func classifyExportError(err error) error {
	switch {
	case errors.Is(err, sessionexport.ErrMissingSession), errors.Is(err, sessions.ErrNotFound):
		return newExitError(exitMissingData, err)
	case errors.Is(err, sessionexport.ErrActiveSessionBlocked):
		return newExitError(exitActiveBlocked, err)
	case errors.Is(err, telemetry.ErrInvalidLapNumber):
		return newExitError(exitUsageOrConfig, err)
	case errors.Is(err, sessionexport.ErrEmptyFrames):
		return newExitError(exitMissingData, err)
	case errors.Is(err, sessionexport.ErrNonMonotonicTimestamps):
		return newExitError(exitTimestamp, err)
	default:
		return newExitError(exitIO, err)
	}
}
