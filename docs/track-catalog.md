# Track Catalog Metadata

Phase 3.1 defines the versioned metadata format used as Telemetry One's source of truth for track, layout, sector, and corner names. Gran Turismo 7 UDP data is not authoritative for names.

## Version

The MVP catalog uses `catalogVersion: "telemetry-one.track-catalog.v1"`. Unknown versions are rejected until an explicit migration path exists.

## Provenance Policy

Telemetry One treats track data sources as provenance, not interchangeable truth.

- `GT7Tracks` `track_list.csv` is allowed as catalog metadata for `ID`, `Name`, `Country`, `Category`, `Length`, `LongestStraight`, `ElevationDiff`, `Altitude`, `LayoutNumber`, `IsReverse`, `PitLaneDelta`, `IsOval`, `NumCorners`, and `NoRain`.
- `GT7Tracks` raw `x/z/y` dumps are fixture-only. They may support dev/test scenarios, but they are not production geometry and must not be used for corner names, sector boundaries, apexes, or centerlines.
- `gt7info` `course.csv` is allowed as a secondary catalog seed for layout `ID`, `Name`, `Length`, `NumCorners`, and other summary fields.
- The official Gran Turismo 7 tracklist page/assets are allowed as a factual reference for layout `ID`, `Name`, base, `Length`, `cornerCount`, `straight`, `elevation`, and `country`.
- The official page/assets are provenance references, not mirrored open data. They do not provide an explicit open-data license, so Telemetry One should not present them as redistributable source data.
- No source means no emitted name.
- `NumCorners` may only produce ordinal labels `Corner 1` through `Corner N`.
- Never infer named corners, sectors, apexes, or centerlines from raw telemetry dumps or from length-only matching.
- Every imported track, layout, sector, and corner must carry `sources[]` when its display name may be emitted.

Safe vs unsafe data types:

- Safe: track/layout identity, lengths, countries, `NumCorners`, and provenance notes from the sources above.
- Safe: ordinal corner labels generated from a sourced `NumCorners` value.
- Unsafe: named corners inferred from telemetry, sector cuts inferred from dumps, apex positions inferred from `NumCorners`, and centerlines fabricated from raw dumps.

## Shape

```json
{
  "catalogVersion": "telemetry-one.track-catalog.v1",
  "generatedBy": "manual catalog process",
  "notes": "Human-readable provenance notes.",
  "tracks": [
    {
      "id": "track_id",
      "name": "Track name from catalog",
      "country": "Optional country",
      "sources": [
        {
          "url": "https://www.gran-turismo.com/us/gt7/tracklist/",
          "sourceType": "official_gran_turismo_tracklist",
          "retrievedAt": "2026-07-12",
          "note": "official Gran Turismo source"
        }
      ],
      "layouts": [
        {
          "id": "layout_id",
          "name": "Layout name from catalog",
          "lengthMeters": 1300,
          "centerLine": [
            { "index": 0, "x": 0, "y": 0, "z": 0, "accumulatedMeters": 0 },
            { "index": 1, "x": 400, "y": 0, "z": 0, "accumulatedMeters": 400 }
          ],
          "sectors": [
            { "number": 1, "name": "Sector name from catalog", "startMeters": 0, "endMeters": 430 }
          ],
          "corners": [
            {
              "id": "layout_t1",
              "number": 1,
              "name": "Corner name from catalog",
              "definitionMode": "catalog_manual",
              "startMeters": 100,
              "apexMeters": 140,
              "endMeters": 190,
              "direction": "right",
              "severity": "medium",
              "confidence": 1
            }
          ]
        }
      ]
    }
  ]
}
```

The machine-readable schema is in `docs/track-catalog.schema.json`.

## Validation Rules

