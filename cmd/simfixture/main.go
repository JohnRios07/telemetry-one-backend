package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"telemetry-one-backend/internal/telemetry/sim"
)

func main() {
	sessionID := flag.String("session", sim.DefaultSessionID, "synthetic session id")
	frequencyHz := flag.Int("hz", 20, "frame frequency: 20, 30, or 60")
	durationSeconds := flag.Int("duration", int(sim.DefaultLapDuration/time.Second), "lap duration in seconds")
	flag.Parse()

	batch, err := sim.GenerateBatch(sim.Options{
		SessionID:   *sessionID,
		FrequencyHz: *frequencyHz,
		LapDuration: time.Duration(*durationSeconds) * time.Second,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate synthetic fixture: %v\n", err)
		os.Exit(1)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(batch); err != nil {
		fmt.Fprintf(os.Stderr, "encode synthetic fixture: %v\n", err)
		os.Exit(1)
	}
}
