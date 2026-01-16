# Live Tail Plan (LogChef)

## Context & Goal
LogChef wants a robust live tail experience for UI and CLI (`-f`), but must remain **schemaless** for ClickHouse sources. The only guaranteed field is the timestamp (`tskey`). Timestamp collisions can be extreme (thousands of logs per same timestamp), so we must design for correctness (no gaps) and acceptable duplication behavior when no stable unique ID exists.

This plan consolidates architecture guidance, OSS learnings (SigNoz/HyperDX/Betterstack), and Oracle recommendations.

---

## Key Constraints
- **Schemaless ClickHouse**: no control over user log schema.
- **Guaranteed field**: timestamp (`tskey`) only.
- **Tail semantics**: must avoid gaps; minimize duplicates.
- **Transport**: SSE preferred (works for UI + CLI, simple HTTP).
- **Backend first**: UI/CLI can follow once backend is solid.

---

## Findings (External References)
### SigNoz
- Live tail uses **SSE + polling**.
- Requires a stable **`id` field** and enforces ordering by `(timestamp, id)`.
- Cursor is composite `(timestamp, id)` and uses `id > last_id` to prevent duplicates.
- This **assumes ID exists**; not directly compatible with schemaless sources.

### HyperDX
- Uses **HTTP polling** (no WS) with rolling time windows.
- Uses **client-side dedup** by hashing row content into a stable synthetic ID.
- Uses configurable refresh interval (seconds) and 15-minute rolling window.
- Works without a stable ID but accepts duplicates in overlap windows.

### Betterstack
- Live tail uses **time-based windows** and filtering; no hard dependency on IDs.
- Focused on query language + pattern filters; relies on timestamp-driven fetch.

### ClickHouse / ClickStack
- Best practice for pagination is **composite cursor** `(timestamp, unique_id)`.
- Timestamp alone is insufficient; collisions are common.

---

## Oracle Recommendation (Final)
**Use SSE for transport, but poll ClickHouse on the server.**
- Backend should query ClickHouse on a short interval (250–1000ms).
- Push batches over SSE to keep UI/CLI simple.

**Cursor must be composite `(ts, tie)`**:
- `ts`: timestamp (`tskey`)
- `tie`: deterministic tiebreaker (fingerprint or synthetic ID)

**Overlap + dedup**:
- Use overlap window (2–5s) on reconnect/continuous polling.
- Client (or server) dedups by `(ts, tie)` in a bounded LRU window.

---

## Decision Matrix
### 1) If we can add a minimal ingestion field (recommended)
Add one stable column that doesn't violate “schemaless” UX:
- **Option A (best)**: `fp UInt64 MATERIALIZED sipHash64(raw)`
- **Option B**: `ingest_id UUID DEFAULT generateUUIDv4()`

Then use cursor:
```
WHERE (ts > :ts) OR (ts = :ts AND tie > :tie)
ORDER BY ts ASC, tie ASC
LIMIT N
```
This provides correctness (no gaps) and low duplication.

### 2) If we cannot add any ingestion field (fallback)
Use **timestamp-only + overlap + dedup**:
- Query from `cursor.ts - overlap_ms` (2–5s).
- Dedup client-side using a stable hash of row content.
- Accept possibility of duplicates under extreme collisions.

---

## Proposed Backend Design
### SSE Endpoint (shared for UI + CLI)
```
GET /api/v1/teams/:teamID/sources/:sourceID/live/tail?query=...&cursor=...
Accept: text/event-stream
```

**SSE events**
- `event: logs` → `data: { events: [...], cursor: { ts, tie }, stats: {...} }`
- `event: heartbeat` → `data: { now, cursor }`
- `event: error` → `data: { message, retryable }`

### Cursor Encoding
- Encode cursor as base64: `ts_nanos|tie`
- Pass in query param `cursor` or SSE `Last-Event-ID`.

### Polling Loop (server)
- Interval: 250–1000ms (configurable)
- Batch size: 200–1000 rows
- Query window: `(cursor.ts - overlap_ms) → now()`
- Order by `(ts, tie)`; emit ascending

### Dedup Strategy
- Server (preferred) or client keeps LRU set of `(ts, tie)` for overlap window.
- Drop duplicates before emitting.

---

## UI Strategy (Phase 2)
- Add **LIVE mode** toggle.
- When enabled, open SSE stream and append logs.
- Pause/resume by closing/opening stream.
- Maintain dedup cache per session.

---

## CLI Strategy (Phase 2)
- Add `-f/--follow` in CLI.
- Use SSE client (`eventsource-client`) or fallback to polling.
- Print rows as they arrive; honor dedup.

---

## Risks & Mitigations
| Risk | Impact | Mitigation |
|------|--------|------------|
| Timestamp collisions | duplicates/gaps | composite cursor `(ts, tie)` |
| No stable tie-breaker | unavoidable duplicates | overlap + dedup fallback |
| High volume SSE | server load | limit batch size + min poll interval |
| Late-arriving logs | missing events | overlap window + dedup |

---

## Phased Execution Plan
### Phase 0 (now)
- Finalize backend contract: cursor format + SSE endpoint + polling loop.

### Phase 1 (backend)
- Implement SSE endpoint with polling.
- Integrate composite cursor logic.
- Add optional tie-breaker if ingestion allows.

### Phase 2 (UI)
- Live toggle + streaming in Explorer.
- Pause/resume and auto-scroll.

### Phase 3 (CLI)
- Add `-f` tail mode in CLI.
- SSE client with reconnection + cursor persistence.

---

## Open Questions
1. Can we add a minimal stable field (`fp` or `ingest_id`) at ingestion without violating “schemaless”? If yes, do it now.
2. If not, accept timestamp-only + overlap + dedup as v1.
3. What should default poll interval be? (Oracle suggests 250–1000ms; HyperDX default is ~4s).
