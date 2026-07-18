package sessionexport

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"telemetry-one-backend/internal/telemetry"
)

var (
	createTempFile = os.CreateTemp
	renameFile     = os.Rename
	removeFile     = os.Remove
)

func WriteRequestFile(outputPath string, request telemetry.IngestBatchRequest) (err error) {
	if outputPath == "" {
		return fmt.Errorf("output path is required")
	}

	dir := filepath.Dir(outputPath)
	temp, err := createTempFile(dir, ".export-session-frames-*.json")
	if err != nil {
		return fmt.Errorf("create temp output: %w", err)
	}
	tempPath := temp.Name()
	defer func() {
		_ = removeFile(tempPath)
	}()

	if err := writeRequest(temp, request); err != nil {
		return err
	}
	if err := renameFile(tempPath, outputPath); err != nil {
		return fmt.Errorf("move output into place: %w", err)
	}

	return nil
}

func writeRequest(w io.WriteCloser, request telemetry.IngestBatchRequest) (err error) {
	defer func() {
		if closeErr := w.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close output: %w", closeErr)
		}
	}()

	encoded, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal export request: %w", err)
	}
	if _, err := w.Write(encoded); err != nil {
		return fmt.Errorf("write export request: %w", err)
	}

	return nil
}
