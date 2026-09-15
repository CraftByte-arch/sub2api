## 1. Bound-group projection in the browser

- [x] 1.1 Derive identity-scoped available groups from existing group snapshots, decorating them with remote-key bindings and snapshot-missing fallbacks.
- [x] 1.2 Replace the inline available-group section with a per-upstream available-group count and dialog trigger.

## 2. Accessible compact dialog

- [x] 2.1 Add the read-only bound-group dialog markup, close behavior, focus restoration, and grouped rendering.
- [x] 2.2 Add responsive styles for the dialog's data-dense group and binding rows without horizontal overflow.

## 3. Validation

- [x] 3.1 Add focused static UI regression coverage for aggregation, empty states, dialog behavior, and removed inline rendering.
- [x] 3.2 Run JavaScript syntax checks, web tests, full sidecar tests, vet, and diff validation.

## 4. Dialog scroll isolation

- [x] 4.1 Prevent background scroll chaining while the upstream-group dialog is open and its list reaches a vertical edge.
- [x] 4.2 Add static regression coverage and rerun relevant sidecar validation.
