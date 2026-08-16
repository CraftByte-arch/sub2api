## Context

The account overview embeds Sub2API account quota fields but deliberately strips the independent billing-probe result and hides `Extra`. The sidecar's upstream-balance synchronization stores authoritative metadata such as managed/exhausted/unlimited/remaining in that hidden `Extra` map, so the browser cannot reliably distinguish a synchronized zero balance from a generic administrator quota. Separately, direct probes convert HTTP failures into plain text errors before the engine classifies a check, which loses the HTTP status needed for a safe insufficient-balance classification.

The change must remain entirely inside `account-auto-scheduler`, keep old state files readable, preserve the existing failure/recovery counters, and fit the existing dense light/dark admin UI down to a 375px viewport.

## Goals / Non-Goals

**Goals:**

- Give the browser a minimal, safe administrator-balance projection with a remaining amount and explicit insufficient state.
- Make remaining balance visually prominent without changing the account table's overall information hierarchy.
- Distinguish an insufficient-balance HTTP 403 from unrelated authentication, policy, or WAF 403 responses.
- Persist the structured latest failure reason and expose it in account state and history while retaining the full sanitized upstream error.
- Preserve all current automatic suspend and recovery behavior.

**Non-Goals:**

- Modifying Sub2API account, billing, quota, or scheduling code.
- Parsing a balance amount from arbitrary error text or changing synchronized quota values.
- Treating every HTTP 403 response as insufficient balance.
- Adding new credentials, network calls, dependencies, filters, or scheduler policy settings.

## Decisions

### Project a sidecar-specific administrator-balance view

The overview account will include a nested `admin_balance` object containing `configured`, `managed`, `unlimited`, `insufficient`, optional `remaining`, and optional exhausted-dimension identifiers. Managed upstream quota metadata takes precedence; otherwise the projection derives remaining/exhausted state from finite positive daily, weekly, and total quota limits already returned by Sub2API.

This keeps `Extra` private and centralizes quota semantics on the server. Computing everything in JavaScript was rejected because the browser cannot see the managed exhausted/unlimited markers and would misread the exhausted sentinel as an ordinary tiny quota.

### Preserve HTTP status in a typed direct-probe error

Direct HTTP failures will use a typed core error carrying the status code and already-sanitized display message. An exported predicate will classify insufficient balance only when the status is 403 and the normalized message contains an allowlist of Chinese or English balance/quota exhaustion phrases.

String matching on all errors was rejected because an unrelated error could mention 403 text without being an HTTP response. Classifying every 403 was rejected because credentials, region policy, WAF, and account permissions also commonly return 403.

### Store a stable failure kind separately from the message

`CheckResult` and `ManagedAccount` will gain an optional `failure_kind`, with `balance_insufficient` as the first defined value. The engine copies the check's kind into `last_failure_kind` for the latest counted unhealthy result and clears it when the latest result is not a counted failure. JSON omission makes the fields backward compatible with version-4 state.

The original message remains unchanged (apart from existing sanitization/truncation), so operators retain the upstream detail and request ID. Replacing the message with a generic label was rejected because it would make diagnosis harder.

### Use a semantic balance card and textual error state

The quota section will lead with a compact balance card. A finite remaining amount uses tabular, higher-weight figures; an exhausted quota uses the existing danger tokens and the literal text `余额不足`, with the exhausted dimension in supporting copy. Existing used/limit details remain below for auditability. The account state will use `余额不足导致检测失败` when `last_failure_kind` is `balance_insufficient`; history tooltips/dialog details continue to include the original message.

The design uses existing spacing, radius, and semantic color tokens. Color is not the sole signal, and the block stacks naturally at the existing mobile breakpoint.

## Risks / Trade-offs

- [Upstream wording changes] The allowlist may miss a new insufficient-balance phrase. → Keep classification conservative, cover common Chinese/English variants in table tests, and leave the original error visible even when unclassified.
- [False positive from prose containing a marker] A 403 body could mention quota terminology in another context. → Require both a real typed HTTP 403 and a direct insufficient/exhausted phrase; do not match request IDs or provider-specific type fields.
- [Stale synchronized quota] The projected administrator balance can only be as fresh as the existing upstream synchronization cycle. → Present the value as administrator balance, not as a new live probe, and do not add network calls to overview rendering.
- [Added state fields] Older binaries ignore the new JSON fields after rollback. → The fields are optional and do not require a state-version migration; rollback retains all core scheduler state even if the classification metadata is discarded.

## Migration Plan

1. Deploy the sidecar binary/static assets normally; no Sub2API deployment or database migration is required.
2. Existing accounts begin with empty `last_failure_kind`; the next probe populates or clears it.
3. Roll back by restoring the previous sidecar image. Version-4 state remains readable because unknown JSON fields are ignored.

## Open Questions

None.