- `catalogVersion` is required and must be `telemetry-one.track-catalog.v1`.
- Track, layout, and corner IDs must be globally unique within a catalog.
- Track, layout, sector, and corner names are required because downstream APIs must not source these names from UDP telemetry.
- Corner `definitionMode` is required. In the MVP, usable corner ranges must be `catalog_manual`. `catalog_enumerated` is allowed only for ordinal `Corner 1`..`Corner N` entries generated from a sourced corner count; those entries must not carry `startMeters`, `apexMeters`, or `endMeters` ranges. `auto_detected_future` is reserved documentation for a later automatic-detection contract and is rejected by the current Go validator.
- `sources` is optional for synthetic/dev fixtures, but any declared source must include `url`, `sourceType`, and `retrievedAt`.
- When source data comes from GT7Tracks, gt7info, or the official GT7 tracklist, the source entry must identify that provenance explicitly instead of using a generic placeholder.
- Layout `lengthMeters` must be greater than zero.
- Sector ranges must satisfy `0 <= startMeters < endMeters <= lengthMeters`.
- Manual corner ranges normally satisfy `0 <= startMeters < apexMeters < endMeters <= lengthMeters`.
- Enumerated corners are non-spatial metadata only. They use generated display labels like `Corner 1`, `Corner 2`, etc., and keep all distance fields at `0` because the current source does not provide reliable corner ranges, names, apexes, or centerline geometry.
- A closed-loop corner may wrap across start/finish by using `startMeters > endMeters`. In that case the corner must be the final corner in distance order, its apex must be inside the wrapped interval (`apexMeters >= startMeters` or `apexMeters < endMeters`), and its early-lap portion must not overlap the first corner.
- `centerLine` is optional. When present, it must contain at least two finite points, start at `accumulatedMeters: 0`, increase monotonically, stay within `lengthMeters`, and avoid zero-length consecutive geometry.
- Sectors and corners must be listed in increasing distance order and must not overlap with the previous range of the same type.
- Sector and corner ranges may have gaps. Gaps are valid straights or unmodeled areas in MVP metadata.

## Current Corner Resolution

Phase 4.3 resolves the current corner from `distanceFromStart` and catalog-owned manual corner ranges only. It never reads or derives `cornerId` or `cornerName` from GT7 UDP telemetry.

Boundary semantics are explicit:

- `startMeters` is inclusive.
- `endMeters` is exclusive.
- Distances in gaps between catalog corners resolve to `status: "no_corner"` with `reason: "outside_corner_range"`.
- Layouts with no catalog corners resolve to `status: "no_corner"` with `reason: "catalog_has_no_corners"`.
- Closed-loop resolution normalizes distances by layout length and supports final wrap-around corner ranges.
- Open-layout resolution does not match wrap-around ranges.
- Corners whose `definitionMode` is not `catalog_manual` are not spatially resolvable in the MVP. If a layout has only `catalog_enumerated` entries, current-corner resolution returns `status: "no_corner"` with `reason: "catalog_has_enumerated_corners"` because there are no ranges to match. If a future automatic definition reaches the resolver before its contract is implemented, resolution returns `status: "no_corner"` with `reason: "unsupported_corner_definition"` rather than emitting a corner name.

## Manual MVP vs Future Automatic Corner Detection

Phase 4.5 separates two concepts that must not be conflated:

- `catalog_manual`: the only supported MVP mode. A human-curated catalog entry provides the corner range, name, ID, and provenance. Resolver and analysis engines may emit these IDs/names after normal catalog/source-of-truth validation.
- `catalog_enumerated`: a generated ordinal entry created from a provenance-backed `NumCorners` value. It can support UI/advice copy such as "Corner 7" but is not a named corner catalog and cannot be resolved from distance until reliable ranges or geometry are added.
- `auto_detected_future`: a reserved future mode for algorithmically detected candidate ranges. It is intentionally unsupported in catalog v1 runtime validation and cannot emit corner names without catalog provenance.

Automatic detection may later propose anonymous or candidate ranges, but it must not become a source of official track/corner naming by itself. Names remain catalog metadata, not GT7 UDP output and not inferred geometry labels.

## Source-of-Truth Validation

`Catalog.Validate()` checks the structural catalog contract and still allows synthetic/dev fixtures without provenance. Runtime metadata that may be emitted to Flutter, event payloads, or future AI consumers must pass `Catalog.ValidateSourceOfTruth()` instead.

Source-of-truth validation requires every emitted display-name owner to be provenance-backed:

- Track names require complete track `sources`.
- Layout names require complete layout `sources`.
- Sector names require complete sector `sources` when sectors are present.
- Corner names require complete corner `sources` when corners are present.

Each source must include `url`, `sourceType`, and `retrievedAt`. GT7 UDP telemetry is never a source for track, layout, sector, or corner names. If metadata lacks provenance, consumers must keep the selected display name unknown rather than inventing or copying a heuristic label.

## Official GT7 / gt7info Seed

