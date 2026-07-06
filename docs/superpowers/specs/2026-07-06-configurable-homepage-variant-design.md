---
comet_change: configurable-homepage-variant
role: technical-design
canonical_spec: openspec
---

# Configurable Homepage Variant Design

## Context

This branch currently routes `/home` directly to the custom AIXW homepage at `frontend/src/views/public/AixwHomeView.vue`. The upstream default homepage still exists at `frontend/src/views/HomeView.vue`.

Deployments need a container startup parameter that chooses between those two homepages without rebuilding the image, editing static build output, storing a database setting, or adding an admin UI. The selected default is conservative: missing or invalid values use the upstream default homepage.

The project already has a suitable runtime configuration path:

- backend public settings are returned by `/api/v1/settings/public`;
- embedded frontend HTML injects the same settings as `window.__APP_CONFIG__`;
- the frontend app store hydrates from `window.__APP_CONFIG__` before normal async public settings loading.

The implementation should reuse that path with the smallest practical code surface.

## Architecture

Use a thin runtime selector for the public homepage.

```text
Container env
  HOMEPAGE_VARIANT=default|aixw
        |
        v
Backend normalizes value
  missing/invalid -> default
        |
        +--> /api/v1/settings/public: homepage_variant
        |
        +--> index.html injection: window.__APP_CONFIG__.homepage_variant
        |
        v
Frontend /home route
  HomeVariantView.vue
        |
        +--> default -> HomeView.vue
        |
        +--> aixw    -> AixwHomeView.vue
```

## Backend Design

Add the smallest environment-backed homepage variant helper:

- accepted values: `default`, `aixw`;
- missing, blank, or unsupported values normalize to `default`;
- comparison should be case-insensitive after trimming whitespace;
- no new database setting;
- no admin setting;
- no config.yaml requirement.

Expose the normalized value through the existing public settings path:

- service public settings model gains `HomepageVariant`;
- DTO `PublicSettings` gains `homepage_variant`;
- `PublicSettingsInjectionPayload` gains `homepage_variant`;
- `GetPublicSettingsForInjection` copies the normalized value into the injection payload.

This keeps API and first-load HTML behavior aligned.

## Frontend Design

Add a thin selector component, for example `frontend/src/views/public/HomeVariantView.vue`.

Responsibilities:

- read `appStore.cachedPublicSettings?.homepage_variant`;
- treat anything other than `aixw` as `default`;
- render `AixwHomeView` only when the normalized value is `aixw`;
- render `HomeView` otherwise.

Change the `/home` route component from direct `AixwHomeView.vue` import to the selector component. Keep route name, path, metadata, and `/` redirect unchanged.

This is intentionally one extra component rather than dynamic route mutation. The route table stays stable and all existing guards keep the same behavior.

## Deployment And Rollback

Document the environment variable in Docker examples:

```env
HOMEPAGE_VARIANT=default
```

Operators enable the custom homepage with:

```env
HOMEPAGE_VARIANT=aixw
```

Rollback is removing the variable or setting it back to `default`. No migration is required.

## Testing

Backend focused tests:

- missing env normalizes to `default`;
- blank/invalid env normalizes to `default`;
- `HOMEPAGE_VARIANT=default` returns `default`;
- `HOMEPAGE_VARIANT=aixw` returns `aixw`;
- public settings and injection payload include `homepage_variant`.

Frontend focused tests:

- selector renders default `HomeView` when settings are missing;
- selector renders default `HomeView` for `homepage_variant: 'default'`;
- selector renders AIXW homepage for `homepage_variant: 'aixw'`;
- `/home` still resolves as the public Home route and points at the selector.

## Risks And Mitigations

- First render in non-embedded dev mode may happen before async settings load. Mitigation: selector defaults to `default`; embedded/container builds use injected settings on first load.
- Public settings schema drift can hide the field in one path. Mitigation: update existing backend schema drift coverage and add focused tests for both API and injection payload.
- The custom homepage imports image assets, increasing bundle inclusion even when default is selected. Mitigation: use async component imports in the selector if bundle splitting is needed during implementation; keep the design open to the least disruptive option the current Vite output supports.
