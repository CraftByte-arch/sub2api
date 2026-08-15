## Context

`account-auto-scheduler` already authenticates multiple identities per upstream, encrypts reusable session material, and periodically synchronizes key metadata through family-specific Sub2API and NewAPI adapters. The Upstreams tab currently exposes only key-level quota/use. Both supported families return the authenticated user's account balance from an existing profile endpoint, but the adapters discard that field.

One upstream can contain multiple identities with independent balances, so a balance cannot safely be modeled as one upstream-wide scalar. NewAPI additionally represents balance as an internal quota integer whose USD value depends on the deployment's public `quota_per_unit`; Sub2API returns `balance` directly in USD.

## Goals / Non-Goals

**Goals:**

- Capture the real authenticated account balance for each connected identity during connect and synchronization.
- Normalize monetary values where the upstream provides an authoritative conversion and preserve raw NewAPI quota when it does not.
- Retain the last successful balance across later key/balance synchronization failures and make freshness explicit.
- Present identity balances and a non-ambiguous upstream summary within the existing administrator-only Upstreams tab.
- Keep all upstream traffic server-side and reuse current encrypted identities, synchronization controls, and deployment isolation.
- Keep bound Sub2API account remaining quota aligned with fresh upstream identity balance so native quota exhaustion automatically removes depleted accounts from scheduling.

**Non-Goals:**

- Reading balances without an authenticated upstream identity.
- Summing balances across identities into one spendable pool or converting them with the manually configured recharge multiplier.
- Scraping rendered upstream pages, bypassing CAPTCHA/2FA, or guessing undocumented currency conversions.
- Changing upstream balances, recharge settings, keys, Sub2API source, or the Sub2API database.
- Sharing or dividing one identity balance across multiple bound local accounts; each account receives the same full-site balance projection.

## Decisions

### 1. Persist one balance snapshot per identity

`UpstreamIdentity` receives an optional balance snapshot containing a finite amount, display currency/unit, source family, observation time, raw quota/conversion metadata when applicable, and a stale flag. The public view copies only these non-secret fields.

This is preferred over an upstream-level field because multiple logins can exist for one site. It also lets a failed or expired identity retain its own last-known balance without affecting other identities.

### 2. Return balance through adapter results already used by connect and sync

The adapter `LoginResult` and `SyncResult` carry an optional normalized balance. Sub2API extends its `/api/v1/auth/me` user shape with `balance`. NewAPI extends `/api/user/self` with `quota` and reads anonymous `/api/status` for `quota_per_unit` during connect/sync.

No balance-only browser route is required: the existing login and identity/upstream sync actions become authoritative refresh actions. This keeps upstream calls out of overview polling and avoids parallel refresh races.

### 3. Use family-specific, conservative normalization

Sub2API `balance` is stored as USD because the existing API contract defines user balance and API-key quota in USD. NewAPI quota is divided by a positive finite `quota_per_unit` published by the same origin and stored as USD. If NewAPI omits or returns an invalid conversion, the snapshot stores the raw quota as unit `quota` and the UI labels it as raw upstream quota rather than money.

The manual recharge rate is not used for this conversion. Recharge rate describes the administrator's acquisition cost, while upstream account balance describes remaining upstream credit; combining them would change meaning and could double-convert a displayed value.

### 4. Preserve last-good values and mark freshness

Successful connect/sync replaces the identity balance and clears stale state. A failed synchronization keeps the previous snapshot and marks it stale while updating the identity's normal failure fields. A successful key sync whose profile lacks any supported balance field clears a prior snapshot only when the family contract authoritatively indicates the field is unsupported; transient endpoint failures fail the whole identity sync and retain the old value.

This matches existing last-good key snapshot behavior and prevents a temporary upstream error from presenting zero as a real balance.

### 5. Keep the existing dense upstream interface

Each identity row shows `站点余额`, formatted value/unit, last observation time, and stale/unsupported state. The upstream header shows a compact summary: a single balance when exactly one identity has a current snapshot, `N 个账号余额` for multiple identities, or `余额待登录/同步` when unavailable. Expanded identity rows remain the authoritative values and are never summed.

Existing synchronization controls refresh keys and balances together. Semantic text accompanies color, layout remains responsive at 375 CSS pixels, and long values cannot resize action controls.

### 6. Project fresh identity balance into native Sub2API account quota

After connection, manual synchronization, periodic synchronization, or binding changes, the manager resolves each current non-stale remote-key binding to its owning identity balance. Only a fresh finite USD balance and a finite non-negative key group multiplier are eligible. Raw NewAPI quota, stale balances, missing multipliers, stale keys, and ambiguous bindings do not overwrite an existing local quota.

For a positive group multiplier, the local remaining quota is `max(identity_balance_usd, 0) / group_multiplier`. Every bound local account receives the full identity balance independently; the value is not divided by the number of local accounts. Because Sub2API stores total limit and accumulated use separately, the sidecar normally writes `quota_limit = current quota_used + projected remaining` and leaves `quota_used` untouched.

Sub2API interprets `quota_limit = 0` as unlimited. Therefore, when the authoritative projected remaining balance is zero and the account has never accumulated quota use, the sidecar atomically writes a tiny equal positive `quota_limit` and `quota_used` sentinel so `IsQuotaExceeded` is immediately true. A later positive balance sets the limit above the preserved used value and automatically restores eligibility. An exact zero group multiplier represents a non-consuming/unlimited upstream group and maps to `quota_limit = 0`.

The write reuses `POST /api/v1/admin/accounts/bulk-update` with only Extra keys; it never sends `rate_multiplier`, account status, or schedulable state. Sidecar ownership is recorded in Extra so a later explicit unbind or local-upstream move can disable only the quota limit previously managed by this projection. No Sub2API source or database migration is required.

## Risks / Trade-offs

- [Forked upstreams use different balance fields] -> Accept bounded numeric variants explicitly covered by fixtures; show unavailable rather than recursively searching arbitrary JSON.
- [NewAPI publishes a misleading conversion] -> Treat only the same-origin public `quota_per_unit` as authoritative and expose conversion metadata in the public snapshot for auditability.
- [A zero balance is mistaken for missing] -> Model presence with an optional snapshot, not a zero-value sentinel.
- [Synchronization failure leaves an old value visible] -> Mark it stale and show observation time and identity failure status next to it.
- [Multiple identities produce an ambiguous total] -> Never sum; show count in the upstream header and separate values per identity.
- [One upstream is bound to multiple local accounts] -> Apply the full identity balance to each account independently as requested; accept that aggregate local quota intentionally exceeds the single upstream balance because it is a depletion signal, not a reservation ledger.
- [Concurrent usage changes during quota projection] -> Preserve `quota_used` and normally update only `quota_limit`; write the equal positive exhaustion sentinel only for the zero-used/zero-balance edge where no successful upstream spend should be possible.
- [Stale or raw balance corrupts local scheduling] -> Skip quota writes unless the identity balance is fresh, finite, and normalized to USD.

## Migration Plan

1. Back up the sidecar v3 state file and record container IDs/restart counts on hc2.
2. Deploy only `account-auto-scheduler`; existing v3 states load with nil identity balance snapshots.
3. Existing successful periodic or manual identity synchronization populates balances without re-login where credentials remain valid.
4. Verify desktop/mobile Upstreams rendering and representative Sub2API/NewAPI identities.
5. Roll back by replacing only the sidecar image; the added optional JSON fields are ignored by the prior v3 binary.

## Open Questions

None. Additional upstream families or site-specific conversion rules require separate fixtures and explicit normalization decisions.
