package sessions

import (
	"time"

	"telemetry-one-backend/internal/tracks"
)

type Status string

const (
	StatusActive   Status = "active"
	StatusFinished Status = "finished"
)

type Session struct {
	ID               string     `json:"id"`
	Source           string     `json:"source,omitempty"`
	Game             string     `json:"game,omitempty"`
	Platform         string     `json:"platform,omitempty"`
	DriverAlias      string     `json:"driverAlias,omitempty"`
	StartedAt        time.Time  `json:"startedAt"`
	EndedAt          *time.Time `json:"endedAt,omitempty"`
	TrackID          string     `json:"trackId,omitempty"`
	LayoutID         string     `json:"layoutId,omitempty"`
	DetectedTrackID  string     `json:"detectedTrackId,omitempty"`
	DetectedLayoutID string     `json:"detectedLayoutId,omitempty"`
}

func (s Session) EffectiveLayoutID() string {
	if s.LayoutID != "" {
		return s.LayoutID
	}

	return s.DetectedLayoutID
}

func (s Session) Status() Status {
	if s.EndedAt != nil {
		return StatusFinished
	}

	return StatusActive
}

func (s Session) DurationMs() *int64 {
	if s.EndedAt == nil {
		return nil
	}
	duration := s.EndedAt.Sub(s.StartedAt).Milliseconds()
	return &duration
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

type Response struct {
	Session DTO `json:"session"`
}

type DTO struct {
	ID                string                     `json:"id"`
	Source            string                     `json:"source"`
	Game              string                     `json:"game"`
	Platform          string                     `json:"platform"`
	DriverAlias       string                     `json:"driverAlias,omitempty"`
	StartedAt         time.Time                  `json:"startedAt"`
	EndedAt           *time.Time                 `json:"endedAt"`
	TrackID           string                     `json:"trackId,omitempty"`
	LayoutID          string                     `json:"layoutId,omitempty"`
	Status            Status                     `json:"status"`
	DurationMs        *int64                     `json:"durationMs,omitempty"`
	FrameCount        int                        `json:"frameCount,omitempty"`
	EventCount        int                        `json:"eventCount,omitempty"`
	DetectedTrackID   *string                    `json:"detectedTrackId,omitempty"`
	DetectedLayoutID  *string                    `json:"detectedLayoutId,omitempty"`
	TrackCapabilities *tracks.LayoutCapabilities `json:"trackCapabilities,omitempty"`
}

type FinishRequest struct {
	EndedUnixMs *int64 `json:"endedUnixMs,omitempty"`
}

func (r FinishRequest) EndedAt(now func() time.Time) time.Time {
	if r.EndedUnixMs == nil {
		return now().UTC()
	}

	return time.UnixMilli(*r.EndedUnixMs).UTC()
}

func NewDTO(session Session, frameCount int, eventCount int) DTO {
	dto := DTO{
		ID:          session.ID,
		Source:      session.Source,
		Game:        session.Game,
		Platform:    session.Platform,
		DriverAlias: session.DriverAlias,
		StartedAt:   session.StartedAt.UTC(),
		EndedAt:     session.EndedAt,
		TrackID:     session.TrackID,
		LayoutID:    session.EffectiveLayoutID(),
		Status:      session.Status(),
		DurationMs:  session.DurationMs(),
		FrameCount:  frameCount,
		EventCount:  eventCount,
	}
	if session.DetectedTrackID != "" {
		v := session.DetectedTrackID
		dto.DetectedTrackID = &v
	}
	if session.DetectedLayoutID != "" {
		v := session.DetectedLayoutID
		dto.DetectedLayoutID = &v
	}
	return dto
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
