## ADDED Requirements

### Requirement: Sidecar reads online presence from PostgreSQL aggregates
The sidecar SHALL determine administrator online-user counts exclusively from existing per-minute PostgreSQL user/group aggregates and their durable watermark, and MUST NOT read raw usage or Ops request details for this feature.

#### Scenario: Aggregate summary succeeds
- **WHEN** the minute aggregate and watermark are available
- **THEN** the sidecar returns the distinct global user count and distinct per-group user counts for the ten aggregate minutes ending at the watermark

#### Scenario: Successful and failed requests are represented
- **WHEN** a user/group aggregate row has a positive success count or a positive recorded-error count
- **THEN** that user is included once globally and once for that group

#### Scenario: Raw fallback is unavailable
- **WHEN** the aggregate query fails or aggregate data is stale
- **THEN** the sidecar MUST NOT call Sub2API raw usage or Ops request-detail endpoints

### Requirement: Online reads are demand-driven and cached
The sidecar SHALL perform no periodic online-user database polling and SHALL cache successful summary and detail snapshots for 60 seconds with concurrent cache-miss coalescing.

#### Scenario: Administrator page is closed
- **WHEN** no administrator requests an online summary or detail payload
- **THEN** the sidecar performs no online-user database query

#### Scenario: Cached summary is requested
- **WHEN** a summary cache entry is younger than 60 seconds
- **THEN** the sidecar returns a deep copy of that entry without querying PostgreSQL

#### Scenario: Concurrent cache misses occur
- **WHEN** multiple administrator requests miss the same online cache concurrently
- **THEN** exactly one PostgreSQL query supplies the shared result

#### Scenario: Detail dialog remains closed
- **WHEN** the browser only renders or polls online counts
- **THEN** it requests the summary endpoint and does not load user identities or today usage

### Requirement: Aggregate freshness is explicit
The sidecar SHALL anchor the online window to the aggregate watermark and expose the watermark, aggregation lag, readiness, stale state, and a non-sensitive notice.

#### Scenario: Normal aggregation lag
- **WHEN** the watermark is no more than three minutes behind current time
- **THEN** the response is ready and the ten-minute window is evaluated relative to that watermark

#### Scenario: Stale aggregation
- **WHEN** the watermark is more than three minutes but no more than ten minutes behind current time
- **THEN** the response retains aggregate counts, marks them stale or partial, and displays the lag

#### Scenario: Unusable aggregation
- **WHEN** the watermark is more than ten minutes behind current time or missing
- **THEN** the response is not ready and MUST NOT present old counts as current online counts

#### Scenario: Transient query failure after success
- **WHEN** a database query fails while a sufficiently recent last-good snapshot exists
- **THEN** the sidecar may return that snapshot marked stale with a generic failure notice

### Requirement: Online detail uses compact daily aggregates
The sidecar SHALL load online-user identities and current-day requests, tokens, and actual cost only for the detail endpoint by joining active aggregate users with the existing daily per-user route aggregate.

#### Scenario: Daily aggregate is ready
- **WHEN** an administrator opens the detail dialog and the daily aggregate readiness state is true
- **THEN** each online user includes username-first display identity, email, minute-precision last activity, and current-day requests, tokens, and actual cost

#### Scenario: Daily aggregate is not ready
- **WHEN** presence data is available but the daily aggregate readiness state is false
- **THEN** users remain visible, today usage fields are null, and the detail response is marked partial

#### Scenario: User is active in multiple groups
- **WHEN** one user has presence rows in multiple groups during the window
- **THEN** the detail response contains one user row whose group membership includes every active group while the global count includes the user once

### Requirement: Database access is optional and least-privilege
The sidecar SHALL use an optional dedicated PostgreSQL login that is read-only, connection-bounded, and authorized only for required aggregate data and non-sensitive user identity columns.

#### Scenario: Database DSN is absent
- **WHEN** the sidecar starts without aggregate database configuration
- **THEN** online data is reported unavailable while scheduling, upstream, notification, authentication, and other administrator features continue operating

#### Scenario: Database is unreachable
- **WHEN** the configured database cannot be reached
- **THEN** sidecar startup and unrelated features remain available and a later online request may retry the connection

#### Scenario: Browser requests online data
- **WHEN** an authenticated administrator requests summary or detail data
- **THEN** database credentials remain server-side and are never included in the browser response

#### Scenario: Unauthenticated request
- **WHEN** a request to either online endpoint lacks a valid administrator session
- **THEN** the sidecar rejects it using the existing administrator authorization behavior before querying PostgreSQL

### Requirement: Browser separates summary and detail loading
The administrator UI SHALL populate site-wide and per-group indicators from a summary endpoint and SHALL request the detailed user list only while opening or refreshing its dialog.

#### Scenario: Initial page load
- **WHEN** the administrator opens the sidecar group page
- **THEN** the browser loads the online summary without waiting for identities or today usage

#### Scenario: Visible-page refresh
- **WHEN** the page performs its online refresh while the detail dialog is closed
- **THEN** it refreshes only the summary at approximately the aggregate refresh cadence

#### Scenario: Detail response is newer
- **WHEN** a detail response has a newer query or watermark time than the displayed summary
- **THEN** the browser may update the displayed global and per-group counts from that response
