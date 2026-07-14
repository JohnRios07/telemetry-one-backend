package sessions

import (
	"context"
	"fmt"
	"sort"
	"time"

	"telemetry-one-backend/internal/events"
	"telemetry-one-backend/internal/telemetry"
)

type MemorySummaryRepository struct {
	sessionRepo Repository
	frameStore  telemetry.Store
	eventRepo   events.Repository
}

func NewMemorySummaryRepository(sessionRepo Repository, frameStore telemetry.Store, eventRepo events.Repository) *MemorySummaryRepository {
	return &MemorySummaryRepository{
		sessionRepo: sessionRepo,
		frameStore:  frameStore,
		eventRepo:   eventRepo,
	}
}

func (r *MemorySummaryRepository) List(ctx context.Context, filter SummaryFilter) (*ListResponse, error) {
	all, err := r.sessionRepo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	sort.Slice(all, func(i, j int) bool {
		if !all[i].StartedAt.Equal(all[j].StartedAt) {
			return all[i].StartedAt.After(all[j].StartedAt)
		}
		return all[i].ID > all[j].ID
	})

	limit := filter.Limit
	if limit <= 0 || limit > len(all) {
		limit = len(all)
	}

	items := make([]SessionSummaryItem, 0, limit)
	for _, s := range all[:limit] {
		item := r.buildItem(ctx, s)
		items = append(items, item)
	}

	return &ListResponse{Sessions: items}, nil
}

func (r *MemorySummaryRepository) Summary(ctx context.Context, sessionID string) (*SessionDetailSummary, error) {
	session, err := r.sessionRepo.FindByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	item := r.buildItem(ctx, session)

	frames, _ := r.frameStore.Frames(ctx, sessionID)

	pf := len(frames)
	fb := 0

	var timeRange *TimeRange
	var laps int

	if len(frames) > 0 {
		from := frames[0].TimestampUnixMs
		to := frames[0].TimestampUnixMs
		lapSet := make(map[int]struct{})
		for _, f := range frames {
			if f.TimestampUnixMs < from {
				from = f.TimestampUnixMs
			}
			if f.TimestampUnixMs > to {
				to = f.TimestampUnixMs
			}
			lapSet[f.LapNumber] = struct{}{}
		}
		timeRange = &TimeRange{From: from, To: to}
		laps = len(lapSet)
	}

	eventCount := 0
	if r.eventRepo != nil {
		storedEvents, err := r.eventRepo.List(ctx, events.Query{SessionID: sessionID})
		if err == nil {
			eventCount = len(storedEvents)
		}
	}

	return &SessionDetailSummary{
		Session:            item,
		FrameBatches:       fb,
		PersistedFrames:    pf,
		TimeRangeMs:        timeRange,
		LapsDetected:       laps,
		EngineerEventCount: eventCount,
		AIAuditLogCount:    0,
	}, nil
}

func (r *MemorySummaryRepository) buildItem(ctx context.Context, session Session) SessionSummaryItem {
	item := SessionSummaryItem{
		ID:          session.ID,
		Source:      session.Source,
		Game:        session.Game,
		Platform:    session.Platform,
		DriverAlias: session.DriverAlias,
		TrackID:     session.TrackID,
		Status:      session.Status(),
		StartedAt:   session.StartedAt.UTC().Format(time.RFC3339),
	}

	if session.EndedAt != nil {
		ended := session.EndedAt.UTC().Format(time.RFC3339)
		item.EndedAt = &ended
		d := session.DurationMs()
		item.DurationMs = d
	}

	frames, _ := r.frameStore.Frames(ctx, session.ID)
	item.PersistedFrames = len(frames)

	if r.eventRepo != nil {
		storedEvents, err := r.eventRepo.List(ctx, events.Query{SessionID: session.ID})
		if err == nil {
			item.EventCount = len(storedEvents)
		}
	}

	return item
}
