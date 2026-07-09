# Comet Design Handoff

- Change: configure-image-studio-available-groups
- Phase: design
- Mode: compact
- Context hash: 574c26c14a36b71682aea6b5126602dbfbf9621fdcc1caa7364d43589d3307a0

Generated-by: comet-handoff.sh

OpenSpec remains the canonical capability spec. This handoff is a deterministic, source-traceable context pack, not an agent-authored summary.

## openspec/changes/configure-image-studio-available-groups/proposal.md

- Source: openspec/changes/configure-image-studio-available-groups/proposal.md
- Lines: 1-30
- SHA256: 8abb1ea752e4500f2fa0de78f0abe10b0499d18cfd6f2e7b19614da77973a803

```md
## Why

The image studio currently derives usable API keys from each key's group image-generation flag, so administrators cannot centrally choose which groups should appear in the image studio experience. Administrators need a dedicated image studio setting that limits the selectable key groups without changing the broader group capability or billing configuration.

## What Changes

- Add admin-managed image studio controls under the existing image studio settings tab:
  - image studio available groups
  - client `image_generation` tool declaration policy
- Persist the selected group IDs through the system settings flow.
- Filter image studio API key choices so only keys from selected groups can be chosen.
- Preserve the existing OpenAI/image-generation eligibility checks; the new setting is an additional allowlist, not a replacement for group capability validation.
- Treat an empty allowlist as no groups available in image studio until an administrator selects groups.
- Distinguish client tool declaration from actual image generation so clients that predeclare `image_generation` tools are not necessarily rejected when group image generation is disabled.

## Capabilities

### New Capabilities
- `image-studio-available-groups`: Controls which API key groups are available in the image studio experience and how clients may declare the native `image_generation` tool.

### Modified Capabilities
- None.

## Impact

- Backend system settings model, defaults, validation, persistence, DTOs, and admin settings handler.
- Frontend admin settings API types and settings page image studio tab.
- Frontend image studio API key filtering and empty-state copy.
- Gateway request normalization and permission checks for `/v1/responses` and Responses WebSocket.
- Tests covering settings persistence/normalization, image studio key visibility, and image tool declaration policy behavior.
```

## openspec/changes/configure-image-studio-available-groups/design.md

- Source: openspec/changes/configure-image-studio-available-groups/design.md
- Lines: 1-71
- SHA256: 8bf7f121bcd39fa4f31274e86c7263ddcb7d736e92d9ccb183442ddd4473f4bd

```md
## Context

Image studio already has an admin settings area for async concurrency and asset retention. API key choices in `ImageStudioView.vue` are currently loaded from the user's keys and filtered client-side to active OpenAI groups whose `allow_image_generation` flag is true. That means the same group-level image-generation capability controls both technical eligibility and whether the group appears in the image studio UI.

Administrators need a separate image studio allowlist so they can keep group image-generation configuration intact while deciding which groups are exposed in the image studio experience. Because image studio jobs are created with `api_key_id`, server-side validation is required in addition to UI filtering.

The existing group image-generation switch also blocks clients that merely predeclare the native Responses `image_generation` tool. Some clients, especially Codex/Responses-compatible clients, can advertise the tool in ordinary requests even when the user has not asked for image generation. The new design separates "client may declare the tool" from "the group may actually generate images".

## Goals / Non-Goals

**Goals:**
- Add a system setting that stores the selected image studio group IDs.
- Add a system setting that controls client `image_generation` tool declaration behavior.
- Let administrators edit the allowlist in the existing image studio settings tab.
- Filter image studio API key choices by the allowlist, existing active status, OpenAI platform, and group image-generation eligibility.
- Reject image studio job creation when the supplied API key belongs to a group outside the allowlist.
- Keep actual image generation gated by the existing group `allow_image_generation` flag.
- Avoid rejecting ordinary Responses requests solely because the client predeclared `image_generation`.
- Keep empty allowlist behavior explicit: no groups are available until an administrator selects them.

**Non-Goals:**
- Do not change group pricing, image rate multipliers, or the existing `allow_image_generation` meaning.
- Do not change image studio model controls, history, async worker behavior, or asset retention.
- Do not introduce per-user or per-key image studio allowlists.
- Do not migrate existing API keys or groups.
- Do not change image generation billing formulas.

## Decisions

1. Store the allowlist as a system setting containing group IDs.

   Rationale: the rest of image studio admin configuration already uses the settings flow, so this keeps persistence, admin DTOs, audit handling, and deployment behavior consistent. The value can be normalized as an integer list and does not require a schema migration.

   Alternative considered: add a field to the group table. That would mix "group can do image generation" with "group is visible in image studio" and would make the user's requested admin setting less discoverable.

2. Treat the allowlist as an additional gate on top of existing group eligibility.

   Rationale: a group still needs to be OpenAI-backed and have image generation enabled for correct pricing and capability semantics. The new setting only narrows availability for the image studio UX.

   Alternative considered: use the allowlist alone. That could expose groups that are not technically or commercially configured for image generation.

3. Enforce the allowlist in both frontend filtering and backend job creation.

   Rationale: frontend filtering provides the intended user experience, while backend validation prevents direct API calls from creating jobs with non-allowed keys.

   Alternative considered: frontend-only filtering. That would be easier to implement but would not enforce the administrator policy.

4. Empty allowlist means no image studio groups are available.

   Rationale: the requested control says only selected groups should be selectable. Empty-as-all would be more backward compatible but less explicit and could surprise administrators after deployment.

   Alternative considered: empty allowlist means all currently eligible groups. That preserves current behavior but weakens the policy and makes it impossible to intentionally disable all groups with the same setting.

5. Add a global client image tool declaration policy with `strip`, `allow`, and `reject`.

   Rationale: a global setting fits the existing image studio settings tab and fixes the client compatibility issue without adding per-group complexity. `strip` is the recommended default because it prevents predeclared tools from breaking ordinary chat while ensuring disabled groups do not expose the image tool upstream. `allow` preserves declarations for clients that require tool visibility, while still rejecting actual image-generation intent. `reject` preserves the current strict behavior for operators who want it.

   Alternative considered: group-level declaration policy. That would offer finer control but would make the admin model harder to understand and would duplicate the existing group image-generation switch.

6. Treat actual image generation more narrowly than tool declaration.

   Rationale: a request that merely includes `tools: [{type: "image_generation"}]` with automatic tool choice should not be equivalent to dedicated image endpoints, image-only models, or explicit `tool_choice: {type: "image_generation"}`. The gateway should apply the declaration policy before the existing image-generation permission gate, then continue to reject explicit or dedicated image generation when the group is disabled.

   Alternative considered: keep the current "any declaration equals image intent" behavior. That is simple but causes the observed false positive for clients that predeclare available tools.

## Risks / Trade-offs

- [Risk] Existing deployments may see no selectable image studio keys until administrators configure the allowlist. -> Mitigation: make the admin setting visible in the image studio tab and show a clear image studio empty state that points users to administrator configuration.
- [Risk] Settings payload drift can occur across backend service structs, DTOs, frontend API types, and settings form state. -> Mitigation: update focused backend/frontend tests around settings serialization and image studio filtering.
- [Risk] Deleted groups could leave stale IDs in the stored allowlist. -> Mitigation: normalize to positive unique integers on write and ignore unknown group IDs when rendering choices or validating jobs.
- [Risk] `allow` policy can let the model see an image tool even when the group cannot actually generate images. -> Mitigation: recommend `strip` as the default and keep explicit image-generation requests blocked by the group switch.
```

