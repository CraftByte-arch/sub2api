## 1. State and normalized domain models

- [x] 1.1 Add backward-compatible group-account protection records, state version loading, cloning, validation, and store regression tests.
- [x] 1.2 Add normalized probe usage, per-check cost, and per-account detection-cost aggregate models with public views and persistence tests.

## 2. Protection reconciliation and Sub2API integration

- [x] 2.1 Extend final-multiplier projections to resolve protected logical members from their persisted upstream identity/key source even after physical unbinding.
- [x] 2.2 Implement serialized protection reconciliation with automatic unbind/rebind transitions, unknown-multiplier preservation, and explicit manual release/remove ordering.
- [x] 2.3 Add client methods for model pricing, physical group mutation, and protection-aware manager reconciliation after startup, sync, periodic refresh, and binding changes.
- [x] 2.4 Add protection state-machine, concurrency, threshold-boundary, and no-rebind-after-removal tests.

## 3. Probe usage and cost accounting

- [x] 3.1 Normalize usage from legacy Sub2API test events and direct OpenAI/Anthropic/Gemini streaming terminal events without exposing upstream credentials.
- [x] 3.2 Resolve model pricing through the existing administrator endpoint and calculate probe cost at multiplier 1.0, preserving token-only results when pricing is unavailable.
- [x] 3.3 Accumulate probe request/token/cost statistics in engine results, preserve them across restart, and remove them with deleted configurations.
- [x] 3.4 Add parser, pricing, engine, and aggregate regression tests for usage-present, usage-missing, pricing-missing, and restart cases.

## 4. Administrator API and merged projections

- [x] 4.1 Add administrator-protected endpoints to set, release, and remove group-account protection/binding relationships with idempotent validation and structured errors.
- [x] 4.2 Merge protected logical members into overview and bulk-binding projections while preserving legacy unprotected behavior.
- [x] 4.3 Add HTTP handler tests for authorization, confirmation-required mutations, partial failures, protected display, and deselection preventing future rebinding.

## 5. Administrator UI

- [x] 5.1 Show protection multiplier beside final multiplier, explicit exceeded/unavailable states, and detection request/token/cost statistics in each account row.
- [x] 5.2 Add protection editor, release-protection confirmation, and remove-binding confirmation with loading, disabled, focus, keyboard escape, success, and error states.
- [x] 5.3 Make binding dialog selection honor logical protected membership and add responsive/accessibility regression coverage at 375px and desktop widths.

## 6. Verification and documentation

- [x] 6.1 Update sidecar documentation and OpenSpec status with the state-machine, no-rebind guarantee, and probe-cost limitations.
- [x] 6.2 Run formatting, all Go tests, race tests, vet, JavaScript syntax checks, strict OpenSpec validation, and final diff review.
