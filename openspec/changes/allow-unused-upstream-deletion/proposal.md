## Why

Persisted upstream records remain visible after their last local API Key account is removed, leaving obsolete login identities, rate settings, and snapshots in the sidecar. Administrators need a safe way to remove only these unused records while retaining automatic discovery when a local account later uses the same API address again.

## What Changes

- Add an administrator-only endpoint that deletes a persisted upstream record only when no current local API Key account resolves to that API address.
- Recheck local-account usage at deletion time and reject the operation if the address has become active again.
- Show a destructive “删除上游” action only on persisted upstream rows whose existing local-account band says “没有使用该地址的本地 API Key 账号”.
- Require a second-confirmation dialog that explains saved identities, encrypted login material, Key snapshots, and sidecar-only rate settings will be removed.
- Disable the confirmation action while deleting, show success or failure feedback, and reload the list after the operation.
- Preserve the existing derived-candidate behavior so a future local API Key account with the same normalized API address automatically recreates the list entry without restoring deleted sidecar credentials or settings.

## Capabilities

### New Capabilities

- `unused-upstream-deletion`: Safe deletion eligibility, administrator confirmation, persisted-record cleanup, conflict handling, and automatic rediscovery of a reused API address.

### Modified Capabilities

<!-- No existing main specification covers the sidecar upstream-management lifecycle. -->

## Impact

- Affects only `account-auto-scheduler` manager, administrator HTTP route, upstream-list JavaScript/HTML, and tests.
- Uses the existing state-store `DeleteUpstream` operation and existing local-account discovery; no state schema or Sub2API code changes are required.
- The operation deletes only sidecar state and never deletes or edits a Sub2API account.
