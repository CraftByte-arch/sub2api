# image-studio-available-groups Specification

## Purpose
Define administrator-controlled Image Studio group availability and client image-tool declaration behavior while preserving the existing group-level image-generation authorization gate.

## Requirements
### Requirement: Admin-configured image studio group allowlist
The system SHALL provide an administrator setting that stores the API key groups available in the image studio experience.

#### Scenario: Administrator saves selected groups
- **WHEN** an administrator selects one or more groups in the image studio settings tab and saves settings
- **THEN** the system persists the selected group IDs as the image studio available groups setting

#### Scenario: Administrator clears selected groups
- **WHEN** an administrator saves the image studio settings tab with no selected groups
- **THEN** the system persists an empty image studio available groups setting

#### Scenario: Invalid group identifiers are submitted
- **WHEN** the settings update payload contains duplicate, non-positive, or malformed group identifiers for image studio available groups
- **THEN** the system normalizes the setting to unique positive group IDs or rejects malformed payloads with a validation error

### Requirement: Image studio key selection respects the group allowlist
The system SHALL only show image studio API key choices whose group is included in the image studio available groups setting and also satisfies existing image-generation eligibility.

#### Scenario: A key belongs to an allowed eligible group
- **WHEN** a user opens image studio and has an active API key in an allowed OpenAI group with image generation enabled
- **THEN** the API key is available in the image studio key selector

#### Scenario: A key belongs to an unselected group
- **WHEN** a user opens image studio and has an API key in a group that is not selected in the image studio available groups setting
- **THEN** the API key is not available in the image studio key selector

#### Scenario: No groups are selected
- **WHEN** the image studio available groups setting is empty
- **THEN** no API keys are available in the image studio key selector

### Requirement: Image studio job creation enforces the group allowlist
The system SHALL reject image studio job creation when the submitted API key belongs to a group outside the image studio available groups setting.

#### Scenario: Job uses an allowed key
- **WHEN** a user creates an image studio job with an API key from an allowed eligible group
- **THEN** the system accepts the job request subject to existing image studio validation

#### Scenario: Job uses an unallowed key
- **WHEN** a user creates an image studio job with an API key from a group that is not selected in the image studio available groups setting
- **THEN** the system rejects the request and does not enqueue an image studio job

### Requirement: Client image generation tool declaration policy
The system SHALL provide an administrator setting that controls how OpenAI Responses clients may declare the native `image_generation` tool independently from whether a group may actually generate images.

#### Scenario: Strip declared image generation tool
- **WHEN** the declaration policy is `strip` and a client sends an ordinary `/v1/responses` request that predeclares an `image_generation` tool without explicitly selecting it
- **THEN** the system removes the `image_generation` tool declaration and continues processing the request

#### Scenario: Allow declared image generation tool
- **WHEN** the declaration policy is `allow` and a client sends an ordinary `/v1/responses` request that predeclares an `image_generation` tool without explicitly selecting it
- **THEN** the system continues processing the request without rejecting it solely because the tool is declared

#### Scenario: Reject declared image generation tool
- **WHEN** the declaration policy is `reject` and a client sends a `/v1/responses` request that declares an `image_generation` tool
- **THEN** the system rejects the request with a permission error

#### Scenario: Explicit image generation remains gated by group setting
- **WHEN** a group has image generation disabled and a client explicitly selects the `image_generation` tool, uses a dedicated image endpoint, or uses an image-only model
- **THEN** the system rejects the request regardless of the declaration policy

#### Scenario: Responses WebSocket uses the same declaration policy
- **WHEN** a Responses WebSocket client declares an `image_generation` tool
- **THEN** the system applies the same declaration policy as HTTP `/v1/responses`

#### Scenario: Passthrough accounts use the same declaration policy
- **WHEN** an OpenAI passthrough account receives an ordinary `/v1/responses` request that predeclares the native `image_generation` tool
- **THEN** the system applies the configured `strip`, `allow`, or `reject` policy before entering passthrough forwarding

#### Scenario: Codex image namespace remains an actual image request
- **WHEN** a client uses the Codex `image_gen` namespace or Responses Lite `additional_tools` image namespace for an image request
- **THEN** the system continues to enforce the group's existing image-generation switch