## openspec/changes/configure-image-studio-available-groups/tasks.md

- Source: openspec/changes/configure-image-studio-available-groups/tasks.md
- Lines: 1-34
- SHA256: 43406da65736cdc63f6645234bbfc4523e3833ac786fa61471cb0421a032bf77

```md
## 1. Backend Settings

- [ ] 1.1 Add an image studio available group IDs setting key, default, parser, normalizer, and service field.
- [ ] 1.2 Expose the setting through backend admin settings DTOs and update request handling.
- [ ] 1.3 Include the setting in settings update audit/change tracking.

## 2. Backend Enforcement

- [ ] 2.1 Enforce the image studio group allowlist when creating image studio jobs.
- [ ] 2.2 Return a clear validation error when a selected API key's group is not available for image studio.
- [ ] 2.3 Add a client image-generation tool declaration policy setting with `strip`, `allow`, and `reject` values.
- [ ] 2.4 Apply the declaration policy to HTTP `/v1/responses` before image-generation permission checks.
- [ ] 2.5 Apply the declaration policy consistently to Responses WebSocket requests.
- [ ] 2.6 Keep explicit image generation, dedicated image endpoints, image-only models, and image studio jobs gated by the existing group image-generation switch.

## 3. Admin Settings UI

- [ ] 3.1 Extend frontend admin settings API types and form state with image studio available group IDs.
- [ ] 3.2 Extend frontend admin settings API types and form state with the image tool declaration policy.
- [ ] 3.3 Add a multi-select control in the image studio settings tab that loads administrator group options.
- [ ] 3.4 Add a policy selector in the image studio settings tab for `strip`, `allow`, and `reject`.
- [ ] 3.5 Save and restore the selected group IDs and declaration policy through the existing settings save flow.

## 4. Image Studio Experience

- [ ] 4.1 Fetch or reuse the image studio available group IDs setting for the image studio page.
- [ ] 4.2 Filter image studio API key choices by active status, OpenAI image eligibility, and the configured group allowlist.
- [ ] 4.3 Update the empty state so users understand no key is available when no allowed group is configured.

## 5. Tests and Verification

- [ ] 5.1 Add or update backend tests for settings normalization, serialization, job creation enforcement, and declaration policy behavior.
- [ ] 5.2 Add or update frontend tests for admin settings form persistence and image studio API key filtering.
- [ ] 5.3 Run targeted backend and frontend verification commands.
```

## openspec/changes/configure-image-studio-available-groups/specs/image-studio-available-groups/spec.md

- Source: openspec/changes/configure-image-studio-available-groups/specs/image-studio-available-groups/spec.md
- Lines: 1-65
- SHA256: 81e8e746038f031c26b0159b1e08b9eb29f6fe3623e7c9d184830c0f5104b312

```md
## ADDED Requirements

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
```

