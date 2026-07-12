package logging

import (
	"log/slog"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  slog.Level
	}{
		{name: "debug", input: "debug", want: slog.LevelDebug},
		{name: "info default", input: "info", want: slog.LevelInfo},
		{name: "warn", input: "warn", want: slog.LevelWarn},
		{name: "warning alias", input: "warning", want: slog.LevelWarn},
		{name: "error", input: "error", want: slog.LevelError},
		{name: "unknown defaults to info", input: "verbose", want: slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseLevel(tt.input); got != tt.want {
				t.Fatalf("expected level %s, got %s", tt.want, got)
			}
		})
	}
}
