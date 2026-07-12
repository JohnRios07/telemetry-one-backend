package events

import "context"

type Repository interface {
	Append(ctx context.Context, event EngineerEvent) (EngineerEvent, bool, error)
	List(ctx context.Context, query Query) ([]EngineerEvent, error)
}

type Query struct {
	SessionID string
	LapNumber *int
	CornerID  string
	Type      EventType
}

func (q Query) Validate() error {
	if q.SessionID == "" {
		return ErrMissingSessionID
	}
	if q.LapNumber != nil && *q.LapNumber < 0 {
		return ErrInvalidLapNumber
	}
	if q.Type != "" && !supportedType(q.Type) {
		return ErrUnsupportedType
	}

	return nil
}
