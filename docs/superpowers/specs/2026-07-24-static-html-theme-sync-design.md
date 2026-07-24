# Static HTML Theme Synchronization

## Goal

Make every custom static HTML menu page follow the application's active light or
dark theme. The existing card-purchase guide must also provide complete light and
dark styling.

## Chosen Design

`StaticHtmlFrame` owns theme synchronization. It observes the host document's
root `dark` class, derives `light` or `dark`, and rebuilds the sandboxed iframe
document whenever that value changes.

`buildStaticHtmlDocument` receives the resolved theme and adds
`data-theme="light"` or `data-theme="dark"` to the static document's root
element. Static page authors can therefore use selectors such as
`:root[data-theme='dark']` without access to the host page, scripts, storage, or
cross-window messaging.

The card-purchase guide will retain its current layout and teal primary palette.
It will define a dark token set for page background, surface, text, borders,
secondary controls, notice area, focus ring, and shadow. Its light token set
remains the default.

## Data Flow

1. A user toggles the application's theme.
2. The host document root gains or loses the `dark` class.
3. `StaticHtmlFrame` observes the class change and recomputes its `srcdoc`.
4. The sandboxed document receives the new `data-theme` value.
5. CSS in the static page selects the matching token set.

This works for the user-facing custom page and the administrator's HTML preview.

## Security and Compatibility

The existing iframe sandbox and Content Security Policy remain unchanged:

- No static-page JavaScript is enabled.
- No parent-window, local-storage, or `postMessage` access is introduced.
- Existing link normalization remains unchanged.
- Pages without theme-aware CSS continue to render as before.

The approach deliberately does not rely on `prefers-color-scheme`, because that
would fail when the user manually selects a site theme that differs from the
operating-system preference.

## Content Publishing

The card-purchase guide is stored as page content and is not part of the built
application image. After updating the local source artifact, overwrite the
existing stored HTML page with the new source so production receives the dark
theme rules.

## Verification

- Unit-test the generated document root for both `data-theme` values.
- Unit-test `StaticHtmlFrame` theme updates after the host class changes.
- Run frontend type checking and affected unit tests.
- Manually verify the guide in both modes, including a live manual theme switch,
  desktop/mobile layout, and both outbound and redeem links.

## Out of Scope

This change does not add user-configurable brand colors. It follows the existing
application teal primary palette and its light/dark neutral palettes.
