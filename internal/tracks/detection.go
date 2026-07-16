package tracks

import (
	"math"
	"sort"

	"telemetry-one-backend/internal/telemetry"
)

const (
	DetectionStatusPending       = "pending"
	DetectionStatusUnknown       = "unknown"
	DetectionStatusLowConfidence = "low_confidence"
	DetectionStatusDetected      = "detected"
	DetectionStatusAmbiguous     = "ambiguous"

	DetectionReasonInsufficientData = "insufficient_data"
	DetectionReasonNoCompletedLap   = "no_completed_lap"
	DetectionReasonNoCatalogMatch   = "no_catalog_match"
	DetectionReasonAmbiguousLength  = "ambiguous_length"
	DetectionReasonLowConfidence    = "low_confidence"
	DetectionReasonLengthMatch      = "length_match"

	DetectionNextActionCollectMoreFrames = "collect_more_frames"
	DetectionNextActionWaitCompletedLap  = "wait_for_completed_lap"
	DetectionNextActionManualSelection   = "prompt_manual_track_selection"
	DetectionNextActionUseDetectedLayout = "use_detected_catalog_layout"
)

type DetectionOptions struct {
	MinCompletedLapFrames int
	LengthToleranceRatio  float64
	AmbiguousScoreMargin  float64
	MinDetectedConfidence float64
}

type DetectionResult struct {
	Status               string              `json:"status"`
	TrackID              *string             `json:"trackId"`
	LayoutID             *string             `json:"layoutId"`
	TrackName            *string             `json:"trackName"`
	LayoutName           *string             `json:"layoutName"`
	Confidence           float64             `json:"confidence"`
	ObservedLengthMeters float64             `json:"observedLengthMeters,omitempty"`
	Reasons              []string            `json:"reasons"`
	Evidence             []DetectionEvidence `json:"evidence"`
	NextAction           string              `json:"nextAction"`
}

type DetectionEvidence struct {
	Reason           string  `json:"reason"`
	Message          string  `json:"message"`
	TrackID          string  `json:"trackId,omitempty"`
	LayoutID         string  `json:"layoutId,omitempty"`
	CatalogMeters    float64 `json:"catalogMeters,omitempty"`
	ObservedMeters   float64 `json:"observedMeters,omitempty"`
	DifferenceMeters float64 `json:"differenceMeters,omitempty"`
	Confidence       float64 `json:"confidence,omitempty"`
}

type layoutCandidate struct {
	trackID      string
	trackName    string
	layoutID     string
	layoutName   string
	lengthMeters float64
	difference   float64
	confidence   float64
}

func DefaultDetectionOptions() DetectionOptions {
	return DetectionOptions{
		MinCompletedLapFrames: 20,
		LengthToleranceRatio:  0.03,
		AmbiguousScoreMargin:  0.05,
		MinDetectedConfidence: 0.65,
	}
}

func DetectTrack(frames []telemetry.Frame, catalog Catalog, options DetectionOptions) DetectionResult {
	options = normalizeDetectionOptions(options)
	observed, ok := observedCompletedLapLength(frames, options.MinCompletedLapFrames)
	if !ok {
		status := DetectionStatusPending
		reason := DetectionReasonNoCompletedLap
		message := "no completed lap was observed in retained frames"
		nextAction := DetectionNextActionWaitCompletedLap
		if len(frames) < options.MinCompletedLapFrames {
			reason = DetectionReasonInsufficientData
			message = "not enough retained frames to detect a completed lap"
			nextAction = DetectionNextActionCollectMoreFrames
		}

		return fallbackResult(status, 0, 0, nextAction, DetectionEvidence{Reason: reason, Message: message})
	}

	candidates := matchingCandidates(catalog, observed, options.LengthToleranceRatio)
	if len(candidates) == 0 {
		return fallbackResult(DetectionStatusUnknown, 0, observed, DetectionNextActionManualSelection, DetectionEvidence{
			Reason:         DetectionReasonNoCatalogMatch,
			Message:        "observed lap length is outside catalog tolerance",
			ObservedMeters: observed,
		})
	}

	best := candidates[0]
	if len(candidates) > 1 && (best.difference > 0 || candidates[1].difference == 0) && best.confidence-candidates[1].confidence <= options.AmbiguousScoreMargin {
		return fallbackResult(DetectionStatusAmbiguous, 0, observed, DetectionNextActionManualSelection, ambiguousEvidence(candidates, observed, options.AmbiguousScoreMargin)...)
	}
	if best.confidence < options.MinDetectedConfidence {
		return fallbackResult(DetectionStatusLowConfidence, best.confidence, observed, DetectionNextActionManualSelection, DetectionEvidence{
			Reason:           DetectionReasonLowConfidence,
			Message:          "best catalog layout match is below the minimum detection confidence",
			TrackID:          best.trackID,
			LayoutID:         best.layoutID,
			CatalogMeters:    best.lengthMeters,
			ObservedMeters:   observed,
			DifferenceMeters: best.difference,
			Confidence:       best.confidence,
		})
	}

	return DetectionResult{
		Status:               DetectionStatusDetected,
		TrackID:              stringPtr(best.trackID),
		LayoutID:             stringPtr(best.layoutID),
		TrackName:            stringPtr(best.trackName),
		LayoutName:           stringPtr(best.layoutName),
		Confidence:           best.confidence,
		ObservedLengthMeters: observed,
		Reasons:              []string{DetectionReasonLengthMatch},
		Evidence: []DetectionEvidence{{
			Reason:           DetectionReasonLengthMatch,
			Message:          "observed completed lap length matched one catalog layout within tolerance",
			TrackID:          best.trackID,
			LayoutID:         best.layoutID,
			CatalogMeters:    best.lengthMeters,
			ObservedMeters:   observed,
			DifferenceMeters: best.difference,
			Confidence:       best.confidence,
		}},
		NextAction: DetectionNextActionUseDetectedLayout,
	}
}

