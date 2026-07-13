# Image Studio Prompt-Polish Key Design

## Scope

Image Studio's prompt-polish action needs its own API key selection. The image-generation key remains responsible only for queued image jobs. This is a frontend-only change: it reuses the existing user key list and gateway `GET /v1/models` endpoint, with no server API or persistence changes.

## User Interface

The prompt toolbar keeps the existing template control followed by one compact prompt-polish group:

- The left segment is the `润色` action.
- The middle segment selects the prompt-polish model.
- The right segment selects the prompt-polish API key.

The group has one rounded outer border and subtle internal dividers. `润色` is the only primary-tinted segment; model and key controls are lower-emphasis selectors. On narrow screens the group wraps cleanly, with the key selector on its own row when necessary.

## Models

The prompt-polish model selector contains:

- `gpt-5.4-mini`
- `gpt-5.4`
- `gpt-5.5`
- `gpt-5.6-sol`
- `gpt-5.6-terra`
- `gpt-5.6-luna`

## Data Flow

1. Load the user's API keys through the existing `keysAPI.list` call.
2. Create the prompt-polish candidate set from active keys in OpenAI groups. Do not apply the Image Studio group allowlist or image-generation flag.
3. Group candidates by group ID. For one representative key per group, call the existing gateway `GET /v1/models` endpoint with that key's bearer token.
4. Cache each group model list only for the current page instance. There is no local storage, server-side storage, or persisted key selection.
5. When a polish model is selected, show only candidate keys from groups whose cached model list includes that exact model. Multiple keys in the same compatible group remain independently selectable.

The use of `GET /v1/models` makes the selector reflect the group model mappings that are actually routable now, rather than a static administrative model-list display configuration.

## Selection and Errors

- On initial load, select the first configured polish model and then the first compatible polish key.
- When the polish model changes, retain the current polish key if it supports the new model. Otherwise select the first compatible key.
- If no compatible key exists, show an empty selector state, disable `润色`, and communicate that no key supports the selected model.
- Image-key selection must not modify prompt-polish selection, and prompt-polish selection must not modify the image key.
- A model-list lookup failure makes that group unavailable for prompt polishing for the current page session. It must not block image generation.
- The existing polish request sends the selected prompt-polish key; it must never fall back to the image-generation key.

## Tests

- Render all six polish model choices and the combined toolbar group.
- Verify only active OpenAI keys outside the image key allowlist can appear as polish candidates.
- Verify one model lookup per group, even when the user owns multiple keys in that group.
- Verify compatibility filtering for each selected model, including all three GPT-5.6 variants.
- Verify model changes retain a compatible key and replace an incompatible one.
- Verify no compatible key disables polish without disabling image generation.
- Verify the polish request uses the polish key while image job creation continues to use the image key.
