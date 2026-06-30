---
comet_change: global-usage-card-available-summary
role: technical-design
canonical_spec: openspec
---

# Global Usage Card Available Summary Design

## Context

The global top bar currently shows the user's long-term account balance from `authStore.user.balance` and renders `UsageCardMini` as a separate usage-card entry. The long-term balance display must keep its existing meaning and behavior.

`UsageCardMini` currently fetches `/usage-cards` and displays `cards.length`, which counts every returned card, including expired, exhausted, suspended, cancelled, and future cards. The backend already has the canonical available-card criteria in `UserUsageCard.IsAvailableAt(now)` and `ListAvailableCards(ctx, userID, now)`: active, undeleted, started, unexpired, and not exhausted.

The API keys page refresh button currently refreshes API key list data and usage stats. It should also refresh the global top-bar usage-card information and the long-term account balance.

## Architecture

Use a backend summary endpoint as the source of truth for top-bar usage-card totals.

```text
API Keys Refresh
      |
      +--> loadApiKeys()                          (existing)
      +--> usageCardSummary.refresh()             (new shared top-bar state)
      +--> authStore.refreshUser()                (existing long-term balance refresh)

AppHeader
      |
      +--> long-term balance: authStore.user.balance
      +--> UsageCardMini: usageCardSummary state

Backend
      |
      +--> GET /usage-cards/summary
              ListAvailableCards(userID, now)
              sum RemainingUSD()
```

## Backend Design

Add a user-facing usage-card summary response:

```json
{
  "available_count": 2,
  "available_remaining_usd": 7.5
}
```

Implementation details:

- Add a small service method, for example `GetMySummary(ctx, userID, now)`, that calls `ListAvailableCards`.
- Compute `available_count` from the number of available cards.
- Compute `available_remaining_usd` by summing each available card's `RemainingUSD()`.
- Register an authenticated route such as `GET /usage-cards/summary` before the existing `GET /usage-cards` route.
- Keep the existing `/usage-cards` list endpoint unchanged.

This keeps the top-bar summary aligned with the same server-side availability rules used for billing and avoids duplicating time/status logic in the frontend.

## Frontend Design

Add a frontend API method and type:

```ts
interface UsageCardSummary {
  available_count: number
  available_remaining_usd: number
}

usageCardsAPI.getSummary()
```

Add shared summary state through a Pinia store or narrowly scoped composable. The store/composable should expose:

- `summary`
- `loading`
- `error`
- `refresh()`

`UsageCardMini` should read this shared state instead of deriving the top-bar badge from `cards.length`.

Top-bar behavior:

- The existing long-term balance display remains unchanged and continues to read `authStore.user.balance`.
- `UsageCardMini` badge displays `summary.available_count`.
- `UsageCardMini` directly displays the usage-card remaining total as `$0.00`.
- Hover details can continue to show card-level data; card-level remaining amounts can keep the existing higher precision.

API keys page refresh behavior:

- Keep `loadApiKeys()` responsible for API key list and usage stats.
- Also trigger `usageCardSummary.refresh()` and `authStore.refreshUser()` from the refresh button path.
- Top-bar refresh failures must not block API key list refresh. Use separate `try/catch` blocks or `Promise.allSettled` for side refreshes.

## Alternatives Considered

### Frontend-only calculation from `/usage-cards`

Rejected. It avoids a backend endpoint but duplicates the availability rules in the frontend, including start time, expiration, status, deletion, and exhaustion checks. That increases drift risk with billing behavior.

### Merge usage-card summary into `/auth/me`

Rejected. It would make `/auth/me` heavier and couple authentication/profile refresh to usage-card billing concepts. Long-term balance already belongs in `/auth/me`; usage-card summary is better as a small usage-card API.

## Error Handling

- If the summary endpoint fails during top-bar initialization, show a reasonable fallback such as `0` or retain the previous summary and record the error in state.
- If summary refresh fails during API key page refresh, the API key list refresh still completes normally.
- If `authStore.refreshUser()` fails during API key page refresh, API key data still refreshes; existing auth-store behavior handles unauthorized responses.

## Testing Strategy

Backend tests:

- Available active, started, unexpired, non-exhausted cards are counted and summed.
- Expired, exhausted, suspended, cancelled, deleted, and future cards are excluded.
- The user route requires authentication and returns the summary shape.

Frontend tests:

- `UsageCardMini` displays available count instead of total card count.
- `UsageCardMini` displays top-bar usage-card remaining total using `$0.00`.
- The existing long-term balance display remains separate from the usage-card total.
- API key page refresh triggers both usage-card summary refresh and `authStore.refreshUser()`.
- Top-bar side-refresh failures do not prevent API key list refresh from succeeding.

## Spec Patch

No OpenSpec delta changes are needed. The current `usage-cards` delta spec already captures separated long-term balance display, top-bar usage-card summary display, and API key refresh linkage.
