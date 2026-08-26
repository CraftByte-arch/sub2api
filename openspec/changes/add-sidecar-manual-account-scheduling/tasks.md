## 1. Engine scheduling control

- [x] 1.1 Add an account-level operation guard shared by manual scheduling updates and detection runs
- [x] 1.2 Implement manual API Key scheduling updates with active-status validation and automatic-suspension ownership reset
- [x] 1.3 Add engine tests for stop, enable, unmanaged accounts, invalid accounts, write failures, and concurrent checks

## 2. Administrator API

- [x] 2.1 Add the authenticated `PUT /api/accounts/{accountID}/schedulable` endpoint with strict boolean request validation
- [x] 2.2 Map unsupported accounts, inactive enable attempts, running checks, and upstream failures to stable HTTP errors
- [x] 2.3 Add server tests for authorization, validation, success, conflict, and failure responses

## 3. Group account interface

- [x] 3.1 Add a separate global scheduling state and switch to each API Key account row
- [x] 3.2 Add stop and automatic-suspension recovery confirmations, shared-account busy state, disabled-state explanations, and API integration
- [x] 3.3 Extend existing styles and static UI assertions without changing the established admin design system

## 4. Verification

- [x] 4.1 Run formatter, unit tests, race tests, static analysis, and Linux amd64 build verification
