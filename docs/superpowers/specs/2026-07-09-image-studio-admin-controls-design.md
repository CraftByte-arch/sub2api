---
comet_change: configure-image-studio-available-groups
role: technical-design
canonical_spec: openspec
---

# Image Studio Admin Controls Design

## Overview

This change adds two administrator controls to the existing admin image studio settings tab:

1. **Image studio available groups**: a group ID allowlist that determines which groups can appear in the built-in image studio API key selector.
2. **Client `image_generation` tool declaration policy**: a global policy controlling how `/v1/responses` and Responses WebSocket requests handle clients that predeclare the native OpenAI `image_generation` tool.

The core design separates three concepts that are currently coupled:

- A group is allowed to actually generate images.
- The built-in image studio may offer API keys from a group.
- A compatible client may include an `image_generation` tool declaration in an ordinary Responses request.

The existing group `allow_image_generation` switch remains the actual image-generation capability gate. The new settings narrow image studio key visibility and prevent client tool declarations from being mistaken for actual image generation.

## Goals

- Let administrators choose which groups are available in the image studio experience from the admin settings page.
- Keep existing group image-generation eligibility and pricing semantics intact.
- Add a client image tool declaration policy with `strip`, `allow`, and `reject` values.
- Apply declaration policy consistently to HTTP `/v1/responses` and Responses WebSocket ingress.
- Keep dedicated image endpoints, image-only models, explicit image tool choice, and image studio jobs gated by existing group image-generation permission.

## Non-Goals

- No per-user or per-key image studio allowlists.
- No change to image pricing formulas or image rate multipliers.
- No change to image studio history, retention, or async worker scheduling beyond permission checks.
- No database schema migration unless implementation discovers settings storage cannot safely represent the new values.

## Settings Model

Add two system settings:

- `image_studio_available_group_ids`: JSON array or serialized list of positive group IDs.
- `image_generation_tool_declaration_policy`: enum string with allowed values:
  - `strip`
  - `allow`
  - `reject`

Recommended default for `image_generation_tool_declaration_policy` is `strip`.

`image_studio_available_group_ids` normalizes to unique positive integer IDs. Unknown or deleted groups are ignored when rendering selectors and when evaluating image studio key availability.

## Backend Behavior

### Image studio available groups

The image studio job creation path must validate both:

1. Existing actual image-generation permission: the key's group must allow image generation.
2. New image studio allowlist: the key's group ID must be in `image_studio_available_group_ids`.

This validation belongs in the synchronous job creation handler before enqueueing and should also be safe to repeat in the worker if the worker already checks image-generation permission.

### Client tool declaration policy

The gateway must distinguish passive tool declaration from actual image generation.

Passive declaration means the request includes an `image_generation` tool but does not explicitly force it through `tool_choice` and does not use an image-only model or a dedicated image endpoint.

Policy behavior:

- `strip`: remove `image_generation` tool entries from the request. If `tool_choice` points to the removed image tool only because of auto/default handling, normalize it so the request remains valid. Continue processing the request as ordinary text.
- `allow`: keep the declaration and continue processing. Do not reject only because the tool is present.
- `reject`: reject requests that declare the tool, preserving the current strict behavior.

Actual image generation remains blocked when the group disables image generation. Actual image generation includes:

- `/v1/images/generations`
- `/v1/images/edits`
- `/image-studio/jobs`
- image-only model requests such as `gpt-image-*`
- explicit `tool_choice` selecting `image_generation`
- Responses WebSocket payloads that explicitly select or otherwise require image generation

HTTP `/v1/responses` and Responses WebSocket should share the same classifier and policy helper where practical so behavior does not drift.

## Frontend Behavior

### Admin settings

The existing image studio settings tab gains:

- A multi-select for available groups.
- A policy selector for client image tool declaration behavior.

Group options should show all groups and include status hints where available, such as platform and whether the group currently allows image generation. The saved allowlist is allowed to include groups that are not currently eligible; they simply will not produce selectable image studio keys until they satisfy eligibility.

### Image studio page

The built-in image studio key selector filters user API keys by:

- key status is active
- group platform is OpenAI
- group allows image generation
- group ID appears in `image_studio_available_group_ids`

If no key remains, the empty state should explain that no administrator-enabled image studio group is available.

## Data Flow

1. Admin opens settings.
2. Frontend fetches system settings and group options.
3. Admin saves selected group IDs and declaration policy through the existing settings update endpoint.
4. Backend normalizes and persists settings.
5. Image studio page fetches settings and user keys, then filters keys by the allowlist and existing eligibility.
6. Gateway reads declaration policy while processing `/v1/responses` or WebSocket payloads.
7. Gateway strips, allows, or rejects passive tool declarations before actual image-generation permission checks.

## Error Handling

- Invalid declaration policy values should normalize to `strip` or return a validation error. Prefer validation error on admin writes and safe default on reads.
- Malformed group IDs should be rejected when the JSON shape is invalid; duplicates and non-positive numeric values should be normalized away or rejected consistently with existing settings validation patterns.
- Image studio job creation with a key outside the allowlist returns a clear forbidden/bad request message that the key's group is not available for image studio.
- Actual image-generation attempts on disabled groups continue returning the existing stable message: `Image generation is not enabled for this group`.

## Testing Strategy

### Backend

- Settings defaults include empty available group IDs and `strip` declaration policy.
- Settings update persists and returns group IDs and declaration policy.
- Invalid declaration policy is rejected or safely normalized according to implementation convention.
- `/image-studio/jobs` rejects keys outside the allowlist.
- `/v1/responses` with passive `image_generation` declaration and group image disabled:
  - `strip` removes the tool and continues.
  - `allow` continues without the old permission error.
  - `reject` rejects.
- `/v1/responses` with explicit image tool choice still rejects when group image generation is disabled.
- Responses WebSocket applies the same policy.
- Dedicated image endpoints still reject when group image generation is disabled.

### Frontend

- Admin settings form loads, displays, edits, and saves selected group IDs.
- Admin settings form loads, displays, edits, and saves declaration policy.
- Image studio key selector only shows keys from allowed eligible groups.
- Empty allowlist produces a helpful empty state.

## Rollout Notes

Defaulting declaration policy to `strip` reduces false positives for clients that predeclare tools. Defaulting image studio available groups to empty is explicit but may require administrators to configure groups after upgrade before the built-in image studio page shows keys.

## Merge Reconciliation

The post-verification `main` merge introduced split service files. Keep those new ownership boundaries, but port the complete pre-merge behavior rather than resolving at whole-file granularity.

- `gateway_usage_billing.go` owns usage-card command construction and both gateway usage recorders must populate the global enablement and resolved key priority.
- `setting_handler.go` and `setting_handler_update.go` must preserve every existing settings field on unrelated saves; pointer request fields use previous-value semantics.
- Native flat `image_generation` declarations are passive when not explicitly selected. Codex `image_gen` namespace and Responses Lite `additional_tools` remain actual image requests and stay group-gated.
- Image declaration/access preflight runs before passthrough forwarding.
- `openai_gateway_scheduling.go` must carry image protocol preference through both scheduler and load-aware fallback paths.
