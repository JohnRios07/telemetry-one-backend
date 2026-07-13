package ai

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresAuditDB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

type PostgresAuditStore struct {
	pool postgresAuditDB
}

func NewPostgresAuditStore(pool *pgxpool.Pool) *PostgresAuditStore {
	var db postgresAuditDB
	if pool != nil {
		db = pool
	}
	return &PostgresAuditStore{pool: db}
}

func (s *PostgresAuditStore) Record(ctx context.Context, record AuditRecord) error {
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}

	_, err := s.pool.Exec(ctx, `
INSERT INTO ai_audit_logs (
    id,
    session_id,
    trace_id,
    mode,
    provider_model,
    prompt_template_version,
    source_event_ids,
    constraint_summary,
    response_summary,
    response_summary_limited,
    prompt_token_count,
    completion_token_count,
    total_token_count,
    duration_ms,
    status,
    error_message,
    redaction_verified,
    created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
ON CONFLICT (id) DO NOTHING`,
		newAuditLogID(),
		record.SessionID,
		record.TraceID,
		record.Mode,
		record.ProviderModel,
		record.PromptTemplateVersion,
		record.SourceEventIDs,
		record.ConstraintSummary,
		record.ResponseSummary,
		record.ResponseSummaryLimited,
		record.PromptTokenCount,
		record.CompletionTokenCount,
		record.TotalTokenCount,
		record.DurationMs,
		record.Status,
		record.ErrorMessage,
		record.RedactionVerified,
		record.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("record audit log: %w", err)
	}

	return nil
}

func newAuditLogID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return fmt.Sprintf("audit_log_%d", time.Now().UnixNano())
	}
	return "audit_log_" + hex.EncodeToString(bytes[:])
}

var _ AuditLogger = (*PostgresAuditStore)(nil)
