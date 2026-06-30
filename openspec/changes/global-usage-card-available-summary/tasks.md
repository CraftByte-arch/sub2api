## 1. Backend Summary API

- [x] 1.1 Add a user usage-card summary response containing available card count and available remaining USD.
- [x] 1.2 Reuse the existing server-side available-card criteria for active, started, unexpired, undeleted, non-exhausted cards.
- [x] 1.3 Register the authenticated user route and add backend tests for active, expired, exhausted, suspended, cancelled, and future cards.

## 2. Frontend Shared State

- [x] 2.1 Add a frontend API method and type for loading the usage-card summary.
- [x] 2.2 Add a shared store/composable refresh entry point for topbar usage-card summary data.
- [ ] 2.3 Add or reuse a refresh entry point for the existing topbar long-term account balance.
- [ ] 2.4 Ensure usage-card summary and long-term balance refresh failures do not block callers that also refresh unrelated page data.

## 3. Global Topbar UI

- [x] 3.1 Change `UsageCardMini` to show available card count instead of total card count.
- [x] 3.2 Show the available remaining USD total directly in the global topbar component.
- [x] 3.3 Keep the existing long-term balance display unchanged and visually separate from the usage-card remaining total.
- [x] 3.4 Align the hover/summary copy with the available-card summary while preserving useful card details.
- [x] 3.5 Add or update frontend tests for zero cards, mixed unavailable cards, separated long-term balance display, and formatted summary display.

## 4. API Key Page Integration

- [ ] 4.1 Update the API key page refresh action to call the shared usage-card summary refresh and long-term balance refresh.
- [ ] 4.2 Add or update tests proving the API key refresh triggers both topbar usage-card information refresh and long-term balance refresh.

## 5. Verification

- [ ] 5.1 Run targeted backend usage-card tests.
- [ ] 5.2 Run targeted frontend tests or type checks for the topbar and API key page.
- [ ] 5.3 Validate the OpenSpec change artifacts.