`OfficialGT7SeedCatalog()` is a curated GT7 seed for runtime detection. It combines the public Gran Turismo tracklist page and its generated JS asset as the first-party source of layout ID, name, length, and corner count, with the upstream gt7info `course.csv` data source as a secondary cross-check:

- `https://www.gran-turismo.com/gb/gt7/tracklist/` as the public entrypoint for the generated GT7 tracklist assets.
- `https://www.gran-turismo.com/common/dist/gt7/tracklist/assets/tracks.gb-DLTcO0kl.js` as the generated catalog asset containing machine-readable layout metadata.
- `https://www.gran-turismo.com/common/dist/gt7/tracklist/assets/tracks-id-list.gb-Dnd6PD5F.js` as the companion public asset for published GT7 track and layout IDs.
- `https://raw.githubusercontent.com/ddm999/gt7info/web-new/_data/db/course.csv` retrieved on `2026-07-15` as the secondary GT7 seed cross-check for layout IDs, names, lengths, and `NumCorners`.

The seed imports a representative subset rather than the full CSV in this PR. It includes common layouts plus candidates near the observed `3664m` lap-length problem: WeatherTech Raceway Laguna Seca (`3602m`), Nurburgring Sprint (`3629m`), Autodrome Lago Maggiore East/East Reverse (`3643m`), and Lake Louise Long/Long Reverse (`3694m`). This prevents the length-only detector from falsely selecting Watkins Glen for a roughly `3664m` lap; the current policy is to return `ambiguous` with nearest candidate evidence when multiple sourced layouts are similarly close.

The seed intentionally leaves `sectors` empty. It also keeps `centerLine` empty. Neither the official tracklist assets nor `course.csv` provide reliable sector boundaries, sampled centerline geometry, corner ranges, apex positions, or corner names.

`NumCorners` is used only to create `catalog_enumerated` corner entries named exactly `Corner 1`, `Corner 2`, etc. Telemetry One must not invent named corners beyond those ordinal labels. These generated entries have provenance back to `course.csv`, but they are not manually named and are not spatially resolvable.

Because the GT7 seed has only enumerated corner counts and no sourced corner ranges, Phase 4.3 current-corner resolution returns `no_corner` for official seed layouts until sourced corner ranges or reliable geometry are added. This is intentional: Telemetry One prefers an explicit no-current-corner state over pretending that `Corner N` can be inferred from distance without geometry.

Watkins Glen Short Course is included from `course.csv` with GT7 layout ID `1264`, length `3942m`, and `7` enumerated corners. The official Update 1.17 page confirms the layout exists, but the separate Short Course length used here comes from `course.csv`.

## Synthetic Fixture

`testdata/catalogs/synthetic_dev_catalog.json` is synthetic/dev-only. It is not official Gran Turismo 7 metadata and must not be used as a real-world source for track, layout, sector, or corner names.

The synthetic fixture includes a dev-only rectangular `centerLine` so geometry tests can exercise projection, cumulative distance, normalized progress, and wrap-around without pretending official GT7 centerline metadata exists.

## Distance / Progress Geometry

Phase 4.1 introduces deterministic centerline projection helpers. Given a sampled centerline, the geometry engine computes segment lengths, cumulative distance, the nearest clamped projection for a car position, `distanceFromStart`, normalized progress, and off-centerline distance. This is infrastructure only: it does not emit sector or corner names and does not infer official GT7 geometry.

Phase 4.2 adds lap continuity over sequential projected distances. The continuity tracker is session-local state: callers feed each frame's projected `distanceFromStart`, the layout length, and whether the centerline is closed. It returns accepted state with current zero-based `lapIndex`, completed `lapCount`, forward delta, total forward meters, start/finish wrap detection, reverse movement, large-jump rejection, and reset support.

Phase 4.3 adds deterministic current-corner resolution over accepted `distanceFromStart` values and catalog-manual corner ranges. It only returns catalog-owned corner IDs and names, and it returns `no_corner` when metadata is absent, unsupported, or the distance is on a straight/unmodeled gap.

Phase 4.4 adds deterministic per-corner analysis over sequential samples that already include normalized telemetry plus projected `distanceFromStart`. The analyzer does not project positions, detect tracks, infer corner names, or create Engineer events. It accepts only `catalog_manual` corner definitions, filters samples using the same corner boundary semantics as current-corner resolution, and returns an explicit `complete`, `partial`, or `unavailable` status.

