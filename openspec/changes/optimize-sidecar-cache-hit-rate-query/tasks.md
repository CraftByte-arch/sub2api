## 1. Bulk cache-stat data path

- [x] 1.1 Add a keyed, cached, read-only bulk today-cache-stat query to the existing sidecar database service.
- [x] 1.2 Return zero-valued projections for requested accounts without rows and retain the last good result through transient query failure.
- [x] 1.3 Remove unconditional per-account cache enrichment from the base Sub2API today-stats client path.

## 2. Overview integration and compatibility

- [x] 2.1 Attach the direct bulk cache projections to the existing overview response without changing its nested usage contract.
- [x] 2.2 Preserve the bounded legacy HTTP enrichment only when the direct database facility is explicitly unconfigured; never use it after a configured database failure.

## 3. Verification and release

- [x] 3.1 Add focused database-service, overview, and core-client tests covering the no-N+1 path, zero data, cache reuse, stale fallback, and unconfigured compatibility path.
- [ ] 3.2 Run formatting, sidecar tests, static checks, and a local container build.
- [ ] 3.3 Build and deploy only the optimized `account-auto-scheduler` image to HC2, then verify health, overview output, and unchanged Sub2API container state.
