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

## Merge Reconciliation

The `main` merge split gateway, scheduling, settings, and billing code into smaller files after the original implementation was verified. Reconciliation uses the `main` file boundaries as the structural baseline while preserving the pre-merge business behavior.

- Native flat `image_generation` declarations without explicit tool selection remain passive and follow the global declaration policy.
- Codex `image_gen` namespace and Responses Lite `additional_tools` image requests remain actual image intent because `main` added those classifiers to close a group-permission bypass.
- Declaration policy and actual-image permission checks run before HTTP passthrough forwarding so passthrough and transformed requests have the same access behavior.
- Existing usage-card billing, settings round-trip, and image-protocol scheduling behavior must survive the structural merge even though those capabilities are not expanded by this change.
