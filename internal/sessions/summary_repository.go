package sessions

import (
	"context"

	"telemetry-one-backend/internal/telemetry"
)

const (
	DefaultListLimit = 20
	MinListLimit     = 1
	MaxListLimit     = 100
)

type SummaryFilter struct {
	Limit int
}

type SessionSummaryItem struct {
	ID               string  `json:"id"`
	Source           string  `json:"source"`
	Game             string  `json:"game"`
	Platform         string  `json:"platform"`
	DriverAlias      string  `json:"driverAlias,omitempty"`
	TrackID          string  `json:"trackId,omitempty"`
	Status           Status  `json:"status"`
	StartedAt        string  `json:"startedAt"`
	EndedAt          *string `json:"endedAt,omitempty"`
	DurationMs       *int64  `json:"durationMs,omitempty"`
	FrameBatches     int     `json:"frameBatches"`
	PersistedFrames  int     `json:"persistedFrames"`
	RejectedFrames   int     `json:"rejectedFrames"`
	EventCount       int     `json:"eventCount"`
	DetectedTrackID  *string `json:"detectedTrackId,omitempty"`
	DetectedLayoutID *string `json:"detectedLayoutId,omitempty"`
}

type ListResponse struct {
	Sessions []SessionSummaryItem `json:"sessions"`
}

type TimeRange struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

type SessionDetailSummary struct {
	Session            SessionSummaryItem          `json:"session"`
	FrameBatches       int                         `json:"frameBatches"`
	PersistedFrames    int                         `json:"persistedFrames"`
	TimeRangeMs        *TimeRange                  `json:"timeRangeMs,omitempty"`
	LapsDetected       int                         `json:"lapsDetected"`
	EngineerEventCount int                         `json:"engineerEventCount"`
	AIAuditLogCount    int                         `json:"aiAuditLogCount"`
	RejectionSummary   *telemetry.RejectionSummary `json:"rejectionSummary,omitempty"`
}

type SummaryRepository interface {
	List(ctx context.Context, filter SummaryFilter) (*ListResponse, error)
	Summary(ctx context.Context, sessionID string) (*SessionDetailSummary, error)
}
