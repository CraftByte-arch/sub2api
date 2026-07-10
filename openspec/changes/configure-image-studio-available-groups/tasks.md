## 1. Backend Settings

- [x] 1.1 Add an image studio available group IDs setting key, default, parser, normalizer, and service field.
- [x] 1.2 Expose the setting through backend admin settings DTOs and update request handling.
- [x] 1.3 Include the setting in settings update audit/change tracking.

## 2. Backend Enforcement

- [x] 2.1 Enforce the image studio group allowlist when creating image studio jobs.
- [x] 2.2 Return a clear validation error when a selected API key's group is not available for image studio.
- [x] 2.3 Add a client image-generation tool declaration policy setting with `strip`, `allow`, and `reject` values.
- [x] 2.4 Apply the declaration policy to HTTP `/v1/responses` before image-generation permission checks.
- [x] 2.5 Apply the declaration policy consistently to Responses WebSocket requests.
- [x] 2.6 Keep explicit image generation, dedicated image endpoints, image-only models, and image studio jobs gated by the existing group image-generation switch.

## 3. Admin Settings UI

- [x] 3.1 Extend frontend admin settings API types and form state with image studio available group IDs.
- [x] 3.2 Extend frontend admin settings API types and form state with the image tool declaration policy.
- [x] 3.3 Add a multi-select control in the image studio settings tab that loads administrator group options.
- [x] 3.4 Add a policy selector in the image studio settings tab for `strip`, `allow`, and `reject`.
- [x] 3.5 Save and restore the selected group IDs and declaration policy through the existing settings save flow.

## 4. Image Studio Experience

- [x] 4.1 Fetch or reuse the image studio available group IDs setting for the image studio page.
- [x] 4.2 Filter image studio API key choices by active status, OpenAI image eligibility, and the configured group allowlist.
- [x] 4.3 Update the empty state so users understand no key is available when no allowed group is configured.

## 5. Tests and Verification

- [x] 5.1 Add or update backend tests for settings normalization, serialization, job creation enforcement, and declaration policy behavior.
- [x] 5.2 Add or update frontend tests for admin settings form persistence and image studio API key filtering.
- [x] 5.3 Run targeted backend and frontend verification commands.

## 6. Merge Reconciliation

- [x] 6.1 Restore usage-card billing command fields, billing priority resolution, and production call-site propagation after the `main` service split.
- [ ] 6.2 Restore lossless admin settings round trips for legacy subscriptions, usage cards, default usage cards, and OpenAI long-context billing.
- [ ] 6.3 Reconcile passive native `image_generation` declarations with Codex `image_gen` namespace intent, including passthrough HTTP and WebSocket behavior.
- [ ] 6.4 Restore image-protocol preference propagation through the load-aware fallback scheduler.
- [ ] 6.5 Run focused red/green tests, full backend and frontend verification, and a post-merge code review.
