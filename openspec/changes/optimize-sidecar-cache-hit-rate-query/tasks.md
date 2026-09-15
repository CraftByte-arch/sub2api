## 1. Sidecar cache-stat reduction

- [x] 1.1 Remove the unusable direct raw-usage-log query path and retain the existing HTTP-only contract.
- [x] 1.2 Materialize known-zero cache projections for idle accounts and fetch details only for active accounts.
- [x] 1.3 Bound active-account calls to four workers and cache successful projections for five minutes.

## 2. Verification and release

- [x] 2.1 Add focused tests for idle-account skipping, zero projections, bounded active failure, and cache reuse.
- [x] 2.2 Run formatting, sidecar tests, static checks, and a local Linux/amd64 container build.
- [x] 2.3 Deploy only the optimized `account-auto-scheduler` image to HC2, then verify authenticated overview output and unchanged Sub2API container state.
