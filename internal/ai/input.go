package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"telemetry-one-backend/internal/events"
)

var (
	ErrMissingContractVersion  = errors.New("ai input contractVersion is required")
	ErrUnsupportedContract     = errors.New("ai input contractVersion is not supported")
	ErrMissingSessionContext   = errors.New("ai input session context is required")
	ErrMissingSessionID        = errors.New("ai input sessionId is required")
	ErrMissingEvents           = errors.New("ai input must contain at least one engineer event or derived signal")
	ErrInvalidEvent            = errors.New("ai input contains invalid engineer event")
	ErrMissingSignalKind       = errors.New("ai input signal kind is required")
	ErrMissingSignalSummary    = errors.New("ai input signal summary is required")
	ErrRawTelemetryField       = errors.New("ai input must not contain raw telemetry fields")
	ErrMissingRedactionPolicy  = errors.New("ai input redaction policy is required")
	ErrMissingAllowedInputKind = errors.New("ai input allowedInputKinds is required")
	ErrUnsupportedInputKind    = errors.New("ai input kind is not supported")
)

const (
	ContractVersionV1 = "telemetry-one.ai-consumer-input.v1"

	AllowedInputEngineerEvents = "engineer_events"
	AllowedInputDerivedMetrics = "derived_metrics"
	AllowedInputDerivedSignals  = "derived_signals"
	AllowedInputCatalogRefs    = "catalog_refs"
	AllowedInputSessionContext = "session_context"
	UnknownStateExplicit       = "unknown_states_explicit"
)

type ConsumerInput struct {
	ContractVersion string          `json:"contractVersion"`
	Session         SessionContext  `json:"session"`
	Events          []EventEnvelope `json:"events"`
	Signals         []Signal        `json:"signals,omitempty"`
	Safety          SafetyMetadata  `json:"safety"`
	Constraints     []string        `json:"constraints,omitempty"`
}

type SessionContext struct {
	SessionID string             `json:"sessionId"`
	Track     *events.CatalogRef `json:"track"`
	Layout    *events.CatalogRef `json:"layout"`
}

type EventEnvelope struct {
	Event events.EngineerEvent `json:"event"`
}

type Signal struct {
	Kind     string           `json:"kind"`
	Severity events.Severity  `json:"severity"`
	Summary  string           `json:"summary"`
	Details  []string         `json:"details,omitempty"`
}

type SafetyMetadata struct {
	RedactionPolicy   string   `json:"redactionPolicy"`
	AllowedInputKinds []string `json:"allowedInputKinds"`
}

func DecodeConsumerInputJSON(payload []byte) (ConsumerInput, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()

	var input ConsumerInput
	if err := decoder.Decode(&input); err != nil {
		if rawTelemetryFieldError(err) {
			return ConsumerInput{}, ErrRawTelemetryField
		}
		return ConsumerInput{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return ConsumerInput{}, err
	}
	if err := input.Validate(); err != nil {
		return ConsumerInput{}, err
	}

	return input, nil
}

func (i ConsumerInput) Validate() error {
	if i.ContractVersion == "" {
		return ErrMissingContractVersion
	}
	if i.ContractVersion != ContractVersionV1 {
		return ErrUnsupportedContract
	}
	if err := i.Session.Validate(); err != nil {
		return err
	}
	if len(i.Events) == 0 && len(i.Signals) == 0 {
		return ErrMissingEvents
	}
	for index, envelope := range i.Events {
		if err := envelope.Event.Validate(); err != nil {
			return fmt.Errorf("%w at events[%d]: %v", ErrInvalidEvent, index, err)
		}
	}
	for index, signal := range i.Signals {
		if err := signal.Validate(); err != nil {
			return fmt.Errorf("%w at signals[%d]: %v", ErrInvalidEvent, index, err)
		}
	}
	if err := i.Safety.Validate(); err != nil {
		return err
	}

	return nil
}

func (s SessionContext) Validate() error {
	if s.SessionID == "" {
		return ErrMissingSessionID
	}
	if err := s.Track.Validate(); err != nil {
		return err
	}
	if err := s.Layout.Validate(); err != nil {
		return err
	}

	return nil
}

func (s SafetyMetadata) Validate() error {
	if s.RedactionPolicy == "" {
		return ErrMissingRedactionPolicy
	}
	if len(s.AllowedInputKinds) == 0 {
		return ErrMissingAllowedInputKind
	}
	for _, kind := range s.AllowedInputKinds {
		if !allowedInputKind(kind) {
			return ErrUnsupportedInputKind
		}
	}

	return nil
}

func (s Signal) Validate() error {
	if s.Kind == "" {
		return ErrMissingSignalKind
	}
	if s.Summary == "" {
		return ErrMissingSignalSummary
	}
	if s.Severity != "" && s.Severity != events.SeverityLow && s.Severity != events.SeverityMedium && s.Severity != events.SeverityHigh {
		return events.ErrUnsupportedSeverity
	}
	return nil
}

func allowedInputKind(kind string) bool {
	switch kind {
	case AllowedInputEngineerEvents, AllowedInputDerivedMetrics, AllowedInputDerivedSignals, AllowedInputCatalogRefs, AllowedInputSessionContext, UnknownStateExplicit:
		return true
	default:
		return false
	}
}

func rawTelemetryFieldError(err error) bool {
	for _, field := range rawTelemetryFields() {
		if err.Error() == "json: unknown field \""+field+"\"" {
			return true
		}
	}

	return false
}

func rawTelemetryFields() []string {
	return []string{
		"positionX",
		"positionY",
		"positionZ",
		"yawRadians",
		"yawRate",
		"speedMps",
		"rpm",
		"gear",
		"throttle",
		"brake",
		"steering",
		"fuelLiters",
		"wheelSpeedFL",
		"wheelSpeedFR",
		"wheelSpeedRL",
		"wheelSpeedRR",
		"isOnTrack",
		"frames",
		"telemetryFrames",
	}
}
