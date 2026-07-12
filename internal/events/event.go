package events

import (
	"errors"
	"math"
)

var (
	ErrMissingEventID          = errors.New("eventId is required")
	ErrMissingSessionID        = errors.New("sessionId is required")
	ErrMissingVersion          = errors.New("version is required")
	ErrUnsupportedVersion      = errors.New("version is not supported")
	ErrMissingType             = errors.New("type is required")
	ErrUnsupportedType         = errors.New("type is not supported")
	ErrMissingSeverity         = errors.New("severity is required")
	ErrUnsupportedSeverity     = errors.New("severity is not supported")
	ErrInvalidConfidence       = errors.New("confidence must be finite and between 0 and 1")
	ErrInvalidTimestamp        = errors.New("timestampUnixMs must be greater than zero")
	ErrInvalidTimeRange        = errors.New("timeRange must be ordered and greater than zero")
	ErrInvalidLapNumber        = errors.New("lapNumber must be zero or greater")
	ErrMissingSource           = errors.New("source is required")
	ErrUnsupportedSourceKind   = errors.New("source kind is not supported")
	ErrMissingMetricName       = errors.New("metric name is required")
	ErrInvalidMetricValue      = errors.New("metric value must be finite")
	ErrUnsupportedMetricStatus = errors.New("metric status is not supported")
	ErrUnsupportedMetricRole   = errors.New("metric role is not supported")
	ErrMissingMetrics          = errors.New("metrics must contain at least one derived metric")
	ErrRawTelemetryMetric      = errors.New("raw telemetry fields are not allowed in event metrics")
	ErrInvalidCatalogRef       = errors.New("catalog ref must not invent names")
)

const (
	ContractVersionV1 = "telemetry-one.engineer-event.v1"

	TypeEarlyBraking       EventType = "early_braking"
	TypeLateBraking        EventType = "late_braking"
	TypeLateThrottle       EventType = "late_throttle"
	TypeLowExitSpeed       EventType = "low_exit_speed"
	TypeInconsistentCorner EventType = "inconsistent_corner"

	SeverityLow    Severity = "low"
	SeverityMedium Severity = "medium"
	SeverityHigh   Severity = "high"

	SourceDeterministicRule SourceKind = "deterministic_rule"

	MetricStatusAvailable   MetricStatus = "available"
	MetricStatusUnavailable MetricStatus = "unavailable"

	MetricRoleActual    MetricRole = "actual"
	MetricRoleReference MetricRole = "reference"
	MetricRoleDelta     MetricRole = "delta"
	MetricRoleThreshold MetricRole = "threshold"

	DisplayStrategyCatalogName DisplayStrategy = "catalog_name"
	DisplayStrategyIDOnly      DisplayStrategy = "id_only"
	DisplayStrategyNone        DisplayStrategy = "none"
)

type EventType string
type Severity string
type SourceKind string
type MetricStatus string
type MetricRole string
type DisplayStrategy string

type EngineerEvent struct {
	EventID         string           `json:"eventId"`
	SessionID       string           `json:"sessionId"`
	Version         string           `json:"version"`
	Type            EventType        `json:"type"`
	Severity        Severity         `json:"severity"`
	Confidence      float64          `json:"confidence"`
	TimestampUnixMs int64            `json:"timestampUnixMs"`
	TimeRange       *TimeRange       `json:"timeRange"`
	LapNumber       int              `json:"lapNumber"`
	Track           *CatalogRef      `json:"track"`
	Layout          *CatalogRef      `json:"layout"`
	Corner          *CatalogRef      `json:"corner"`
	Metrics         []MetricEvidence `json:"metrics"`
	Source          EventSource      `json:"source"`
}

type ListResponse struct {
	SessionID string          `json:"sessionId"`
	Events    []EngineerEvent `json:"events"`
}

type TimeRange struct {
	StartUnixMs int64 `json:"startUnixMs"`
	EndUnixMs   int64 `json:"endUnixMs"`
}

type CatalogRef struct {
	ID              *string         `json:"id"`
	Name            *string         `json:"name"`
	DisplayStrategy DisplayStrategy `json:"displayStrategy"`
}

type MetricEvidence struct {
	Name   string       `json:"name"`
	Value  float64      `json:"value,omitempty"`
	Unit   string       `json:"unit,omitempty"`
	Status MetricStatus `json:"status"`
	Reason string       `json:"reason,omitempty"`
	Role   MetricRole   `json:"role,omitempty"`
}

