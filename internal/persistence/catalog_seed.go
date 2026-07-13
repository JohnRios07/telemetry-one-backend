package persistence

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"telemetry-one-backend/internal/tracks"
)

type catalogSeedDB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// SeedTrackReferenceCatalog mirrors the embedded, provenance-backed track
// catalog into the runtime tracks table so Postgres foreign keys can accept
// catalog track IDs. The embedded catalog remains the read/source-of-truth
// catalog for track detection and validation.
func SeedTrackReferenceCatalog(ctx context.Context, db catalogSeedDB, catalog tracks.Catalog) error {
	if db == nil {
		return fmt.Errorf("catalog seed database is required")
	}
	if err := catalog.ValidateSourceOfTruth(); err != nil {
		return fmt.Errorf("validate track catalog seed: %w", err)
	}

	for _, track := range catalog.Tracks {
		layout := firstCatalogLayout(track)
		fingerprint, err := json.Marshal(layout.Fingerprint)
		if err != nil {
			return fmt.Errorf("marshal fingerprint for track %q: %w", track.ID, err)
		}
		centerLine, err := json.Marshal(layout.CenterLine)
		if err != nil {
			return fmt.Errorf("marshal center line for track %q: %w", track.ID, err)
		}

		if _, err := db.Exec(ctx, `
INSERT INTO tracks (id, name, layout_name, country, length_meters, fingerprint, center_line, updated_at)
VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, now())
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    layout_name = EXCLUDED.layout_name,
    country = EXCLUDED.country,
    length_meters = EXCLUDED.length_meters,
    fingerprint = EXCLUDED.fingerprint,
    center_line = EXCLUDED.center_line,
    updated_at = now()`,
			track.ID,
			track.Name,
			layout.Name,
			track.Country,
			layout.LengthMeters,
			fingerprint,
			centerLine,
		); err != nil {
			return fmt.Errorf("seed track %q: %w", track.ID, err)
		}
	}

	return nil
}

func firstCatalogLayout(track tracks.CatalogTrack) tracks.CatalogLayout {
	if len(track.Layouts) == 0 {
		return tracks.CatalogLayout{}
	}

	return track.Layouts[0]
}
