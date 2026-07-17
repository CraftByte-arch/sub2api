# TTFT Monitor Redesign

## Goal

Redesign `/admin/ttft` from the current form-first, zero-heavy page into a
compact policy-and-recovery dashboard. It must make the first question clear:
whether the first-token timeout policy is enabled and whether retry/failover is
recovering affected requests.

The page follows the existing application light and dark themes and the visual
language established by the account-performance dashboard: restrained surfaces,
8px-or-smaller radii, semantic status colour, tabular figures, and clear empty
states. It does not introduce a page-wide gradient or a separate theme.

## Scope

### Included

- Compress the timeout policy controls into a compact top control bar.
- Combine time range, protocol, model, and refresh into one responsive filter
  toolbar.
- Replace generic summary cards with a policy/recovery hierarchy.
- Add a compact recovery funnel derived from existing TTFT summary metrics.
- Split the current four-line chart into recovery and residual-failure trends.
- Show failure distribution only when it contains failures.
- Hide account-local filters and the account table when no account samples
  exist; show a meaningful no-sample state instead.
- Preserve all existing API calls, routing, settings save behavior, filters,
  pagination, and data definitions.

### Excluded

- Backend, schema, recorder, retry, or timeout-policy behavior changes.
- New admin controls, alerting, export, or account drill-down APIs.
- Changing the account-performance dashboard.

## Information Architecture

```text
Page title + policy control bar
  enabled/disabled status | timeout seconds | save | effective policy | loaded time

Filter toolbar
  range segments | protocol | model | refresh

No samples
  one clear status panel explaining that metrics accumulate after policy activity

With samples
  recovery funnel: controlled -> attempt timeout -> recovered -> final TTFT failure
  compact metric cards: controlled requests | attempt timeout | recovery |
                        final TTFT failure | other final failure
  recovery trend | residual failure trend
  other failure distribution (only when non-empty)
  account statistics toolbar and table (only when account samples exist)
```

## Visual Rules

- The policy control bar is a single compact framed tool surface, not a large
  background banner. It has a visible text status in addition to its colour.
- Metrics use a large primary number, a precise numerator/denominator, and a
  short context line. They never render `0.0%` cards when the complete period
  has no controlled requests.
- The funnel is a horizontal, CSS-based four-stage visual. It uses labels and
  counts, so colour is supplementary rather than the sole encoding.
- Recovery trend shows only attempt timeout and recovery. Residual-failure
  trend shows final TTFT failure and other final failure. Charts preserve
  height while loading and use an explicit empty state instead of axes with a
  flat zero line.
- The right-side failure distribution is omitted when it has no rows; the
  trend section expands to the available chart rather than leaving a large
  empty panel.
- Account-local filters are rendered directly above the table only once account
  data exists. They do not occupy the initial no-sample viewport.
- Controls have visible labels, focus states, 44px touch targets where icon
  only, and light/dark contrast consistent with the rest of admin UI.

## Data and State Rules

- The policy bar continues to use `settings.saved` for editable fields and
  `settings.effective` for the current effective status.
- `controlled_requests === 0` is the no-sample condition. It does not imply a
  0% success/failure rate.
- Funnel values are taken from existing summary fields: controlled requests,
  attempt TTFT timeout rate numerator, recovery rate numerator, and final TTFT
  failure rate numerator. Values are displayed with their existing metric
  labels; the UI does not invent a new recovery definition.
- Existing separate overview/accounts error and retry behavior remains. A
  failure to load account data does not hide a successfully loaded overview.

## Verification

- Update TTFT component and page tests for compact policy controls, no-sample
  behavior, funnel values, conditional chart/distribution layout, and account
  filter visibility.
- Run targeted TTFT Vitest tests, frontend type checking, and production build.
- Inspect the page with controlled zero and nonzero fixtures in both themes at
  desktop and mobile widths before release.
