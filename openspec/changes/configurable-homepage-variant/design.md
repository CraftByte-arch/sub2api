## Context

The current branch changes `/home` from the upstream `HomeView.vue` to the custom `AixwHomeView.vue`. Container operators need to choose between those two homepage implementations at runtime, not at image build time and not through a database-backed admin setting.

The backend already injects public settings into `index.html` as `window.__APP_CONFIG__`, and the frontend store hydrates from that object before Vue finishes mounting. The implementation should reuse this existing path and avoid new database settings, admin UI, route mutation, or Docker entrypoint rewriting.

## Goals / Non-Goals

**Goals:**

- Read `HOMEPAGE_VARIANT` from the process environment and normalize it to `default` or `aixw`.
- Default to the upstream homepage when `HOMEPAGE_VARIANT` is unset or invalid.
- Expose the normalized value through `/api/v1/settings/public` and `window.__APP_CONFIG__`.
- Let `/home` render either `HomeView.vue` or `AixwHomeView.vue` through a thin selector component.

**Non-Goals:**

- No admin UI for changing the homepage variant at runtime.
- No removal of the AIXW homepage or upstream default homepage.
- No changes to the `main` branch.
- No build-time-only Vite switch.
- No Docker entrypoint static-file rewriting.

## Decisions

1. Use a small environment-variable helper for `HOMEPAGE_VARIANT` with `default` and `aixw` as the only accepted values.

   Rationale: the requested interface is explicit, easy to document in Docker Compose, and independent of database state. Invalid values fail closed to `default` so upstream behavior remains the safe fallback. This does not need a new config.yaml section or admin-managed setting.

   Alternative considered: config.yaml-only setting. This would work for bare-metal installs but is less direct for container startup parameters and does not satisfy the primary deployment ergonomics as well.

2. Add the normalized value to public settings and HTML injection.

   Rationale: routing must make the homepage choice before or during initial app hydration. The existing injection mechanism already solves this for public runtime flags and avoids an extra request before route resolution.

   Alternative considered: frontend-only `import.meta.env`. That would be build-time, not container-runtime, for the embedded frontend bundle.

3. Select the homepage through a thin wrapper route component instead of mutating the static route table after settings load.

   Rationale: Vue Router static routes can continue to point `/home` at one stable component while the wrapper renders the selected homepage. This is a one-line route change plus one small component, keeping route metadata and existing guards simple.

   Alternative considered: dynamic route replacement. That adds lifecycle complexity and can be sensitive to whether public settings have loaded before the first navigation.

4. Do not rewrite built frontend files in the container entrypoint.

   Rationale: entrypoint rewriting looks small but couples startup scripts to hashed build output and makes local tests weaker. A public-settings field is less brittle and reuses code the app already depends on.

## Risks / Trade-offs

- [Risk] The first route render could happen before public settings are fetched in non-embedded development mode. -> Mitigation: default the frontend selector to `default` and rely on the store's injected config path for embedded/container builds.
- [Risk] Existing tests assume `/home` maps directly to `AixwHomeView.vue`. -> Mitigation: update route tests to assert runtime selection behavior instead of direct component identity.
- [Risk] Adding public settings fields can drift between backend DTO, injection payload, and frontend types. -> Mitigation: extend existing schema drift tests and focused store/type tests where practical.

## Migration Plan

1. Deploy images with no `HOMEPAGE_VARIANT` to keep the upstream default homepage.
2. Set `HOMEPAGE_VARIANT=aixw` in the container environment to enable the custom homepage.
3. Roll back by removing the variable or setting `HOMEPAGE_VARIANT=default`; no database migration or rebuild is required.
