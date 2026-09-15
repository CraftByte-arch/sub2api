## ADDED Requirements

### Requirement: Administrator can see the online user count

The sidecar SHALL expose an administrator-authenticated online user summary whose online window is exactly the ten minutes immediately preceding the query time.

#### Scenario: Users with recent requests are counted once

- **WHEN** multiple successful or recorded failed requests from the same user occur during the ten-minute window
- **THEN** the summary counts that user once and returns the latest request timestamp for that user

#### Scenario: Requests outside the window are not online

- **WHEN** a user's latest recorded request is older than ten minutes at query time
- **THEN** that user is excluded from the online count and user list

#### Scenario: Unauthenticated access is rejected

- **WHEN** a request to the sidecar online-user endpoint does not contain a valid administrator session
- **THEN** the sidecar SHALL return the same unauthorized response used by its existing administrator endpoints

### Requirement: Online user details include today consumption

The sidecar SHALL provide each online user with a stable identifier, a display name or masked fallback, the latest call time, today's actual consumed amount, and today's total token count.

#### Scenario: Identity and consumption are available

- **WHEN** the Sub2API administrator APIs return user identity and batch usage data
- **THEN** the sidecar returns username when present, otherwise email, the latest call time, `today_actual_cost`, and the sum of input, output, cache creation, and cache read tokens

#### Scenario: A user detail lookup fails

- **WHEN** one user's identity lookup fails while other online users are available
- **THEN** the sidecar keeps that user in the result with a non-sensitive fallback label and does not discard other users

#### Scenario: Today's usage lookup fails

- **WHEN** the batch usage endpoint is unavailable but recent request records are readable
- **THEN** the sidecar still returns online users and timestamps, marks today's consumption as unavailable, and reports a retryable partial-data state

### Requirement: Online data is isolated from the existing overview

The sidecar SHALL keep existing group/account overview behavior available when online-user collection fails.

#### Scenario: Online endpoint failure

- **WHEN** the online-user upstream request times out or returns an error
- **THEN** `/api/overview` remains successful and the UI shows an online-data error state with a retry action

#### Scenario: Upstream lacks Ops request details

- **WHEN** the Ops request-details endpoint is unavailable or disabled
- **THEN** the sidecar falls back to the current-day administrator usage endpoint and filters records locally by the ten-minute window

### Requirement: Administrator can see the online count for each group

The sidecar SHALL expose a distinct online-user count for every group using the request-time `group_id` recorded during the same ten-minute online window, while retaining the existing site-wide count.

#### Scenario: Repeated calls in one group count once

- **WHEN** one user makes multiple requests through the same group during the ten-minute window
- **THEN** that group displays the user once

#### Scenario: One user can be online in multiple groups

- **WHEN** one user makes requests through two different groups during the ten-minute window
- **THEN** each group counts that user once, while the site-wide count still counts the user once

#### Scenario: A group has no recent users

- **WHEN** grouping data is available and no request in the ten-minute window used a group
- **THEN** that group explicitly displays an online count of zero

#### Scenario: An ungrouped request is recorded

- **WHEN** a recent request has no group assignment under the current Sub2API request-details contract
- **THEN** the user is counted under the sidecar's synthetic ungrouped group

#### Scenario: Grouping data is partial or unavailable

- **WHEN** the fallback source, pagination limit, or an incompatible response prevents a complete per-group count
- **THEN** the sidecar marks the group counts partial or unavailable instead of presenting an uncertain value as a complete zero

### Requirement: Administrators can inspect the user list in a dialog

The sidecar UI SHALL render the online count as an accessible control and open a dialog containing the current online-user rows.

#### Scenario: Open and refresh the dialog

- **WHEN** an administrator activates the online count control
- **THEN** the sidecar opens a dialog, fetches the latest online-user payload, and displays user, last call time, today's cost, and today's tokens

#### Scenario: Empty online set

- **WHEN** no user has a recorded request in the ten-minute window
- **THEN** the dialog displays an explicit empty state and the count is zero

#### Scenario: Responsive and keyboard use

- **WHEN** the dialog is viewed on a narrow screen or opened with keyboard focus
- **THEN** rows remain readable without horizontal clipping, and the dialog can be closed and retried using keyboard-accessible controls
