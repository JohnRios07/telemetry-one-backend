package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresAuditStore struct {
	pool *pgxpool.Pool
}

func NewPostgresAuditStore(pool *pgxpool.Pool) *PostgresAuditStore {
	return &PostgresAuditStore{pool: pool}
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
) VALUES ($1, $2, $1, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
ON CONFLICT (id) DO NOTHING`,
		record.TraceID,
		record.SessionID,
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

var _ AuditLogger = (*PostgresAuditStore)(nil)
