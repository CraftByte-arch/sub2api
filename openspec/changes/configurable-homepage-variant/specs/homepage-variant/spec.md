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
