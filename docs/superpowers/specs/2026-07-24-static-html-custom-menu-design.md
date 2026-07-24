# Static HTML Custom Menu Page Design

## Goal

Allow an administrator to create a custom sidebar menu page from one static HTML file. The administrator can either upload a `.html` file or paste and edit HTML source in the settings UI. The rendered page supports static layout, inline CSS, images, media, tables, document anchors, internal navigation, and external links, but it never executes JavaScript.

Existing external URL menu items and local Markdown menu items remain compatible and keep their current behavior. This change does not attempt to bypass third-party iframe restrictions.

## Scope

Included:

- one HTML document per custom menu item;
- upload and source editing in the admin settings UI;
- an exact preview using the production renderer;
- persistent file storage under the configured data directory;
- authenticated, visibility-aware delivery to users;
- sanitization, iframe isolation, and deterministic link behavior;
- safe update and deletion ordering;
- backward compatibility for HTTP(S) and `md:slug` items.

Excluded:

- JavaScript, inline event handlers, forms, or automatic redirects;
- ZIP archives or accompanying local asset directories;
- a proxy intended to make arbitrary third-party sites embeddable;
- changing the existing external URL iframe behavior;
- a general-purpose website builder or revision history.

## Data Model And Compatibility

Extend `CustomMenuItem` with an optional `content_type` field whose normalized values are:

- `url`: an external HTTP(S) page;
- `markdown`: an existing local Markdown page;
- `html`: a managed static HTML page.

Continue using the optional `page_slug` field for local pages. New HTML items receive a generated slug that satisfies the existing page slug rules and is not exposed as a required administrator input.

Compatibility inference applies when `content_type` is absent:

- a URL beginning with `md:` is `markdown`, and its suffix is the slug;
- an absolute HTTP(S) URL is `url`;
- an explicit `html:<slug>` value is accepted as an HTML compatibility form, but the settings UI stores the explicit type and slug for new items.

The backend remains the authority for normalization and validation. It accepts older saved menu JSON without requiring a migration and emits normalized public settings that include `content_type` and `page_slug`.

## Persistent Storage

Store HTML documents at:

```text
<pricing.data_dir>/pages/<slug>.html
```

The default container configuration resolves this directory beneath `/app/data`, which is already persisted by either the `sub2api_data` named volume or the host `deploy/data` bind mount. Normal image pulls and container recreation therefore retain HTML pages.

Each document has a maximum raw size of 1 MiB and must be valid UTF-8. Slugs use the existing restricted alphanumeric, underscore, and hyphen format and remain within the existing length limit. Path resolution must reject traversal, encoded traversal, separators, and symlink escapes.

Saving uses a temporary file in the same directory followed by an atomic rename. A failed write leaves the previous document intact. Container shutdown does not require a flush because a successful response is returned only after the rename completes.

Operators can still lose page data by deleting the Docker volume, running `docker compose down -v`, or moving to another server without copying the persisted data directory. Documentation should call out the existing `/app/data` backup boundary.

## Backend API

Add administrator-only endpoints for HTML page management under the existing admin authentication and compliance guards:

```text
GET    /api/v1/admin/pages/:slug/html
PUT    /api/v1/admin/pages/:slug/html
DELETE /api/v1/admin/pages/:slug/html
```

`PUT` accepts one UTF-8 HTML document, enforces the 1 MiB limit, and writes it atomically. `GET` returns the raw source as non-executable plain text for editing. `DELETE` only removes an `.html` file and must never remove a same-named Markdown file or directory.

Add an authenticated user endpoint:

```text
GET /api/v1/pages/:slug/html
```

It serves raw source as `text/plain; charset=utf-8`, not `text/html`. Before reading the file it verifies that a configured custom menu item references the slug as HTML and that the authenticated user satisfies the item's `user` or `admin` visibility. A missing, unreferenced, or unauthorized page returns the same not-found response so the endpoint does not disclose private slugs.

The existing Markdown routes remain unchanged. The HTML endpoints should reuse shared slug validation, containment checks, file-size constants, and visibility lookup helpers instead of duplicating security-sensitive path logic.

## Admin Experience

The custom menu editor gains a content type control. It presents external URL, Markdown, and HTML modes while preserving the current fields for name, visibility, icon, and ordering.

HTML mode replaces the URL field with an "Edit HTML" action and a compact saved/unsaved/error status. The editor dialog provides:

- a source editor for pasting and editing HTML;
- an upload action that accepts one `.html` file and replaces the editor contents only after extension, size, and UTF-8 validation;
- a preview rendered with the same sanitizer, link transformer, CSP, and iframe component used by the user page;
- an explicit close action that warns before discarding unsaved editor changes.

Selecting a file does not immediately change the live page. The source remains local until the administrator saves system settings. Switching away from HTML mode preserves the draft during the current editing session but excludes it from the save request unless HTML mode is restored.

New HTML menu items receive an internal slug before their first upload. The UI does not derive identity from the mutable menu label or uploaded filename.

## Save And Delete Flow

The frontend keeps dirty HTML source keyed by page slug. When the administrator saves system settings:

