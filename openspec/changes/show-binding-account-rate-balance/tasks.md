## 1. Data and rendering

- [x] 1.1 Add read-only binding-row render helpers for final multiplier and available balance, including available, unavailable, unlimited, and insufficient states.
- [x] 1.2 Extend the management dialog binding row markup with explicit metric labels, accessible descriptions, and the existing account/platform/binding state information.

## 2. Responsive presentation

- [x] 2.1 Update binding dialog grid styles for desktop metric columns and the existing narrow-screen stacked layout without changing selection controls.
- [x] 2.2 Verify the dialog uses existing semantic colors, numeric formatting, focus states, and no horizontal overflow at mobile breakpoints.

## 3. Verification

- [x] 3.1 Add or update static UI assertions for multiplier/balance labels and fallback states.
- [x] 3.2 Run JavaScript syntax checks, Go tests, and strict OpenSpec validation; confirm no Sub2API code or binding API payload changed.
