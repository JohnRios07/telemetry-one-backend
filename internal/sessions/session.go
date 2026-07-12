package sessions

import "time"

type Session struct {
	ID          string    `json:"id"`
	Source      string    `json:"source,omitempty"`
	Game        string    `json:"game,omitempty"`
	Platform    string    `json:"platform,omitempty"`
	DriverAlias string    `json:"driverAlias,omitempty"`
	StartedAt   time.Time `json:"startedAt"`
	EndedAt     time.Time `json:"endedAt,omitempty"`
	TrackID     string    `json:"trackId,omitempty"`
}

type CreateRequest struct {
	Source        string `json:"source"`
	Game          string `json:"game"`
	Platform      string `json:"platform"`
	DriverAlias   string `json:"driverAlias,omitempty"`
	TrackID       string `json:"trackId,omitempty"`
	StartedUnixMs int64  `json:"startedUnixMs"`
}

type CreateResponse struct {
	Session Session `json:"session"`
}

func (r CreateRequest) Validate() error {
	if r.Source == "" {
		return ErrMissingSource
	}
	if r.Game == "" {
		return ErrMissingGame
	}
	if r.Platform == "" {
		return ErrMissingPlatform
	}
	if r.StartedUnixMs <= 0 {
		return ErrInvalidStartedAt
	}

	return nil
}
