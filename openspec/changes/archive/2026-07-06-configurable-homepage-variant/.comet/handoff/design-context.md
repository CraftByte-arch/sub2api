# Comet Design Handoff

- Change: configurable-homepage-variant
- Phase: design
- Mode: compact
- Context hash: 31fbc867454d21580e6c45c182d044666da431c727ebf77d4fb07f06fedff49d

Generated-by: comet-handoff.sh

OpenSpec remains the canonical capability spec. This handoff is a deterministic, source-traceable context pack, not an agent-authored summary.

## openspec/changes/configurable-homepage-variant/proposal.md

- Source: openspec/changes/configurable-homepage-variant/proposal.md
- Lines: 1-28
- SHA256: a899d0b4ae1b628c123e4efb2dde527136125fca0d35bd5d5d9dca948940cb74

```md
## Why

The project now contains a custom AIXW landing page, while deployments still need a simple way to keep the upstream default homepage without rebuilding the image. A runtime container parameter lets operators choose the homepage variant at startup.

## What Changes

- Add a public runtime homepage variant setting controlled by the `HOMEPAGE_VARIANT` environment variable.
- Support `HOMEPAGE_VARIANT=aixw` to show the custom AIXW homepage.
- Support `HOMEPAGE_VARIANT=default` to show the upstream default homepage.
- Default to `default` when the variable is missing or invalid.
- Expose the selected variant through public settings and the server-side HTML injection payload so the first page load uses the configured homepage.

## Capabilities

### New Capabilities

- `homepage-variant`: Runtime selection of the public homepage variant for container deployments.

### Modified Capabilities

- None.

## Impact

- Backend configuration loading and public settings payloads.
- Frontend public settings type and route/homepage component selection.
- Docker/deploy environment examples documenting `HOMEPAGE_VARIANT`.
- Focused backend/frontend tests for default, AIXW, and invalid values.
```

## openspec/changes/configurable-homepage-variant/design.md

- Source: openspec/changes/configurable-homepage-variant/design.md
- Lines: 1-58
- SHA256: 156fd7ffd81ca984e7f48a9fbfa02833062513cfadfe776d3270d98dbe2e9662

```md
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
```

## openspec/changes/configurable-homepage-variant/tasks.md

- Source: openspec/changes/configurable-homepage-variant/tasks.md
- Lines: 1-17
- SHA256: 91f8f279dcea7cbdcc47f6c16708efa0c7d3f234a344d18a278e58577ff07475

```md
## 1. Runtime Configuration

- [ ] 1.1 Add minimal backend environment support for `HOMEPAGE_VARIANT` with normalization to `default` or `aixw`.
- [ ] 1.2 Expose the normalized homepage variant through service public settings, DTO public settings, and HTML injection payloads.
- [ ] 1.3 Document `HOMEPAGE_VARIANT` in Docker environment examples.

## 2. Frontend Homepage Selection

- [ ] 2.1 Add frontend public settings typing and default handling for the homepage variant.
- [ ] 2.2 Route `/home` through a thin runtime selector that renders the upstream `HomeView` or custom `AixwHomeView`.
- [ ] 2.3 Preserve `/` redirect behavior and unauthenticated access behavior for `/home`.

## 3. Verification

- [ ] 3.1 Add backend tests for missing, invalid, `default`, and `aixw` homepage variant values.
- [ ] 3.2 Add frontend tests for runtime homepage selection from injected/public settings.
- [ ] 3.3 Run focused backend and frontend verification commands.
```

## openspec/changes/configurable-homepage-variant/specs/homepage-variant/spec.md

- Source: openspec/changes/configurable-homepage-variant/specs/homepage-variant/spec.md
- Lines: 1-31
- SHA256: 6340db16a005c70783eaa2a4ba0fa0e30d46e66cd3637102048eefb6d5432eca

```md
## ADDED Requirements

### Requirement: Runtime homepage variant selection
The system SHALL select the public homepage variant from the `HOMEPAGE_VARIANT` container environment variable at backend process startup.

#### Scenario: AIXW homepage selected
- **WHEN** the backend starts with `HOMEPAGE_VARIANT=aixw`
- **THEN** `/home` renders the custom AIXW homepage.

#### Scenario: Default homepage selected
- **WHEN** the backend starts with `HOMEPAGE_VARIANT=default`
- **THEN** `/home` renders the upstream default homepage.

#### Scenario: Missing homepage variant
- **WHEN** the backend starts without `HOMEPAGE_VARIANT`
- **THEN** `/home` renders the upstream default homepage.

#### Scenario: Invalid homepage variant
- **WHEN** the backend starts with an unsupported `HOMEPAGE_VARIANT` value
- **THEN** the system treats the value as `default`.

### Requirement: Homepage variant public settings exposure
The system SHALL expose the normalized homepage variant through public settings and the server-side HTML injection payload.

#### Scenario: API exposes normalized value
- **WHEN** a client requests `/api/v1/settings/public`
- **THEN** the response includes the normalized homepage variant.

#### Scenario: Injected settings expose normalized value
- **WHEN** the embedded frontend serves `index.html`
- **THEN** `window.__APP_CONFIG__` includes the normalized homepage variant before Vue mounts.
```

