# Ingest Performance Notes

Phase 2.5 measures the current MVP ingest path without adding production metrics or external observability dependencies.

The measured paths are:

- `Frame.Normalize`: validation and canonical field checks for one frame.
- `IngestBatchRequest.Normalize`: all-or-nothing batch validation, per-frame normalization, and batch-local ordering checks.
- `FrameStore.Append`: bounded in-memory append plus defensive copying of accepted frames.
- HTTP ingest: JSON decode, route/body `sessionId` validation, batch normalization, store append, and `202 Accepted` acknowledgement.

Run the focused benchmarks from the backend folder:

```sh
go test ./internal/telemetry ./internal/api -bench 'Benchmark(FrameNormalize|IngestBatchNormalize|FrameStoreAppend|FrameStoreAppendAtRetentionLimit|IngestFramesHTTP)' -benchmem
```

## Expected Volumes

Common telemetry rates produce these frame counts per session:

| Rate | 1 minute | 5 minutes | 10 minutes | 30 minutes | 60 minutes |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 20 Hz | 1,200 | 6,000 | 12,000 | 36,000 | 72,000 |
| 30 Hz | 1,800 | 9,000 | 18,000 | 54,000 | 108,000 |
| 60 Hz | 3,600 | 18,000 | 36,000 | 108,000 | 216,000 |

The default `TELEMETRY_ONE_RETAINED_FRAMES_PER_SESSION=12000` retains approximately:

| Rate | Retained window at 12,000 frames |
| ---: | ---: |
| 20 Hz | 10 minutes |
| 30 Hz | 6 minutes 40 seconds |
| 60 Hz | 3 minutes 20 seconds |

## Batch Size Tradeoffs

The current maximum batch size is 600 frames.

| Batch size | 20 Hz coverage | 30 Hz coverage | 60 Hz coverage |
| ---: | ---: | ---: | ---: |
| 20 frames | 1 second | 0.67 seconds | 0.33 seconds |
| 60 frames | 3 seconds | 2 seconds | 1 second |
| 300 frames | 15 seconds | 10 seconds | 5 seconds |
| 600 frames | 30 seconds | 20 seconds | 10 seconds |

Small batches reduce live-analysis delay and retry cost. Large batches improve request overhead but increase latency before data is visible and make all-or-nothing rejection more expensive for Flutter retries.

## Memory Implications

`FrameStore` keeps accepted normalized frames in process memory per session. Each append defensively copies incoming frames and retains only the newest configured frames. Optional pointer fields are also copied so callers cannot mutate retained data.

Memory use scales with:

- active sessions in this backend process,
- `TELEMETRY_ONE_RETAINED_FRAMES_PER_SESSION`,
- the size of `telemetry.Frame`, including optional pointer fields,
- append churn caused by larger batches at the retention limit.

The current default intentionally favors a small MVP live-analysis window over long-term raw telemetry storage. PostgreSQL raw-frame persistence is still deferred.

## Current Limits

- Ingest validates monotonic timestamps only inside a batch; cross-batch timestamp monotonicity is still not enforced.
- Retention is process-local and ephemeral. Restarts or multiple backend instances can lose or split retained frames.
- Benchmarks use synthetic normalized frames. They measure backend ingest overhead, not GT7 UDP decode or Flutter networking.
- No production metrics endpoint or external telemetry library is introduced in this phase.
