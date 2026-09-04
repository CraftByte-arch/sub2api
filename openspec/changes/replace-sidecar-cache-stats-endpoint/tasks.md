## 1. Cache-hit-rate source

- [x] 1.1 Replace the per-account account-statistics request with the current-day administrator usage-statistics request and map its aggregate token fields to the existing cache projection.
- [x] 1.2 Align the projection's denominator with the Sub2API usage-record cache-hit formula.

## 2. Request containment

- [x] 2.1 Coalesce concurrent uncached reads for the same account and retain the existing five-minute successful snapshot lifetime.
- [x] 2.2 Add a bounded per-account failure backoff that preserves base overview data and prevents poll-driven retry storms.

## 3. Verification and release

- [x] 3.1 Add focused client tests for source path/query, formula, idle accounts, concurrent reads, and failed-read backoff.
- [ ] 3.2 Run focused tests and static checks, build the sidecar-only image, deploy it to HC2, and verify the main Sub2API container is unchanged.
