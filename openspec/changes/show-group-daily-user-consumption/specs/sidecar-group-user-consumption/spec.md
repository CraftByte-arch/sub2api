## ADDED Requirements

### Requirement: Administrator can inspect a group's daily top users

The sidecar SHALL expose an administrator-authenticated view for a real group that returns at most the 20 users with the highest actual consumption for the current day.

#### Scenario: Group users are sorted and limited

- **WHEN** an administrator requests a valid group summary
- **THEN** the sidecar returns no more than 20 users sorted by today's `actual_cost` descending, with deterministic tie ordering

#### Scenario: User rows include consumption fields

- **WHEN** Sub2API returns a user's group breakdown
- **THEN** the sidecar returns the user ID, display name or email, today's actual cost, request count, and total token count

#### Scenario: Invalid or synthetic group is rejected

- **WHEN** a request uses a non-positive or unsupported synthetic group ID
- **THEN** the sidecar returns a client error and does not query Sub2API for an unscoped group

### Requirement: Group consumption uses the current day and actual-cost semantics

The sidecar SHALL query the current calendar day and SHALL use Sub2API's actual-cost field without applying a second sidecar multiplier.

#### Scenario: Current-day range is sent upstream

- **WHEN** the sidecar queries a group on August 29, 2026
- **THEN** it sends August 29, 2026 as both the start and end date of the administrator breakdown query

#### Scenario: Missing upstream fields are explicit

- **WHEN** a returned row lacks a cost, request count, or token field
- **THEN** the sidecar preserves the row, marks the response partial, and exposes the missing metric as unavailable rather than guessing zero

### Requirement: Group consumption is isolated behind administrator authentication

The sidecar SHALL protect the group consumption endpoint with the same administrator session middleware used by the existing overview endpoints.

#### Scenario: Unauthenticated request is rejected

- **WHEN** a request lacks a valid administrator session
- **THEN** the sidecar returns the existing unauthorized response and does not call Sub2API

#### Scenario: Upstream failure is retryable

- **WHEN** the Sub2API breakdown request fails or times out
- **THEN** the sidecar returns a retryable upstream error while leaving `/api/overview` and `/api/online-users` independent

### Requirement: Administrators can open a responsive top-user dialog

The sidecar UI SHALL provide a per-group control that opens a dialog for the selected group's current-day top users.

#### Scenario: Open a group's top-user dialog

- **WHEN** an administrator activates “今日用户 Top 20” for a group
- **THEN** the dialog identifies the group and displays the latest user consumption rows

#### Scenario: Empty and error states are explicit

- **WHEN** the group has no usage or the request fails
- **THEN** the dialog displays an explicit empty/error state and offers a refresh action

#### Scenario: Narrow screens remain readable

- **WHEN** the dialog is viewed below the mobile breakpoint or operated with a keyboard
- **THEN** rows use readable stacked fields, controls remain focusable, and closing returns focus to the group button
