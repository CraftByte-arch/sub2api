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

The intended product direction is close to the provided reference screenshot: a compact image creation workspace, not a generic settings page.

Reference layout interpretation:

- The existing Sub2API sidebar remains visible. The `Image Studio` item should be highlighted with a soft teal active background and icon treatment consistent with the existing sidebar.
- The main content uses a single large rounded workspace shell with subtle border and shadow, centered inside the app content area.
- The workspace top bar has three tabs:
  - `文生图`
  - `图生图`
  - `记录`
- The active tab uses teal text plus a 2px underline. Inactive tabs use muted gray text and small line icons.
- The body splits into two columns:
  - left control column, about 500-540px wide on desktop
  - right preview/output canvas taking the remaining width
- The left control column contains:
  - API key select
  - model select/input
  - aspect ratio grid
  - image count slider
  - prompt textarea
  - optional advanced controls collapsed below the main flow
- The right canvas is a large dashed-border drop/preview surface with a centered empty state when no result exists.
- Empty state should show a small spark/image icon, title `还没有图片`, and helper text `在左侧填写提示词和参数，点击生成图片。`
- Generated results replace the empty state with a responsive image grid. Each image tile should expose download/open actions without shifting layout.

Visual tokens for the extension:

- Accent: teal/cyan in the same family as the screenshot, but adjusted to existing Sub2API colors.
- Background: app content may use a very light cool tint or existing page background; avoid heavy gradients.
- Radius: workspace shell can use a larger radius than small controls, but individual cards/tiles stay at 8px or below unless matching existing Sub2API components.
- Controls: use fixed-height selects/buttons and stable ratio tiles so hover/selection does not resize the layout.
- Typography: compact dashboard text, no hero-scale heading.

Primary regions:

- top mode tabs
- left prompt and controls panel
- edit-mode upload controls inside the left column
- right preview/results canvas
- raw response drawer or collapsible area below results

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
