package trackbuilder

import (
	"errors"
	"fmt"
	"strings"
)

const (
	DefaultSmoothingWindow = 3
	DefaultEpsilonMeters   = 0.5
	DefaultResampleStep    = 2.0
)

var (
	ErrMissingInputPath        = errors.New("input path is required")
	ErrMissingLayoutID         = errors.New("layout-id is required")
	ErrInvalidSmoothingWindow  = errors.New("smoothing-window must be a positive odd integer")
	ErrInvalidEpsilon          = errors.New("epsilon must be greater than zero")
	ErrInvalidResampleStep     = errors.New("resample step must be greater than zero")
	ErrInsufficientUsablePoint = errors.New("geometry requires at least two usable points")
)

type Options struct {
	InputPath          string
	OutputPath         string
	LayoutID           string
	SmoothingWindow    int
	EpsilonMeters      float64
	ResampleStepMeters float64
}

func DefaultOptions() Options {
	return Options{
		SmoothingWindow:    DefaultSmoothingWindow,
		EpsilonMeters:      DefaultEpsilonMeters,
		ResampleStepMeters: DefaultResampleStep,
	}
}

func (o Options) Normalize() Options {
	return o
}

func (o Options) Validate() error {
	if strings.TrimSpace(o.LayoutID) == "" {
		return ErrMissingLayoutID
	}
	if o.SmoothingWindow < 1 || o.SmoothingWindow%2 == 0 {
		return fmt.Errorf("%w: got %d", ErrInvalidSmoothingWindow, o.SmoothingWindow)
	}
	if !(o.EpsilonMeters > 0) {
		return fmt.Errorf("%w: got %v", ErrInvalidEpsilon, o.EpsilonMeters)
	}
	if !(o.ResampleStepMeters > 0) {
		return fmt.Errorf("%w: got %v", ErrInvalidResampleStep, o.ResampleStepMeters)
	}

	return nil
}
