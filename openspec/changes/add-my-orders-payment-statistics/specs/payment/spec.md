## ADDED Requirements

### Requirement: Authenticated users can access a standalone order statistics page
The system SHALL provide an authenticated user-facing order statistics page that is separate from the existing “My Orders” page and is visible only when the payment feature is available.

#### Scenario: User opens order statistics from navigation
- **WHEN** an authenticated user selects the “订单统计” navigation entry
- **THEN** the system opens the standalone `/order-statistics` page
- **AND** the page loads personal payment statistics without loading the paginated order list

#### Scenario: Existing order page remains unchanged
- **WHEN** the order statistics capability is introduced
- **THEN** the existing `/orders` page continues to provide its current filtering, pagination, cancellation, and refund-request behavior
- **AND** no statistics controls or results are embedded into that page
- **AND** the existing `GET /api/v1/payment/orders/my` contract remains unchanged

#### Scenario: Payment feature is unavailable
- **WHEN** the payment feature is not available to the current user
- **THEN** the “订单统计” navigation entry follows the same visibility and route-access policy as “我的订单”

### Requirement: Personal order statistics are user-isolated and time-bounded
The system MUST calculate order statistics only from the authenticated user's payment orders whose `paid_at` falls within the selected inclusive calendar-date range in the effective timezone, whose status is `PAID`, `RECHARGING`, or `COMPLETED`, and whose `order_type` is `balance`, `usage_card`, or `subscription`.

#### Scenario: Default date range is used
- **WHEN** an authenticated user requests statistics without explicit start and end dates
- **THEN** the system uses the most recent 30 calendar days including the current day in the effective timezone
- **AND** the response identifies the normalized start date, end date, and timezone

#### Scenario: Valid custom date range is used
- **WHEN** the user selects an inclusive custom range of at most 366 calendar days
- **THEN** the system includes only qualifying orders paid from the selected start date at 00:00 through the instant before the day after the selected end date in that timezone

#### Scenario: Date range is invalid
- **WHEN** a request provides only one range boundary, an invalid date, an explicit invalid IANA timezone, an end date before the start date, or more than 366 inclusive calendar days
- **THEN** the system rejects the request with HTTP 400
- **AND** it does not return partial or fallback statistics

#### Scenario: Timezone is omitted
- **WHEN** a request does not provide a timezone
- **THEN** the system uses the configured site timezone for both query boundaries and daily grouping

#### Scenario: Another user's orders exist in the same range
- **WHEN** qualifying orders owned by other users exist within the selected range
- **THEN** none of their amounts, counts, types, or dates contribute to the authenticated user's response

#### Scenario: Unpaid or unsupported-status orders exist
- **WHEN** the current user has orders without `paid_at` or with a status outside `PAID`, `RECHARGING`, and `COMPLETED`
- **THEN** those orders do not contribute to any statistic or drilldown result

#### Scenario: An unsupported order type exists
- **WHEN** the current user has an order type outside `balance`, `usage_card`, and `subscription`
- **THEN** that order does not contribute to the summary, type aggregates, daily aggregates, or drilldown results

#### Scenario: Statistics request is unauthenticated
- **WHEN** a request to either statistics endpoint has no valid authenticated subject
- **THEN** the system returns HTTP 401
- **AND** no order data is returned

### Requirement: Selected-range totals use a single CNY payment basis
The system SHALL return one CNY summary for the selected range using each qualifying order's original `pay_amount`, without currency selection, conversion, or grouping.

#### Scenario: Qualifying orders exist
- **WHEN** one or more qualifying orders exist in the selected range
- **THEN** the response declares `currency` as `CNY`
- **AND** `summary` contains the sum of `pay_amount`, the successful order count, and the arithmetic average `pay_amount`

#### Scenario: Amounts are aggregated
- **WHEN** qualifying `pay_amount` values are accumulated
- **THEN** the system converts each value to integer cents before summing
- **AND** it rounds the final average to the nearest cent
- **AND** returned amounts contain at most two decimal places without floating-point accumulation drift

#### Scenario: No qualifying orders exist
- **WHEN** the selected range contains no qualifying orders for the authenticated user
- **THEN** `summary` contains zero amount, zero count, and zero average
- **AND** the page displays an explicit no-data state rather than treating a failed request as a successful zero result

### Requirement: Selected-range totals are grouped by supported order type
The system SHALL group qualifying orders by the existing order types `balance`, `usage_card`, and `subscription`, returning paid amount, order count, and average paid amount for each type.

#### Scenario: All three order types exist
- **WHEN** the selected range contains balance recharge, usage card, and subscription orders
- **THEN** `by_type` contains distinct aggregates for `balance`, `usage_card`, and `subscription`
- **AND** each aggregate contains only orders of its own type

#### Scenario: A supported type has no orders
- **WHEN** one or more supported order types have no qualifying orders in the selected range
- **THEN** `by_type` still contains exactly one row for each of the three supported types
- **AND** every absent type has zero amount, zero count, and zero average

