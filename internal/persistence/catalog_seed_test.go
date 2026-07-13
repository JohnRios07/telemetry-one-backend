package persistence

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"telemetry-one-backend/internal/tracks"
)

type catalogSeedExecCall struct {
	sql  string
	args []any
}

type fakeCatalogSeedDB struct {
	execs   []catalogSeedExecCall
	execErr error
}

func (db *fakeCatalogSeedDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	db.execs = append(db.execs, catalogSeedExecCall{sql: sql, args: args})
	if db.execErr != nil {
		return pgconn.CommandTag{}, db.execErr
	}

	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func TestSeedTrackReferenceCatalogMirrorsOfficialCatalogTracksIdempotently(t *testing.T) {
	db := &fakeCatalogSeedDB{}
	catalog := tracks.OfficialGT7SeedCatalog()

	if err := SeedTrackReferenceCatalog(context.Background(), db, catalog); err != nil {
		t.Fatalf("seed catalog: %v", err)
	}

	if len(db.execs) != len(catalog.Tracks) {
		t.Fatalf("expected one upsert per catalog track, got %d", len(db.execs))
	}
	for i, track := range catalog.Tracks {
		call := db.execs[i]
		if !strings.Contains(call.sql, "ON CONFLICT (id) DO UPDATE") {
			t.Fatalf("expected idempotent track upsert, got SQL %s", call.sql)
		}
		if got := call.args[0]; got != track.ID {
			t.Fatalf("expected track id %q, got %q", track.ID, got)
		}
		if got := call.args[1]; got != track.Name {
			t.Fatalf("expected track name %q, got %q", track.Name, got)
		}
		if got := call.args[2]; got != track.Layouts[0].Name {
			t.Fatalf("expected first layout name %q, got %q", track.Layouts[0].Name, got)
		}
		if got := call.args[4]; got != track.Layouts[0].LengthMeters {
			t.Fatalf("expected first layout length %v, got %v", track.Layouts[0].LengthMeters, got)
		}
	}
}

func TestSeedTrackReferenceCatalogRequiresProvenanceBackedCatalog(t *testing.T) {
	db := &fakeCatalogSeedDB{}
	catalog := tracks.Catalog{
		CatalogVersion: tracks.CatalogVersionV1,
		Tracks: []tracks.CatalogTrack{{
			ID:   "track-without-source",
			Name: "Track Without Source",
			Layouts: []tracks.CatalogLayout{{
				ID:           "layout-without-source",
				Name:         "Layout Without Source",
				LengthMeters: 1000,
			}},
		}},
	}

	err := SeedTrackReferenceCatalog(context.Background(), db, catalog)

	if !errors.Is(err, tracks.ErrMissingMetadataSource) {
		t.Fatalf("expected missing metadata source error, got %v", err)
	}
	if len(db.execs) != 0 {
		t.Fatalf("expected invalid catalog not to be written, got %d execs", len(db.execs))
	}
}

func TestSeedTrackReferenceCatalogReturnsExecError(t *testing.T) {
	dbErr := errors.New("database unavailable")
	db := &fakeCatalogSeedDB{execErr: dbErr}

	err := SeedTrackReferenceCatalog(context.Background(), db, tracks.OfficialGT7SeedCatalog())

	if !errors.Is(err, dbErr) {
		t.Fatalf("expected database error, got %v", err)
	}
}
