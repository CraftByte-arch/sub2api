# Image Studio Fork Extension Design

## Context

Sub2API already supports OpenAI image generation at the gateway layer:

- `/v1/images/generations`
- `/v1/images/edits`
- `/v1/responses` with the `image_generation` tool

The fork also has an earlier standalone image console in `/Users/jixiwen/Documents/Codex/2026-05-15/openai-2`. That console proves the desired product shape: text-to-image, image editing, model and parameter controls, raw response inspection, and result preview.

This extension should become a native Sub2API website feature without turning the fork into a hard-to-sync rewrite of upstream.

## Goal

Add a native user-facing Image Studio to the fork while keeping upstream sync cost low.

## Non-Goals

- Do not rewrite the existing OpenAI gateway.
- Do not copy the standalone Node proxy into Sub2API.
- Do not require users to paste external upstream account credentials.
- Do not add broad framework or design-system changes.

## Recommended Architecture

Use a fork extension layer under `frontend/src/extensions/image-studio/`.

The extension will add a native Vue page that calls existing Sub2API gateway endpoints using a user-selected Sub2API API key. Existing backend controls continue to apply: group image-generation permission, billing eligibility, usage recording, concurrency limiting, account scheduling, failover, and content moderation.

Only a tiny stable integration surface should touch upstream-owned frontend files:

- router registration
- sidebar navigation entry
- i18n labels
- optional extension registry

This keeps future `upstream/main` rebases small and predictable.

## User Flow

1. User opens `Image Studio` from the sidebar.
2. Page loads the user's active API keys with their groups.
3. User selects an API key. The page should prefer keys whose group is OpenAI and allows image generation.
4. User chooses protocol:
   - default: Responses `image_generation`
   - advanced: Images API
5. User chooses mode:
   - text-to-image
   - image edit
6. User enters prompt and optional parameters.
7. Page calls the existing gateway endpoint with `Authorization: Bearer <selected-api-key>`.
8. Page extracts generated image output and renders thumbnails, metadata, and raw response.

## API Surface

No new backend endpoint in the first version.

The frontend will call:

- `POST /v1/responses`
- `POST /v1/images/generations`
- `POST /v1/images/edits`
- `GET /api/v1/keys` through the existing typed API client

If a later version must avoid exposing Sub2API API keys to browser requests, add a fork-only backend proxy such as:

- `POST /api/v1/extensions/image-studio/generate`
- `POST /api/v1/extensions/image-studio/edit`

That should be a second phase because it increases backend sync surface.

## Frontend Structure

Create:

- `frontend/src/extensions/image-studio/ImageStudioView.vue`
- `frontend/src/extensions/image-studio/imageStudioApi.ts`
- `frontend/src/extensions/image-studio/payload.ts`
- `frontend/src/extensions/image-studio/output.ts`
- `frontend/src/extensions/image-studio/types.ts`
- `frontend/src/extensions/image-studio/__tests__/`

Small upstream-touch files:

- `frontend/src/extensions/index.ts`
- `frontend/src/router/index.ts`
- `frontend/src/components/layout/AppSidebar.vue`
- `frontend/src/i18n/locales/zh.ts`
- `frontend/src/i18n/locales/en.ts`

## UI Design

The page should match Sub2API's existing admin/user console style:

- restrained dashboard layout
- dense but readable forms
- no marketing hero
- no nested decorative cards
- clear modes, model controls, and result grid

Primary regions:

- connection bar: API key, protocol, model
- prompt and controls panel
- upload panel for edit mode
- results panel
- raw response drawer or collapsible area

## Error Handling

Show friendly messages for common gateway errors:

- disabled image generation: group does not allow image generation
- insufficient balance or quota exhausted
- no compatible OpenAI account available
- upstream image model unsupported
- CORS/network errors for direct gateway calls in development
- invalid advanced JSON

Keep raw response available for debugging.

## Testing

Frontend tests should cover:

- payload building for Responses image generation
- payload building for Images API generation
- image output extraction from `b64_json`, `url`, and Responses image-generation output
- API key filtering/preference logic
- component smoke render and mode switching

Manual verification should include:

- `pnpm -C frontend typecheck`
- targeted Vitest tests
- Vite dev server browser check

## Sync Strategy

Keep upstream sync clean by maintaining extension work in fork commits:

```bash
git fetch upstream
git checkout fork/image-studio
git rebase upstream/main
```

Avoid editing upstream files unless they are stable integration points. If conflicts appear, they should usually be limited to router/sidebar/i18n.
