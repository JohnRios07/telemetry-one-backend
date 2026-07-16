package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"
)

type RejectionSummaryRecord struct {
	ID                 string
	SessionID          string
	Status             string
	ReceivedFrames     int
	AcceptedFrames     int
	RejectedFrames     int
	AcceptedFromUnixMs int64
	AcceptedToUnixMs   int64
	Summary            RejectionSummary
	IngestedAt         time.Time
}

type RejectedSummaryAggregate struct {
	RejectedFrames int
	Summary        RejectionSummary
}

type RejectionSummaryStore interface {
	Append(ctx context.Context, record RejectionSummaryRecord) error
	Summary(ctx context.Context, sessionID string) (RejectedSummaryAggregate, error)
	Summaries(ctx context.Context, sessionIDs []string) (map[string]RejectedSummaryAggregate, error)
}

type MemoryRejectionSummaryStore struct {
	mu      sync.RWMutex
	records map[string][]RejectionSummaryRecord
}

func NewMemoryRejectionSummaryStore() *MemoryRejectionSummaryStore {
	return &MemoryRejectionSummaryStore{records: make(map[string][]RejectionSummaryRecord)}
}

func (s *MemoryRejectionSummaryStore) Append(ctx context.Context, record RejectionSummaryRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || record.SessionID == "" || record.RejectedFrames <= 0 {
		return nil
	}

	record = cloneRejectionSummaryRecord(record)
	if record.ID == "" {
		record.ID = newRejectionSummaryID()
	}
	if record.IngestedAt.IsZero() {
		record.IngestedAt = time.Now().UTC()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[record.SessionID] = append(s.records[record.SessionID], record)
	return nil
}

func (s *MemoryRejectionSummaryStore) Summary(ctx context.Context, sessionID string) (RejectedSummaryAggregate, error) {
	if err := ctx.Err(); err != nil {
		return RejectedSummaryAggregate{}, err
	}
	if s == nil || sessionID == "" {
		return RejectedSummaryAggregate{}, nil
	}

	s.mu.RLock()
	records := cloneRejectionSummaryRecords(s.records[sessionID])
	s.mu.RUnlock()

	return aggregateRejectionSummaries(records), nil
}

func (s *MemoryRejectionSummaryStore) Summaries(ctx context.Context, sessionIDs []string) (map[string]RejectedSummaryAggregate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make(map[string]RejectedSummaryAggregate, len(sessionIDs))
	if s == nil || len(sessionIDs) == 0 {
		return result, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := make(map[string]struct{}, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		if sessionID == "" {
			continue
		}
		if _, ok := seen[sessionID]; ok {
			continue
		}
		seen[sessionID] = struct{}{}
		result[sessionID] = aggregateRejectionSummaries(cloneRejectionSummaryRecords(s.records[sessionID]))
	}

	return result, nil
}

func aggregateRejectionSummaries(records []RejectionSummaryRecord) RejectedSummaryAggregate {
	counts := make(map[string]int)
	var total int
	for _, record := range records {
		total += record.RejectedFrames
		for _, reason := range record.Summary.Reasons {
			counts[reason.Code] += reason.Count
		}
	}
	reasons := make([]RejectionReasonCount, 0, len(counts))
	for code, count := range counts {
		reasons = append(reasons, RejectionReasonCount{Code: code, Count: count})
	}
	sort.SliceStable(reasons, func(i, j int) bool {
		if reasons[i].Count != reasons[j].Count {
			return reasons[i].Count > reasons[j].Count
		}
		return reasons[i].Code < reasons[j].Code
	})

	return RejectedSummaryAggregate{RejectedFrames: total, Summary: RejectionSummary{Reasons: reasons}}
}

func cloneRejectionSummaryRecord(record RejectionSummaryRecord) RejectionSummaryRecord {
	record.Summary = cloneRejectionSummary(record.Summary)
	return record
}

func cloneRejectionSummaryRecords(records []RejectionSummaryRecord) []RejectionSummaryRecord {
	cloned := make([]RejectionSummaryRecord, len(records))
	for i, record := range records {
		cloned[i] = cloneRejectionSummaryRecord(record)
	}
	return cloned
}

func cloneRejectionSummary(summary RejectionSummary) RejectionSummary {
	reasons := make([]RejectionReasonCount, len(summary.Reasons))
	copy(reasons, summary.Reasons)
	return RejectionSummary{Reasons: reasons}
}

func newRejectionSummaryID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return fmt.Sprintf("ingest_rejection_%d", time.Now().UnixNano())
	}
	return "ingest_rejection_" + hex.EncodeToString(bytes[:])
}

var _ RejectionSummaryStore = (*MemoryRejectionSummaryStore)(nil)
