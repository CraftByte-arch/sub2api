## Context

The sidecar overview currently obtains today's request, token, and cost totals from Sub2API's batch today-stats endpoint. That endpoint intentionally exposes only aggregate totals, while the existing per-account `GET /admin/accounts/:id/stats?days=1` response contains model rows with input, cache-creation, and cache-read token counts. The sidecar page polls its overview, so issuing uncached per-account requests on every poll would add avoidable database and HTTP load.

## Goals / Non-Goals

**Goals:**

- Display today's cache hit rate for each account wherever the shared today-usage renderer appears.
- Use `cache_read / (input + cache_creation + cache_read)` and expose the underlying hit/base token values for an explanatory tooltip.
- Reuse existing Sub2API administrator APIs and keep cache enrichment best-effort.
- Bound enrichment concurrency and cache successful observations briefly so regular overview refreshes remain inexpensive.
- Preserve the existing compact visual hierarchy and responsive layout.

**Non-Goals:**

- Changing Sub2API production code, database tables, or billing behavior.
- Adding historical cache-rate charts or changing the existing total-token calculation.
- Treating a cache-stat enrichment failure as an account-health failure.

## Decisions

### Store cache details as an optional nested sidecar view

`model.WindowStats` gains an optional `cache` object containing input, cache-creation, cache-read, prompt-base tokens, and hit rate. A missing object means enrichment is unavailable; a present object with a zero denominator represents a known 0.0% result.

### Enrich with the existing per-account stats endpoint

After the existing batch today-stats request succeeds, the sidecar client fetches `GET /admin/accounts/:id/stats?days=1` for each normalized account ID and aggregates the model rows. Requests run with a small fixed concurrency limit. Individual enrichment failures are ignored so requests, total tokens, and cost continue to render.

### Cache only successful enrichment results

The client retains successful cache-stat observations for 60 seconds. Failed requests do not poison the cache, allowing the next overview refresh to recover.

### Keep the account row compact

The existing second usage line shows today's cost followed by `缓存命中 N%`. The token numerator and denominator are available in title text. When enrichment is unavailable, it shows `缓存命中 —`.

## Risks / Trade-offs

- **Additional administrator API calls on a cold cache** → Limit concurrency, cache successful results for 60 seconds, and keep failures non-fatal.
- **Large account fleets take longer to enrich** → Execute enrichment in parallel and bound enrichment to 10 seconds.
- **Sub2API response shape changes** → Decode only stable model token fields; missing data yields an unavailable projection.

## Migration Plan

Build and test the sidecar image, deploy only the sidecar container on HC2, then verify its health endpoint and overview payload. Rollback consists of switching the sidecar container back to the prior image.

## Open Questions

None.