func normalizeDetectionOptions(options DetectionOptions) DetectionOptions {
	defaults := DefaultDetectionOptions()
	if options.MinCompletedLapFrames <= 0 {
		options.MinCompletedLapFrames = defaults.MinCompletedLapFrames
	}
	if options.LengthToleranceRatio <= 0 {
		options.LengthToleranceRatio = defaults.LengthToleranceRatio
	}
	if options.AmbiguousScoreMargin <= 0 {
		options.AmbiguousScoreMargin = defaults.AmbiguousScoreMargin
	}
	if options.MinDetectedConfidence <= 0 {
		options.MinDetectedConfidence = defaults.MinDetectedConfidence
	}

	return options
}

func observedCompletedLapLength(frames []telemetry.Frame, minFrames int) (float64, bool) {
	if len(frames) < minFrames {
		return 0, false
	}

	var currentLap int
	var currentLength float64
	var currentSamples int
	started := false

	for i, frame := range frames {
		if !started {
			currentLap = frame.LapNumber
			currentSamples = 1
			started = true
			continue
		}

		previous := frames[i-1]
		if frame.IsOnTrack && previous.IsOnTrack {
			currentLength += frameDistance(previous, frame)
		}
		currentSamples++

		if frame.LapNumber > currentLap {
			if currentSamples >= minFrames && currentLength > 0 {
				return currentLength, true
			}
			currentLap = frame.LapNumber
			currentLength = 0
			currentSamples = 1
		}
	}

	return 0, false
}

func frameDistance(a telemetry.Frame, b telemetry.Frame) float64 {
	dx := b.PositionX - a.PositionX
	dy := b.PositionY - a.PositionY
	dz := b.PositionZ - a.PositionZ

	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

func matchingCandidates(catalog Catalog, observedLength float64, toleranceRatio float64) []layoutCandidate {
	var candidates []layoutCandidate
	for _, track := range catalog.Tracks {
		for _, layout := range track.Layouts {
			if !hasSelectableLayoutMetadata(track, layout) {
				continue
			}
			if layout.LengthMeters <= 0 {
				continue
			}
			difference := math.Abs(observedLength - layout.LengthMeters)
			allowed := layout.LengthMeters * toleranceRatio
			if difference > allowed {
				continue
			}

			confidence := 0.85
			if allowed > 0 {
				confidence = 0.65 + 0.20*(1-(difference/allowed))
			}
			candidates = append(candidates, layoutCandidate{
				trackID:      track.ID,
				trackName:    track.Name,
				layoutID:     layout.ID,
				layoutName:   layout.Name,
				lengthMeters: layout.LengthMeters,
				difference:   difference,
				confidence:   confidence,
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].confidence == candidates[j].confidence {
			return candidates[i].layoutID < candidates[j].layoutID
		}

		return candidates[i].confidence > candidates[j].confidence
	})

	return candidates
}

func ambiguousEvidence(candidates []layoutCandidate, observedLength float64, margin float64) []DetectionEvidence {
	if len(candidates) == 0 {
		return nil
	}
	bestConfidence := candidates[0].confidence
	evidence := make([]DetectionEvidence, 0, len(candidates))
	for _, candidate := range candidates {
		if bestConfidence-candidate.confidence > margin {
			continue
		}
		evidence = append(evidence, DetectionEvidence{
			Reason:           DetectionReasonAmbiguousLength,
			Message:          "multiple catalog layouts have similar observed lap length evidence",
			TrackID:          candidate.trackID,
			LayoutID:         candidate.layoutID,
			CatalogMeters:    candidate.lengthMeters,
			ObservedMeters:   observedLength,
			DifferenceMeters: candidate.difference,
			Confidence:       candidate.confidence,
		})
	}

	return evidence
}

func hasSelectableLayoutMetadata(track CatalogTrack, layout CatalogLayout) bool {
	if track.ID == "" || track.Name == "" || layout.ID == "" || layout.Name == "" {
		return false
	}
	if len(track.Sources) == 0 || len(layout.Sources) == 0 {
		return false
	}

	return validateSources("track.sources", track.Sources) == nil && validateSources("layout.sources", layout.Sources) == nil
}

func fallbackResult(status string, confidence float64, observedLength float64, nextAction string, evidence ...DetectionEvidence) DetectionResult {
	reasons := make([]string, 0, len(evidence))
	if len(evidence) > 0 {
		reasons = append(reasons, evidence[0].Reason)
	}
	return DetectionResult{
		Status:               status,
		TrackID:              nil,
		LayoutID:             nil,
		TrackName:            nil,
		LayoutName:           nil,
		Confidence:           confidence,
		ObservedLengthMeters: observedLength,
		Reasons:              reasons,
		Evidence:             evidence,
		NextAction:           nextAction,
	}
}

func stringPtr(value string) *string {
	return &value
}