1. validate every menu item and dirty HTML document locally;
2. upload dirty HTML documents through the admin API;
3. stop without changing menu settings if any HTML write fails;
4. save the normalized custom menu configuration only after all required HTML writes succeed;
5. reload settings and clear dirty state after the menu save succeeds.

Uploading first guarantees that a successfully saved menu never points to a missing newly created document. If the later settings update fails, an uploaded page may be orphaned, but it remains inaccessible because the user endpoint requires a matching configured menu item. A retry reuses the same slug and content.

When an HTML menu item is removed, save the menu configuration first. After that succeeds, delete its HTML file only when no remaining menu item references the slug. A deletion failure is reported as cleanup failure without rolling back the already valid menu configuration. The unreferenced file remains inaccessible and can be removed by a later retry.

For an existing live HTML page, an HTML upload can become visible before an unrelated menu setting failure is returned. The UI must state which phase failed and retain the draft until both phases succeed so a retry is deterministic. This is an accepted trade-off of file storage without a cross-database/filesystem transaction.

## Rendering And Isolation

The user custom page fetches HTML source with the existing bearer token and passes it through a shared static HTML renderer. The renderer is also used by the admin preview.

Rendering steps:

1. parse and sanitize the document with DOMPurify;
2. remove active or navigation-triggering markup;
3. normalize allowed links;
4. inject a restrictive content security policy into the rendered document;
5. assign the result to a sandboxed iframe using `srcdoc`.

The iframe does not receive `allow-scripts` or `allow-same-origin`. CSS and document layout are consequently isolated from the Sub2API application. The sandbox may grant only the minimum user-initiated navigation capabilities required by the link policy.

The static allowlist includes common document structure, headings, paragraphs, lists, tables, code, images, audio/video, semantic containers, `<style>` blocks, and style attributes. External stylesheets and local sibling files are not resolved. Images and media may use HTTP(S) or embedded `data:` sources.

Explicitly remove or forbid:

- `script`, nested `iframe`, `object`, `embed`, `base`, and refresh-capable metadata;
- forms and interactive form controls;
- all `on*` event attributes;
- `javascript:` and other executable URL schemes;
- script-capable SVG or equivalent active embedded content;
- automatic top-level navigation.

The iframe document CSP denies scripts, frames, forms, connections, and plugins. It permits only the image, media, font, and inline style sources required by the supported static feature set.

## Link Policy

Transform links after sanitization:

- `#fragment` stays within the HTML iframe and provides document anchor navigation;
- root-relative application paths such as `/orders` target the top application window and require a user click;
- absolute HTTP(S) links open in a new tab with `rel="noopener noreferrer"`;
- supported `mailto:` and `tel:` links remain user-initiated external actions;
- relative file links, executable schemes, malformed URLs, and automatic redirects are removed or made inert.

The preview applies the same policy. No preview-only permissions are allowed.

## Error Handling

Admin-facing errors distinguish invalid extension, invalid UTF-8, oversized content, invalid slug, write failure, settings failure, and post-save cleanup failure. A failed atomic write preserves the last successful HTML document.

The user page uses the existing loading state and shows a localized "page temporarily unavailable" state for missing or unreadable HTML. It never renders unsanitized source as an error fallback. Unauthorized and missing documents remain indistinguishable at the API boundary.

The admin editor rejects a document when the shared renderer produces no visible content. The HTML management API separately rejects an empty or whitespace-only source. A caller that bypasses the editor can store inert markup, but it still cannot execute because the user endpoint returns plain text and the user page always applies the sanitizer and sandbox.

## Testing

Backend coverage:

- normalize legacy HTTP(S) and `md:slug` menu items;
- validate HTML content type, slug, size, and UTF-8 encoding;
- enforce admin authentication and compliance guards on management endpoints;
- enforce JWT authentication and user/admin visibility on reads;
- return HTML source with a non-executable content type;
- reject traversal, encoded traversal, separators, and symlink escapes;
- prove atomic replacement keeps the old file after a failed write;
- delete only unreferenced `.html` files;
- retain existing Markdown behavior and same-named `.md` files.

Frontend coverage:

- select URL, Markdown, and HTML modes without corrupting existing values;
- upload a valid document and reject invalid extension, encoding, and size;
- edit, preview, preserve, discard, save, retry, and delete HTML drafts;
- remove active tags, event handlers, dangerous URLs, forms, and redirects;
- keep CSS isolated inside an iframe without script or same-origin sandbox permissions;
- handle fragment, application, external, mail, and telephone links as specified;
- render admin preview and user output through the same renderer;
- preserve current external URL iframe and Markdown rendering behavior.

Focused integration coverage should create an HTML menu item, persist its file in a temporary data directory, read it as an authorized user, deny it to the wrong role, update it, and remove the menu before deleting the file. Deployment verification should confirm that the standard Compose definitions mount the directory containing `pages/` at `/app/data`.

## Acceptance Criteria

- An administrator can add an HTML custom menu item by uploading one `.html` file or pasting source.
- The same sanitized preview is shown in the editor and to an authorized user.
- Static styling and supported links work without JavaScript execution or application-style leakage.
- Existing URL and Markdown items behave as before.
- HTML survives normal container replacement because it is stored beneath the persisted data directory.
- Failed writes preserve the previous page, unauthorized pages are not disclosed, and removed menu pages become inaccessible before file cleanup.
