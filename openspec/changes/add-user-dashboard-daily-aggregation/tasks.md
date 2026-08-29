## 1. Database aggregation foundation

- [x] 1.1 Add the user Dashboard daily route aggregate and state tables with supporting indexes
- [x] 1.2 Add statement-level INSERT, DELETE, and UPDATE triggers that maintain aggregate deltas

## 2. Independent aggregation store

- [x] 2.1 Implement ready-gated aggregate reads that preserve the existing Dashboard response contract
- [x] 2.2 Implement resumable daily backfill, verification, final cutover, and retention reconciliation

## 3. Minimal integration

- [x] 3.1 Initialize and start the independent store from the production usage-log repository
- [x] 3.2 Add the aggregate fast path while preserving the existing Dashboard query as the legacy fallback

## 4. Verification

- [x] 4.1 Add migration and store tests covering trigger structure, fallback, aggregate reads, and weighted duration
- [x] 4.2 Run targeted repository tests, formatting, and OpenSpec validation
