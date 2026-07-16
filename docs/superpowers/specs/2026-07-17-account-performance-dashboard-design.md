# Account Performance Dashboard Design

## Goal

Provide administrators with a dedicated account-performance dashboard for
daily health checks and incident investigation. It must measure account
quality independently from user billing and usage reporting.

The dashboard answers three questions:

1. Is the account pool healthy now and over the selected period?
2. Which accounts are degrading, with enough evidence to prioritize them?
3. When a degradation occurs, is it caused by first-token latency, upstream
   failures, rate limits, authentication, or account capacity?

## Scope

Add an administrator navigation entry named Account Performance with three
views:

| View | Primary use | Contents |
| --- | --- | --- |
| Performance Overview | Daily inspection | Health KPIs, trends, failure composition, capacity summary, attention-needed accounts |
| Account Ranking | Compare accounts | Sortable performance table, health score, explicit component metrics, sample size |
| Investigation | Diagnose an incident | Timeline, failure composition, latency trends, and account/model/group context |

Existing `/admin/ttft` remains the configuration and specialist analysis page
for first-token timeout policy. The new dashboard consumes TTFT as one metric;
it does not replace or relocate TTFT configuration.

Out of scope for this change:

- automatic account suspension, rate limiting, or scheduler policy changes;
- alert delivery and escalation rules;
- retroactively inventing failure data before performance collection exists.

## Data Model

### Collection

Every completed upstream account attempt emits a best-effort performance delta.
The request path must never wait for persistence. The delta contains:

- account ID, platform, group ID, model, protocol, and bucket timestamp;
- outcome: success, TTFT timeout, rate limit, authentication, upstream 4xx,
  upstream 5xx, network, protocol, other failure, or client cancellation;
- first-token latency, when applicable;
- end-to-end upstream duration;
- whether the attempt was selected through failover.

Client cancellation is counted separately and is excluded from availability and
failure-rate denominators.

### Aggregates

The recorder batches deltas into an account performance minute aggregate. Each
row is keyed by minute plus the dashboard dimensions required for filtering:
account, platform, group, model, and protocol. It stores counters, sums, and
fixed latency histogram buckets for TTFT and total duration.

Hourly aggregates are derived from minute aggregates. Retention is:

- minute aggregates: 7 days;
- hourly aggregates: 90 days.

Fixed buckets make P50, P95, and P99 queryable after aggregation while keeping
storage bounded. The API documents them as bucket-based percentiles.

Current account capacity is not persisted in these aggregates. It is read from
the live account status, rate-limit/overload state, and scheduler snapshot at
query time.

### Completeness

The recorder exposes queue backlog, dropped-delta count, last successful
flush, and degraded status. All dashboard responses include collection health.
When collection is degraded, the UI marks metrics as potentially incomplete.

The system may backfill successful request duration and TTFT from existing
usage logs where available. It must label that data as partial because historic
failure attempts are unavailable.

## Metric Definitions

| Metric | Definition |
| --- | --- |
| Availability | Successful upstream attempts divided by completed attempts, excluding client cancellation |
| Failure rate | Non-success completed attempts divided by completed attempts, excluding client cancellation |
| P50/P95/P99 TTFT | Bucket-based percentile of eligible streaming attempts with a successfully committed first token; TTFT timeouts remain a separate failure metric |
| P50/P95/P99 total latency | Bucket-based percentile of completed successful attempts |
| Health score | Ranking-only score composed of availability, latency, failure rate, and sample size; every component remains visible |
| Attention-needed account | Low health score, breach of a displayed threshold, or worsening trend with sufficient samples |

Low-sample accounts display an explicit insufficient-sample state and are not
ranked as high risk solely from a small number of attempts.

## API Shape

All APIs are administrator-only and share filters for time range, platform,
group, model, protocol, and account. Queries enforce bounded ranges, an
allowlisted sort field, and bounded page size.

- `GET /api/v1/admin/performance/overview`
- `GET /api/v1/admin/performance/accounts`
- `GET /api/v1/admin/performance/investigation`
- `GET /api/v1/admin/performance/health`

The overview returns the five primary KPIs, trend series, failure composition,
capacity summary, attention-needed accounts, data coverage, and collection
health. Account and investigation routes return the selected dimensions and
coverage metadata so the frontend never mixes data sets silently.

## UI Behavior

Performance Overview defaults to the previous 24 hours. It offers shared
filters for platform, group, and model, then presents:

1. availability, failure rate, P95 TTFT, P95 total latency, and currently
   available account count;
2. a request/latency/failure trend;
3. an attention-needed account list;
4. failure composition and current capacity/scheduler status.

Selecting an account or a trend anomaly opens the relevant downstream view
with the same filters and time context. The Account Ranking table supports
sorting by health score or any explicit metric. Investigation presents the
anomaly timeline, failure breakdown, latency trend, and affected model/group
context.

The dashboard is observational only. It has no action that alters account
status or traffic routing.

## Implementation Boundaries

Keep the following responsibilities isolated:

- `PerformanceRecorder`: classify attempts, buffer deltas, expose health;
- `PerformanceRepository`: persist aggregates and execute bounded rollup
  queries;
- `PerformanceService`: apply metric definitions, percentile calculation,
  coverage, and ranking;
- `PerformanceHandler`: validate request filters and map response DTOs;
- frontend API and views: preserve filters when drilling down.

Existing TTFT recorder and Ops data should be reused where their metrics match
the definitions, but the performance dashboard owns its unified public API and
must not directly couple to TTFT page DTOs.

## Error Handling

- Failed or saturated recorder writes increment drop/degradation metadata and
  never fail an API request.
- Invalid ranges, sort fields, dimensions, and page sizes return validation
  errors before a database query.
- Empty periods return zero/empty metrics with coverage metadata, not a
  fabricated baseline.
- Live capacity lookup failure returns historical performance plus a clearly
  unavailable capacity segment.

## Validation

Add focused coverage for:

- outcome classification and client-cancellation exclusion;
- recorder batching, overflow, and health reporting;
- minute-to-hour aggregation and bucket percentile calculation;
- filter, sort, pagination, and retention-bound queries;
- low-sample ranking and partial-history labels;
- admin authorization and invalid request handling;
- overview-to-ranking and overview-to-investigation filter propagation;
- no-data and recorder-degraded frontend states.
