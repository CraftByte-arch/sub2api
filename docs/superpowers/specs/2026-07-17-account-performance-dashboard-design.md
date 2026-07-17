# Account Performance Dashboard Design

## Goal

Turn `/admin/performance` from a summary-and-table page into an operational
performance dashboard. An administrator must be able to scan overall health,
spot a regression in time, identify the affected account, and inspect that
account's failure mix without leaving the page.

The dashboard follows the application theme. Light mode takes visual cues from
the supplied reference: calm white surfaces, modest depth, large tabular
figures, compact coloured icons, and small area trends. Dark mode uses the
existing `dark-*` surfaces and preserves the same hierarchy and contrast.

## Scope

### Included

- A responsive dashboard header with range control, labelled platform filter,
  refresh action, collection state, and data coverage.
- Two large overview cards (request health and latency) and three compact
  supporting cards (TTFT timeout rate, failover rate, and sample count).
- Line/area charts for availability/failure rate and latency trends.
- A horizontal failure-outcome distribution chart.
- A sortable risk-account table with status badges, loading/error/empty states,
  and keyboard-accessible row selection.
- An account investigation drawer that loads the existing investigation
  endpoint with the selected account ID and global filters.
- Vue API types for all existing response fields used by the dashboard.

### Excluded

- Schema, aggregation, retention, or gateway request-path changes.
- New backend endpoints. The existing overview, accounts, investigation, and
  health routes provide all data required by the page.
- Cross-account alerting, configurable thresholds, CSV export, and persisted
  dashboard preferences.

## Information Architecture

```text
Page header
  title + collection status + coverage timestamp
  range segmented control + labelled platform filter + refresh

Overview
  request health (attempts, availability, success/failure counts, sparkline)
  latency performance (P50/P95 TTFT, P95 duration, sparkline)
  TTFT timeout rate | failover rate | sampled successful requests

Trends
  availability and failure rate                  latency (P50/P95 TTFT, P95 duration)
  failure outcomes                               data collection state

Risk accounts
  account/platform | health badge | availability | failure rate | P95 TTFT |
  P95 duration | samples

Account investigation drawer
  selected account context | trend | failure distribution | close
```

On widths below `lg`, cards become a single vertical flow and charts stack.
The table retains a deliberate horizontal scroll area below the fold rather
than compressing numeric columns until they are unreadable.

## Data Flow

Initial load and an explicit refresh fetch overview and account rankings in
parallel. The overview request supplies summary counters, time points, health,
and coverage. The account request is sorted by ascending health score and
provides the risk list.

The client derives time-point availability, failure rate, P50/P95 TTFT, and
P95 duration from each `counters` object using the stable histogram contract.
This avoids changing the backend's additive aggregate API.

Selecting an account opens the drawer and fetches `GET
/admin/performance/investigation` with the global filters plus `account_id`.
Changing range or platform closes any active investigation and reloads the
overview and account ranking. Stale responses are ignored using request
generation counters.

## Visual and Interaction Rules

- Keep page sections unframed; individual metric cards, charts, the table, and
  the drawer are the only framed surfaces.
- Use 8px card radius, 4px/8px spacing increments, visible keyboard focus,
  tabular numerals, and semantic status colours paired with text.
- Metric cards use restrained, semantic tinted accents only. No global
  gradient, decorative glow, or competing colour fields.
- Each chart has a heading, text summary for screen readers, keyboard-reachable
  tooltip content through Chart.js interaction, accessible light/dark axes,
  and a meaningful empty state.
- Loading preserves each card/chart's final height. Errors state the failed
  data set and include a retry action. The empty state explains that samples
  begin accumulating after deployment.
- Account rows are buttons semantically and visually, use a minimum 44px hit
  area, and can be opened by mouse, Enter, or Space. The drawer has an
  accessible title and a visible close control.

## Metric Definitions

- Availability: successful attempts divided by non-client-cancelled attempts.
- Failure rate: non-client-cancelled, non-success attempts divided by
  non-client-cancelled attempts.
- TTFT timeout rate: TTFT timeouts divided by non-client-cancelled attempts.
- Failover rate: failovers divided by all attempts.
- P50/P95 values: bucket-upper-bound percentiles from successful latency
  samples; no sample is rendered as `--`, never as zero milliseconds.
- Low sample status: use the backend's `low_sample` boolean for accounts. The
  dashboard additionally displays the raw sample count.

## Component Boundaries

- `AccountPerformanceView.vue`: filters, request lifecycle, selection, and
  layout orchestration.
- `components/PerformanceMetricCard.vue`: one metric visual language, optional
  sparkline, and accessible summary.
- `components/PerformanceTrendChart.vue`: reusable two-to-three series line/
  area chart with theme-aware options.
- `components/PerformanceFailureDistribution.vue`: failure outcome bar chart.
- `components/PerformanceAccountTable.vue`: table rendering, sorting,
  pagination, and row selection.
- `components/PerformanceInvestigationDrawer.vue`: selected-account detail and
  its independent load/error/empty states.
- `performance.ts`: response types, counter-to-metric helpers, and API calls.

## Verification

- Unit tests cover counter-derived ratios and percentiles, API request filters,
  risk badge mapping, and drawer lifecycle.
- Component tests cover loading, error retry, empty data, sorting, and opening
  the selected account investigation.
- Run the frontend type check and relevant Vitest suite.
- Inspect the page in light and dark mode at 375px, 768px, and 1440px. Verify
  no overlapping content, controlled table overflow, contrast, keyboard
  focus, and reduced-motion-safe rendering.
