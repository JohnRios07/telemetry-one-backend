package admin

import (
	"context"
	"sort"
	"time"

	"telemetry-one-backend/internal/sessions"
	"telemetry-one-backend/internal/telemetry"
)

const (
	ModeMemory   = "memory"
	ModePostgres = "postgres"

	DefaultLimit = 10
	MaxLimit     = 100
	MinLimit     = 1
	DefaultDays  = 7
	MaxDays      = 90
	MinDays      = 1
)

type Totals struct {
	Sessions        int   `json:"sessions"`
	ActiveSessions  int   `json:"activeSessions"`
	FinishedSessions int  `json:"finishedSessions"`
	FrameBatches    *int  `json:"frameBatches,omitempty"`
	PersistedFrames *int  `json:"persistedFrames,omitempty"`
	EngineerEvents  *int  `json:"engineerEvents,omitempty"`
	AIAuditLogs     *int  `json:"aiAuditLogs,omitempty"`
}

type RecentSession struct {
	ID             string  `json:"id"`
	StartedAt      string  `json:"startedAt"`
	EndedAt        *string `json:"endedAt"`
	DurationMs     *int64  `json:"durationMs,omitempty"`
	FrameBatches   int     `json:"frameBatches"`
	PersistedFrames int    `json:"persistedFrames"`
	Status         string  `json:"status"`
}

type DailyStats struct {
	Date            string `json:"date"`
	Sessions        int    `json:"sessions"`
	FrameBatches    int    `json:"frameBatches"`
	PersistedFrames int    `json:"persistedFrames"`
}

type IngestStatsResponse struct {
	Mode           string          `json:"mode"`
	Totals         Totals          `json:"totals"`
	RecentSessions []RecentSession `json:"recentSessions"`
	Daily          []DailyStats    `json:"daily"`
}

type StatsRepository interface {
	Stats(ctx context.Context, limit int, days int) (*IngestStatsResponse, error)
}

type MemoryStatsRepo struct {
	sessionRepo sessions.Repository
	frameStore  telemetry.Store
}

func NewMemoryStatsRepo(sessionRepo sessions.Repository, frameStore telemetry.Store) *MemoryStatsRepo {
	return &MemoryStatsRepo{sessionRepo: sessionRepo, frameStore: frameStore}
}

func (r *MemoryStatsRepo) Stats(ctx context.Context, limit int, days int) (*IngestStatsResponse, error) {
	all, err := r.sessionRepo.List(ctx)
	if err != nil {
		return nil, err
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].StartedAt.After(all[j].StartedAt)
	})

	var totalActive, totalFinished int
	for _, s := range all {
		if s.Status() == sessions.StatusActive {
			totalActive++
		} else {
			totalFinished++
		}
	}

	totalSessions := len(all)

	var totalPersistedFrames int
	for _, s := range all {
		frames, err := r.frameStore.Frames(ctx, s.ID)
		if err != nil {
			return nil, err
		}
		totalPersistedFrames += len(frames)
	}
	pf := totalPersistedFrames

	totals := Totals{
		Sessions:         totalSessions,
		ActiveSessions:   totalActive,
		FinishedSessions: totalFinished,
		PersistedFrames:  &pf,
	}

	recentLimit := limit
	if recentLimit > len(all) {
		recentLimit = len(all)
	}
	recent := all[:recentLimit]
	recentSessions := make([]RecentSession, 0, recentLimit)
	for _, s := range recent {
		rs := RecentSession{
			ID:        s.ID,
			StartedAt: s.StartedAt.UTC().Format(time.RFC3339),
			Status:    string(s.Status()),
		}
		if s.EndedAt != nil {
			ended := s.EndedAt.UTC().Format(time.RFC3339)
			rs.EndedAt = &ended
			d := s.EndedAt.Sub(s.StartedAt).Milliseconds()
			rs.DurationMs = &d
		}
		frames, err := r.frameStore.Frames(ctx, s.ID)
		if err != nil {
			return nil, err
		}
		rs.PersistedFrames = len(frames)
		recentSessions = append(recentSessions, rs)
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -days)
	dailyMap := make(map[string]*DailyStats)
	for _, s := range all {
		if s.StartedAt.Before(cutoff) {
			continue
		}
		date := s.StartedAt.UTC().Format("2006-01-02")
		if _, ok := dailyMap[date]; !ok {
			dailyMap[date] = &DailyStats{Date: date}
		}
		dailyMap[date].Sessions++
		frames, err := r.frameStore.Frames(ctx, s.ID)
		if err != nil {
			return nil, err
		}
		dailyMap[date].PersistedFrames += len(frames)
	}
	daily := make([]DailyStats, 0, len(dailyMap))
	for _, d := range dailyMap {
		daily = append(daily, *d)
	}
	sort.Slice(daily, func(i, j int) bool {
		return daily[i].Date < daily[j].Date
	})

	return &IngestStatsResponse{
		Mode:           ModeMemory,
		Totals:         totals,
		RecentSessions: recentSessions,
		Daily:          daily,
	}, nil
}