type EventSource struct {
	Kind        SourceKind `json:"kind"`
	RuleID      string     `json:"ruleId,omitempty"`
	RuleVersion string     `json:"ruleVersion"`
}

func (e EngineerEvent) Validate() error {
	if e.EventID == "" {
		return ErrMissingEventID
	}
	if e.SessionID == "" {
		return ErrMissingSessionID
	}
	if e.Version == "" {
		return ErrMissingVersion
	}
	if e.Version != ContractVersionV1 {
		return ErrUnsupportedVersion
	}
	if e.Type == "" {
		return ErrMissingType
	}
	if !supportedType(e.Type) {
		return ErrUnsupportedType
	}
	if e.Severity == "" {
		return ErrMissingSeverity
	}
	if !supportedSeverity(e.Severity) {
		return ErrUnsupportedSeverity
	}
	if !finite(e.Confidence) || e.Confidence < 0 || e.Confidence > 1 {
		return ErrInvalidConfidence
	}
	if e.TimestampUnixMs <= 0 {
		return ErrInvalidTimestamp
	}
	if e.TimeRange != nil && (e.TimeRange.StartUnixMs <= 0 || e.TimeRange.EndUnixMs <= 0 || e.TimeRange.StartUnixMs > e.TimeRange.EndUnixMs) {
		return ErrInvalidTimeRange
	}
	if e.LapNumber < 0 {
		return ErrInvalidLapNumber
	}
	if err := e.Track.Validate(); err != nil {
		return err
	}
	if err := e.Layout.Validate(); err != nil {
		return err
	}
	if err := e.Corner.Validate(); err != nil {
		return err
	}
	if len(e.Metrics) == 0 {
		return ErrMissingMetrics
	}
	for _, metric := range e.Metrics {
		if err := metric.Validate(); err != nil {
			return err
		}
	}
	if err := e.Source.Validate(); err != nil {
		return err
	}

	return nil
}

func (r *CatalogRef) Validate() error {
	if r == nil {
		return nil
	}
	switch r.DisplayStrategy {
	case DisplayStrategyCatalogName:
		if r.ID == nil || *r.ID == "" || r.Name == nil || *r.Name == "" {
			return ErrInvalidCatalogRef
		}
	case DisplayStrategyIDOnly:
		if r.ID == nil || *r.ID == "" || r.Name != nil {
			return ErrInvalidCatalogRef
		}
	case DisplayStrategyNone:
		if r.ID != nil || r.Name != nil {
			return ErrInvalidCatalogRef
		}
	default:
		return ErrInvalidCatalogRef
	}

	return nil
}

func (m MetricEvidence) Validate() error {
	if m.Name == "" {
		return ErrMissingMetricName
	}
	if rawTelemetryMetric(m.Name) {
		return ErrRawTelemetryMetric
	}
	if !finite(m.Value) {
		return ErrInvalidMetricValue
	}
	if m.Status != MetricStatusAvailable && m.Status != MetricStatusUnavailable {
		return ErrUnsupportedMetricStatus
	}
	if m.Role != "" && m.Role != MetricRoleActual && m.Role != MetricRoleReference && m.Role != MetricRoleDelta && m.Role != MetricRoleThreshold {
		return ErrUnsupportedMetricRole
	}

	return nil
}

func (s EventSource) Validate() error {
	if s.Kind == "" || s.RuleVersion == "" {
		return ErrMissingSource
	}
	if s.Kind != SourceDeterministicRule {
		return ErrUnsupportedSourceKind
	}

	return nil
}

func supportedType(t EventType) bool {
	switch t {
	case TypeEarlyBraking, TypeLateBraking, TypeLateThrottle, TypeLowExitSpeed, TypeInconsistentCorner:
		return true
	default:
		return false
	}
}

func supportedSeverity(s Severity) bool {
	switch s {
	case SeverityLow, SeverityMedium, SeverityHigh:
		return true
	default:
		return false
	}
}

func rawTelemetryMetric(name string) bool {
	switch name {
	case "positionX", "positionY", "positionZ", "yawRadians", "yawRate", "speedMps", "rpm", "gear", "throttle", "brake", "steering", "fuelLiters", "wheelSpeedFL", "wheelSpeedFR", "wheelSpeedRL", "wheelSpeedRR", "isOnTrack":
		return true
	default:
		return false
	}
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
