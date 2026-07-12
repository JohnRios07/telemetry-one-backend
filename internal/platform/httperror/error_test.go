package httperror

import "testing"

func TestErrorHelpersUseStableCodes(t *testing.T) {
	tests := []struct {
		name string
		err  Error
		code string
	}{
		{name: "bad request", err: BadRequest("invalid payload"), code: "bad_request"},
		{name: "not found", err: NotFound("missing resource"), code: "not_found"},
		{name: "not implemented", err: NotImplemented("planned endpoint"), code: "not_implemented"},
		{name: "internal", err: Internal("unexpected failure"), code: "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.code {
				t.Fatalf("expected code %q, got %q", tt.code, tt.err.Code)
			}
			if tt.err.Message == "" {
				t.Fatal("expected error message to be preserved")
			}
		})
	}
}

func TestEnvelopeWrapsError(t *testing.T) {
	err := BadRequest("invalid payload")
	response := Envelope(err)

	if response.Error != err {
		t.Fatalf("expected envelope to contain original error")
	}
}

func TestBadRequestWithDetailsPreservesDiagnostics(t *testing.T) {
	details := map[string]string{"rejectionCode": "invalid_throttle"}
	err := BadRequestWithDetails("invalid payload", details)

	if err.Code != "bad_request" || err.Message != "invalid payload" {
		t.Fatalf("unexpected error: %+v", err)
	}
	if err.Details == nil {
		t.Fatal("expected details to be preserved")
	}
}
