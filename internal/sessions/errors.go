package sessions

import "errors"

var (
	ErrMissingSource    = errors.New("source is required")
	ErrMissingGame      = errors.New("game is required")
	ErrMissingPlatform  = errors.New("platform is required")
	ErrInvalidStartedAt = errors.New("startedUnixMs must be greater than zero")
	ErrNotFound         = errors.New("session not found")
	ErrAlreadyFinished  = errors.New("session is already finished")
	ErrInvalidEndedAt   = errors.New("endedUnixMs must be greater than or equal to startedUnixMs")
	ErrMissingID        = errors.New("session id is required")
)