#### Scenario: Type labels are displayed to the user
- **WHEN** the frontend renders type aggregates
- **THEN** `balance`, `usage_card`, and `subscription` are presented as “余额”, “余额卡”, and “订阅” using localized labels

### Requirement: Qualifying orders are aggregated by local calendar day
The system SHALL return daily paid amount, successful order count, and average paid amount grouped by calendar date in the effective timezone.

#### Scenario: Orders span multiple local days
- **WHEN** qualifying orders fall on different calendar dates in the effective timezone
- **THEN** `daily` contains one aggregate for each date that has qualifying orders
- **AND** every qualifying order contributes to exactly one daily item

#### Scenario: UTC boundary differs from local boundary
- **WHEN** an order's UTC timestamp maps to a different calendar date in the effective timezone
- **THEN** the order is grouped under the effective timezone's local calendar date

#### Scenario: Daylight-saving offset changes within the selected range
- **WHEN** a selected local day is shorter or longer than 24 hours because of a timezone transition
- **THEN** the system still includes exactly the instants from that local day's start through the next local day's start

#### Scenario: Daily statistics are displayed as a list
- **WHEN** daily statistics are available
- **THEN** the page displays date, total paid amount, successful order count, and average paid amount in a list ordered from newest date to oldest date
- **AND** it does not require a trend chart

### Requirement: Type and daily aggregates support consistent read-only drilldown
The system SHALL let users drill down from every type aggregate row and daily aggregate row into a paginated list built from the same authenticated-user, paid-status, and time predicates as the aggregate.

#### Scenario: User opens a type drilldown
- **WHEN** the user activates a type aggregate row
- **THEN** the system opens a read-only modal for that order type within the currently applied date range
- **AND** the drilldown total equals that type row's `order_count`

#### Scenario: User opens a daily drilldown
- **WHEN** the user activates a daily aggregate row
- **THEN** the system opens a read-only modal for that local calendar date across all supported order types
- **AND** the drilldown total equals that daily row's `order_count`

#### Scenario: Drilldown selector is invalid
- **WHEN** a details request provides both `order_type` and `date`, neither selector, an unsupported order type, an invalid date, or a date outside the selected range
- **THEN** the system rejects the request with HTTP 400
- **AND** it returns no partial detail list

#### Scenario: Drilldown results are paginated and stable
- **WHEN** a valid drilldown contains more than 20 matching orders
- **THEN** the endpoint returns 20 orders per page and a database-derived total
- **AND** orders are sorted by `paid_at` descending and then ID descending
- **AND** navigating between pages cannot duplicate or omit equal-time rows because of an unstable tie order

#### Scenario: Drilldown fields are minimized
- **WHEN** a valid drilldown page is returned
- **THEN** each item contains only order number, order type, original paid amount, status, payment method, and paid time
- **AND** the modal offers no cancel, refund, edit, or other write action

#### Scenario: Aggregate row is operated with a keyboard
- **WHEN** a keyboard user focuses a type or daily aggregate row and presses Enter or Space
- **THEN** the same drilldown opens as for a pointer click

### Requirement: Statistics filters and request states remain coherent
The system SHALL keep the displayed aggregates, the applied date range, and any drilldown request consistent when requests overlap or fail.

#### Scenario: User selects a shortcut range
- **WHEN** the user selects the 7-day, 30-day, or 90-day shortcut
- **THEN** that range is applied immediately and a new aggregate request begins

#### Scenario: User edits a custom range
- **WHEN** the user changes custom start or end dates without activating the query action
- **THEN** the displayed aggregates and drilldown context continue using the previously applied range

#### Scenario: Custom range request succeeds
- **WHEN** the user activates the query action for a valid custom range and the request succeeds
- **THEN** the custom range becomes the applied range
- **AND** the page displays the matching response

#### Scenario: Aggregate request fails
- **WHEN** the initial or replacement aggregate request fails
- **THEN** the page displays a retry action and does not present the failure as zero statistics
- **AND** a failed custom request does not replace the previously applied range

#### Scenario: Drilldown request fails
- **WHEN** a drilldown page request fails
- **THEN** the modal remains open with its selected context
- **AND** it displays an in-place retry action

#### Scenario: An obsolete request completes late
- **WHEN** a previous aggregate or drilldown request completes after a newer request, range change, selection change, page change, or modal close
- **THEN** the obsolete response does not replace the current state

### Requirement: Refunds do not alter order statistics
The system SHALL treat the original qualifying `pay_amount` as the paid-statistics value and SHALL NOT calculate refund totals or net paid amounts.

#### Scenario: A payment was refunded outside the site
- **WHEN** an operator performs a manual refund that is not represented by a qualifying order state or amount change
- **THEN** the statistics continue to reflect the stored original qualifying payment order
- **AND** the system makes no attempt to infer or reconcile the external refund
