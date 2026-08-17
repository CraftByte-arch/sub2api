## Context

The upstream list is assembled from two sources on every refresh: derived candidates from current local API Key accounts and persisted sidecar records containing identities, encrypted credentials, recharge rates, and remote-Key snapshots. Persisted records intentionally survive when their last local account disappears, which produces the existing empty-band message “没有使用该地址的本地 API Key 账号”.

`combinedRecords` already guarantees the desired recovery behavior: a deleted persisted record is absent while no local account uses its normalized API address, but a later matching local API Key account creates a fresh unpersisted candidate with the same stable upstream ID. The new feature only needs to expose a guarded deletion operation and its administrator interaction; it does not need tombstones or new state.

## Goals / Non-Goals

**Goals:**

- Allow an administrator to remove obsolete persisted upstream state only when the API address currently has zero local API Key accounts.
- Revalidate eligibility on the server at click time instead of trusting a stale browser snapshot.
- Remove the complete persisted sidecar record, including identities, encrypted login material, rate configuration, balance snapshots, remote-Key snapshots, and bindings.
- Preserve automatic candidate rediscovery if a local API Key account later uses the same normalized address.
- Present a clear destructive action with second confirmation, busy state, keyboard focus management, and success/error feedback.

**Non-Goals:**

- Deleting, disabling, editing, or unbinding any Sub2API account.
- Retaining deleted sidecar credentials or settings for automatic restoration.
- Allowing deletion while a current local API Key account uses the address.
- Adding a tombstone, ignored-address list, or state schema version.

## Decisions

### 1. Add a guarded manager operation over the existing store deletion

`Manager.Delete(ctx, upstreamID)` first loads the persisted record, then calls the existing local-account listing path and compares normalized API roots. If any current API Key account matches, it returns a sanitized `UPSTREAM_IN_USE` conflict and leaves the record untouched. Only an eligible persisted record reaches `Store.DeleteUpstream`.

This check is intentionally inside the manager even though the UI hides the button. Browser data can be stale, and direct API clients must receive the same protection.

**Alternative considered:** delete solely from `local_accounts.length === 0` supplied by the client. This creates a time-of-check/time-of-use authorization gap and is rejected.

### 2. Reuse derived candidates for automatic reappearance

Deletion creates no tombstone. With zero matching accounts, the persisted record disappears from the next list response. If a matching local API Key account is later added or moved to the address, `combinedRecords` derives a new candidate using the existing stable ID and default metadata. Deleted identities, credentials, rates, and snapshots do not return.

**Alternative considered:** retain a hidden disabled record. This would keep sensitive and obsolete state and complicate the existing merge rules, so it is rejected.

### 3. Expose one administrator-only DELETE route

`DELETE /api/upstreams/{upstreamID}` is registered through the existing administrator middleware and returns `204 No Content` on success. Missing records return the existing `UPSTREAM_NOT_FOUND`; an address that is in use returns HTTP 409 with `UPSTREAM_IN_USE`. Failures to retrieve current accounts fail closed and do not delete.

### 4. Show the action only in the exact empty-local-account state

The row renders a visible danger-styled “删除上游” button only when `upstream.persisted === true` and `upstream.local_accounts.length === 0`, the same condition that renders the empty-band message. The button uses the existing trash SVG, visible text, focus state, and responsive button sizing.

Clicking opens a small confirmation dialog naming the upstream and explaining all sidecar data that will be permanently removed. The destructive confirmation is focused when opened, disabled with a “删除中” label during the request, and cannot be double-submitted. Closing restores focus when the row still exists; successful deletion reloads the list and shows a toast. A conflict also reloads the list so a newly appeared local account is immediately visible.

The UI follows the existing dashboard's semantic colors, modal structure, and interaction patterns rather than introducing a new component style.

## Risks / Trade-offs

- **[A local account appears between the server check and state deletion]** → The subsequent list immediately derives the candidate again from that account; no Sub2API account is changed. If it appears before the check, deletion is rejected with 409.
- **[Deleting an unused record removes credentials the administrator still wants]** → Require explicit second confirmation that lists identities, encrypted login material, rates, and snapshots as permanent losses.
- **[Client snapshot is stale]** → Server re-lists accounts and refuses deletion when in use; the UI reloads after both success and conflict.
- **[Account listing fails]** → Fail closed and keep the persisted record.

## Migration Plan

1. Deploy only the `account-auto-scheduler` image; no state migration is required.
2. Verify manager eligibility, HTTP authorization/conflict behavior, UI visibility, confirmation, and rediscovery tests.
3. Recreate only the HC2 sidecar container with its existing state volume and environment.
4. Roll back to the previous sidecar image if required; state format remains compatible, though upstreams explicitly deleted by an administrator are intentionally not restored by rollback.

## Open Questions

None. The current list merge and stable-ID behavior already provide the required automatic reappearance semantics.