Current per-corner metrics are data-derived only:

- Entry speed is the first in-corner sample speed.
- Minimum speed is the lowest speed inside the catalog corner range. This can serve as an apex-speed proxy for simple corners, but it does not claim official apex metadata beyond the catalog range.
- Exit speed is the last in-corner sample speed before `endMeters`.
- Maximum brake is the maximum available brake input inside the range, expressed as a percentage.
- First throttle reapplication is the first post-apex sample whose throttle crosses the configured threshold after being below threshold.
- Exit acceleration delta is `exitSpeed - minimumSpeed` when at least two ordered in-corner samples make that derivation safe.

Unavailable inputs stay explicit instead of being fabricated. Examples include `no_samples`, `no_samples_in_corner`, `missing_brake_data`, `missing_throttle_data`, `no_throttle_reapplication`, and `insufficient_samples`. Official GT7 seed layouts currently have no sourced corners, so these metrics are expected to run against synthetic/dev-only corners or future provenance-backed catalog corners.

Assumptions and limitations:

- Closed centerlines can increment laps only when projected distance crosses from a higher distance to a lower distance in the forward direction.
- Open centerlines never wrap and endpoint progress remains `1`.
- Small jitter is accepted without adding forward distance, incrementing laps, or moving the previous stable distance.
- Reverse movement is accepted within a bounded tolerance but does not decrement laps or add forward distance.
- Large jumps are flagged and rejected so the previous accepted state is retained; this protects later corner/event logic from teleports, bad projections, or missing data bursts.
- The tracker and per-corner analyzer are deterministic infrastructure. They do not infer corner names, emit events, or use GT7 UDP metadata.

## Initial Track Detection Heuristic

Phase 3.3 adds a conservative deterministic detector that compares retained session frames against catalog layout lengths. It requires enough retained frames and an observed completed lap, currently inferred from a lap number increment. When the observed lap length is within the configured tolerance for exactly one catalog layout and confidence meets the configured minimum, the detector returns `status: "detected"`, the catalog `trackId`, `layoutId`, catalog-owned names, confidence, evidence, and `nextAction: "use_detected_catalog_layout"`.

Because the GT7 seed does not include sampled centerlines, curvature signatures, elevation profiles, sector boundaries, or spatial corner catalogs yet, this detector does not infer geometry or current-corner metadata. Length-only evidence is intentionally capped below high-confidence geometric recognition. If there are too few frames, no completed lap, no layout within tolerance, a candidate below the minimum confidence, or multiple similarly plausible layouts, the detector returns an explicit fallback state with evidence instead of pretending certainty.

Fallback state contract:

| Status | Reason examples | Rule |
| --- | --- | --- |
| `pending` | `insufficient_data`, `no_completed_lap` | The session has not produced enough retained evidence yet. Flutter should keep collecting frames and track-dependent engines must wait. |
| `unknown` | `no_catalog_match` | A completed lap was observed, but no catalog layout matched within tolerance. Manual selection is required if downstream analysis needs a track. |
| `low_confidence` | `low_confidence` | A candidate exists but fails the configured confidence threshold. Candidate details can appear in `evidence`, but no track/layout is selected. |
| `ambiguous` | `ambiguous_length` | Multiple catalog layouts are similarly plausible. Candidate evidence is diagnostic only; no automatic selection is made. |

For all fallback states, `trackId`, `layoutId`, `trackName`, and `layoutName` are `null` in the API response. Catalog candidate details may be present in `evidence`, but consumers must not treat them as selected metadata. This keeps Flutter and future event engines conservative: unknown/low-confidence is valid and preferred over false certainty.

The detector never reads track or corner names from GT7 UDP telemetry. Names are only included when a matched catalog entry provides them.

As of Phase 3.5, the detector only selects candidates whose track and layout names have complete catalog provenance. Fallback candidate evidence is diagnostic only and never contains selected names. The GT7 seed has no sector entries and only non-spatial enumerated corner counts, so current detection responses intentionally emit no sector or corner names.

## Tradeoffs

- Layouts are nested under tracks so shared venues can hold multiple variants without duplicating venue-level metadata.
- IDs are globally unique to make future API payloads, event references, and Flutter cache keys unambiguous.
- Validation intentionally checks ordering and overlap in Go instead of relying only on JSON Schema, because JSON Schema cannot express all distance relationships cleanly without making the schema harder to maintain.
