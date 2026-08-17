## 1. Guarded deletion lifecycle

- [x] 1.1 Add a manager deletion operation that re-lists local accounts, rejects in-use or unverifiable addresses, and deletes only the persisted sidecar record.
- [x] 1.2 Test eligible deletion, in-use conflict preservation, account-list failure, absence after deletion, and automatic fresh-candidate reappearance when the address is reused.

## 2. Administrator HTTP API

- [x] 2.1 Add the authenticated `DELETE /api/upstreams/{upstreamID}` route and expose the manager operation through the upstream console interface.
- [x] 2.2 Test unauthenticated rejection, successful 204 deletion, not-found behavior, and `UPSTREAM_IN_USE` conflict propagation.

## 3. Upstream-list interaction

- [x] 3.1 Render a danger-styled “删除上游” button only for persisted rows with zero local API Key accounts.
- [x] 3.2 Add an accessible second-confirmation dialog, focus restoration, busy/disabled state, DELETE request, list reload, and success/error toast behavior.
- [x] 3.3 Add static UI regression coverage for visibility conditions, confirmation wiring, destructive copy, and request behavior.

## 4. Validation and release

- [x] 4.1 Run formatting, Go unit/race/vet checks, JavaScript syntax checks, diff checks, and strict OpenSpec validation.
- [x] 4.2 Commit only this change and deploy the committed sidecar image to HC2 with checksum verification and post-deploy container/route health checks.
