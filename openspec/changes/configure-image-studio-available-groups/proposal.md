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
