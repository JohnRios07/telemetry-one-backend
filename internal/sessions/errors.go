package sessions

import "errors"

var (
	ErrMissingSource    = errors.New("source is required")
	ErrMissingGame      = errors.New("game is required")
	ErrMissingPlatform  = errors.New("platform is required")
	ErrInvalidStartedAt = errors.New("startedUnixMs must be greater than zero")
)
