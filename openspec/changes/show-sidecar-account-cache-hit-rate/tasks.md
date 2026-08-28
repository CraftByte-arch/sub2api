## 1. Sidecar data projection

- [x] 1.1 Add an optional cache-stat projection to the sidecar today-usage model
- [x] 1.2 Fetch and aggregate one-day account model statistics with bounded concurrency and a short success cache
- [x] 1.3 Keep per-account cache-stat failures non-fatal while preserving base today usage

## 2. Administrator interface

- [x] 2.1 Render cache hit rate and explanatory token values in the existing compact usage summary
- [x] 2.2 Render a clear unavailable state without changing the account table structure

## 3. Verification and release

- [x] 3.1 Add focused client, overview-contract, and static-UI tests
- [ ] 3.2 Run formatting, sidecar tests, static checks, and responsive browser validation
- [ ] 3.3 Build and deploy only the sidecar service to HC2, then verify health and the live overview payload
