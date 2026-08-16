## 1. Balance Projection

- [x] 1.1 Add a sidecar-only administrator-balance overview model and derive managed, unlimited, remaining, and exhausted states without exposing private account metadata
- [x] 1.2 Add overview tests for managed exhaustion, ordinary quota exhaustion, remaining balance, and unconfigured quota

## 2. Probe Failure Classification

- [x] 2.1 Preserve direct-probe HTTP status in a typed error and add conservative HTTP 403 insufficient-balance phrase classification
- [x] 2.2 Add optional check/latest failure-kind fields, persist and clear them in the engine, and keep the existing scheduler threshold behavior unchanged
- [x] 2.3 Add core, engine, model, and store tests for matching, non-matching, successful clearing, threshold handling, and version-4 JSON reload

## 3. Account List UI

- [x] 3.1 Replace the low-emphasis administrator quota summary with a prominent responsive balance status block while retaining quota details
- [x] 3.2 Display `余额不足导致检测失败` for the structured latest failure and preserve the original error in account/history detail text
- [x] 3.3 Add static UI assertions for danger text, semantic classes, and 375px responsive behavior

## 4. Verification

- [x] 4.1 Run Go unit tests, focused race tests, vet, JavaScript syntax validation, strict OpenSpec validation, and whitespace checks
- [x] 4.2 Review the final diff to confirm that only the sidecar and this OpenSpec change were modified
