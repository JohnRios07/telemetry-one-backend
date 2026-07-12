package sessions

import "testing"

func TestCreateRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		request CreateRequest
		wantErr error
	}{
		{
			name: "valid",
			request: CreateRequest{
				Source:        "flutter",
				Game:          "gt7",
				Platform:      "ps5",
				StartedUnixMs: 1720656000000,
			},
		},
		{name: "missing source", request: CreateRequest{Game: "gt7", Platform: "ps5", StartedUnixMs: 1}, wantErr: ErrMissingSource},
		{name: "missing game", request: CreateRequest{Source: "flutter", Platform: "ps5", StartedUnixMs: 1}, wantErr: ErrMissingGame},
		{name: "missing platform", request: CreateRequest{Source: "flutter", Game: "gt7", StartedUnixMs: 1}, wantErr: ErrMissingPlatform},
		{name: "invalid start", request: CreateRequest{Source: "flutter", Game: "gt7", Platform: "ps5"}, wantErr: ErrInvalidStartedAt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.request.Validate()
			if err != tt.wantErr {
				t.Fatalf("expected error %v, got %v", tt.wantErr, err)
			}
		})
	}
}
