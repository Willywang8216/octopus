# Codebase audit patches — 2026-04-28

This directory documents the bugs found and fixed during the channel/group
revamp. All fixes are applied in-tree to the listed files; this is a
reference for review.

## Applied (compiled + go vet clean)

### 1. HTTP transport timeouts — `internal/client/http.go`

**Problem**: `http.Client` had no timeouts of any kind. A stalled upstream
hung the entire request indefinitely; failover never advanced.

**Fix**: Added per-connection limits via the underlying `http.Transport`:
- DialContext timeout 10s
- TLSHandshakeTimeout 10s
- ResponseHeaderTimeout 30s (kills connections where headers never arrive)
- IdleConnTimeout 90s
- MaxIdleConns 200, MaxIdleConnsPerHost 50
- ForceAttemptHTTP2

Deliberately did **not** set `http.Client.Timeout` — that field cancels
streams mid-response. Per-attempt total cancellation is enforced in the
relay layer instead (see #2).

### 2. Per-attempt request timeout — `internal/relay/relay.go`

**Problem**: Even with transport timeouts, a slow upstream that drips
data could hold a non-streaming attempt indefinitely.

**Fix**: Wrap non-streaming attempts in `context.WithTimeout(90s)`.
Streaming attempts are left unbounded (relying on group-level
`first_token_time_out` for stream health).

```go
isStream := ra.internalRequest.Stream != nil && *ra.internalRequest.Stream
if !isStream {
    var cancel context.CancelFunc
    ctx, cancel = context.WithTimeout(ctx, nonStreamAttemptTimeout)
    defer cancel()
}
```

### 3. Weighted balancer correctness — `internal/relay/balancer/balancer.go`

**Problem**: Score formula `rand.Float64() * w / totalWeight` does NOT
produce a weight-proportional distribution. The actual probability of
item *i* winning was nearly uniform regardless of weight.

**Fix**: A-Res reservoir sampling: `score = U^(1/weight)` where
`U ~ Uniform(0,1)`. Probability that score_i > score_j scales monotonically
with weight ratio, which is the intended behaviour.

### 4. Defensive nil-check on inbound adapter — `internal/relay/relay.go`

**Problem**: `inbound.Get(InboundType)` can return nil for unregistered
enum values. The relay called `.TransformRequest(...)` on it, causing
a nil-pointer crash.

**Fix**: Check for nil and return a 500 with a clear error instead:

```go
inAdapter := inbound.Get(inboundType)
if inAdapter == nil {
    err := fmt.Errorf("unsupported inbound type: %d", inboundType)
    resp.Error(c, http.StatusInternalServerError, err.Error())
    return nil, nil, err
}
```

The outbound side already had this check.

### 5. Cache shard-count must be power of 2 — `internal/utils/cache/cache.go`

**Problem**: `getShard(hashedKey & shardMask)` uses `shardMask = shards - 1`,
which only works correctly when `shards` is a power of 2. Caller passing
an arbitrary value (e.g. 100) would skip slots and double-count others.

**Fix**: Round shards up to the next power of 2 in `New()`. Existing
callers all pass 16 (already power of 2), so behaviour is unchanged.

### 6. Auth middleware — wrong status code — `internal/server/middleware/auth.go`

**Problem**: Missing `Authorization` header returned 400 Bad Request
instead of 401 Unauthorized (RFC 7235).

**Fix**: Return 401 for both missing and invalid tokens.

### 7. JWT signing security — `internal/server/auth/auth.go`

**Problems**:
1. Secret = `username + password` (no separator). `("ab","cd")` and
   `("a","bcd")` collide.
2. `jwt.Parse` did not pin the algorithm; vulnerable to
   `alg=none` and HS256/RS256 confusion attacks (mitigated in jwt-go v5
   by type-checking the keyfunc, but still good practice to be explicit).

**Fixes**:
- Secret = `username + ":" + password`.
- Explicit `jwt.WithValidMethods([]string{"HS256"})`.
- Inline check that `token.Method` is `*jwt.SigningMethodHMAC`.

**Not fixed** (deferred — schema change required): the password should not
be the JWT secret at all. A persistent random secret stored in settings
would be safer.

### 8. Race condition on `lastSyncModelsTime` — `internal/task/sync.go`

**Problem**: `lastSyncModelsTime time.Time` was written by the sync task
goroutine and read by the HTTP handler goroutine without synchronization.

**Fix**: Use `sync/atomic.Value` for safe concurrent access.

## Applied in follow-up session (2026-04-28, forward plan)

### 9. JWT secret decoupled from user password — `internal/op/setting.go`, `internal/server/auth/auth.go`

**Problem**: JWT secret was `username + ":" + password`. A leaked JWT
signing key meant the password was implicitly compromised, and changing
the password silently invalidated every issued token (no rotation handle).

**Fix**:
- New `SettingKeyJWTSecret`, lazily seeded with 32 bytes from `crypto/rand`
  on first cache refresh and stored in the settings table.
- `op.JWTSecret()` returns the bytes; `op.RotateJWTSecret()` regenerates
  on demand.
- `auth.GenerateJWTToken` and `auth.VerifyJWTToken` now read from the
  setting; password change does not invalidate tokens.
- `POST /api/v1/user/rotate-jwt-secret` rotates the secret, invalidating
  all outstanding tokens (user must log in again afterwards).
- `SettingList` filters out `IsInternal()` keys so the secret is never
  returned via the public settings API.

### 10. Periodic GC for sticky-session and circuit-breaker maps — `internal/relay/balancer/{session,circuit}.go`, `internal/task/init.go`

**Problem**: `globalSession` and `globalBreaker` `sync.Map`s were never
swept; long-running deployments grew unbounded as keys, channels, and
models were deleted.

**Fix**: New `balancer.GCSticky(maxAge)` and `balancer.GCCircuit(idleAfter)`
helpers, registered as `TaskBalancerGC` running every 5 minutes:
- Sticky: evict entries older than 24h.
- Circuit: evict only entries currently in `Closed` with zero failures
  and idle ≥ 24h.

### 11. Lowercase model identifiers end-to-end — `internal/helper/fetch.go`, `internal/task/sync.go`, `internal/db/migrate/003.go`

**Problem**: Upstream APIs return inconsistent case (`GPT-4` vs `gpt-4`).
The sync diff was case-sensitive, so each sync flagged unchanged models
as both deleted and added, then thrashed `GroupItem` rows.

**Fix**:
- `helper.FetchModels` lowercases all model names at the channel boundary.
- `task.SyncModelsTask` lowercases `oldModels` defensively to cover legacy
  mixed-case rows that pre-date the migration.
- `migrate/003.go` lowercases `channels.model`, `channels.custom_model`,
  `group_items.model_name`, and `llm_infos.name`. Conflict-aware dedupe
  uses window functions (SQLite ≥ 3.25, MySQL 8+, PostgreSQL).

### 12. Parameterized batch UPDATE in `op.GroupUpdate` — `internal/op/group.go`

**Problem**: `CASE id WHEN ... THEN ...` SQL built with `fmt.Sprintf` and
fed to GORM as `gorm.Expr`. Currently safe (integers only) but fragile.

**Fix**: Replaced with parameterized per-row `Updates(map[string]any)`
inside the existing transaction. Kept the `id = ? AND group_id = ?`
guard so a malicious request still cannot reach across groups. Removed
the now-unused `gorm.io/gorm` import.

### 13. Sticky session honours `(channel, key)` — `internal/relay/balancer/iterator.go`, `internal/model/channel.go`, `internal/relay/relay.go`

**Problem**: Sticky promotion only matched on `ChannelID`. For multi-key
channels, the key chosen by `Channel.GetChannelKey()` was based on cost,
not the key used in the previous request, so a sticky session could
silently jump to a different credential.

**Fix**:
- `Iterator` now records the sticky `KeyID` and exposes `StickyKeyID()`.
- New `Channel.GetChannelKeyByID(preferred)` returns the preferred key
  when still healthy (enabled, not in 429 cooldown), else falls back to
  the existing cost-based selection.
- The relay loop passes the iterator's sticky key id when fetching the
  channel key.

### 14. `/v1/rerank` route — `internal/transformer/{model,inbound,outbound}/...`, `internal/server/handlers/relay.go`, `internal/relay/relay.go`

**Problem**: The `rerank` group existed but had no inbound route, so RAG
pipelines had to bypass the gateway.

**Fix**:
- New `RerankInput`/`RerankResult` types in the internal model with
  validation that enforces mutual exclusion against chat/embedding.
- `inbound.openai.RerankInbound` parses the Cohere/Jina/Voyage shape
  (`{model, query, documents, top_n?, return_documents?}`) plus
  arbitrary passthrough knobs (e.g. Cohere `rank_fields`).
- `outbound.openai.RerankOutbound` POSTs to `<base>/rerank`.
- New `OutboundTypeOpenAIRerank`, `RerankChannelTypes` map, and
  `IsRerankChannelType` predicate. Relay compatibility check uses it.
- Route wired at `POST /v1/rerank`.

### 15. Channel health-check probe — `internal/task/healthcheck.go`, `internal/task/init.go`, `internal/server/handlers/setting.go`, `internal/model/setting.go`

**Problem**: NEW/FLAKY channels could only graduate by serving real
traffic. Channels with low organic traffic stayed un-classified.

**Fix**: `ChannelProbeTask` runs every `SettingKeyChannelProbeInterval`
minutes (default 30, set to 0 to disable). For each enabled, non-ALIVE
channel it sends one minimal request through the matching outbound
transformer (chat: 1-token "ping"; embedding: "ping" input; rerank:
"ping"/"pong" pair) and feeds the success/failure into the same
`StatsChannel` counters that `scripts/auditChannels.py` reads, so the
next audit run promotes or demotes the channel automatically.

### 16. Dead variable `newLLMNames` — `internal/helper/price.go`

Removed unused slice that was built and appended to but never read.

## Identified but not fixed

### A. Per-channel probe model selection

Probe currently picks the first model in `channel.Model`. For mixed
channels (one channel exposing chat + rerank models, say) this could
probe a model the channel type can't serve. Cleanest fix is to filter
the model list by what the channel type supports, but that requires a
per-model capability registry that doesn't exist yet.

### B. JWT rotation UI

Backend endpoint exists (`POST /api/v1/user/rotate-jwt-secret`) but
`web/src` does not yet expose it. Operator must call via curl for now.
