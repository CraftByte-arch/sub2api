## 1. Same-origin session handling

- [x] 1.1 Replace sidecar URL/session-storage token capture with same-origin localStorage token reads and storage-event synchronization.
- [x] 1.2 Add browser-side refresh-token request, single-flight refresh coordination, and one-time retry for 401/expired-session responses.

## 2. Login recovery

- [x] 2.1 Add fixed internal login redirect handling for embedded and standalone sidecar pages, without placing tokens in URLs.
- [x] 2.2 Preserve existing admin authorization checks and make session expiry display a recoverable login transition.

## 3. Verification and release

- [x] 3.1 Extend static UI tests for localStorage auth, refresh endpoint, token-free URLs, retry, and login redirect behavior.
- [ ] 3.2 Run sidecar tests, syntax checks, strict OpenSpec validation, commit the change, build an amd64 HC2 image, and verify only the sidecar container changes.
