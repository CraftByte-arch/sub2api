# Static HTML Custom Menu Pages Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let administrators upload or paste one static HTML document for a custom sidebar menu item and render it for authorized users without JavaScript, CSS leakage, or loss during normal container replacement.

**Architecture:** Extend custom menu items with an explicit content type and local page slug while preserving legacy HTTP(S) and `md:slug` records. Store HTML beneath the existing persistent page data directory, expose guarded plain-text management/read APIs, and render one shared DOMPurify-generated document inside a sandboxed `srcdoc` iframe in both admin preview and the user page.

**Tech Stack:** Go, Gin, filesystem atomic rename, Vue 3 `<script setup>`, TypeScript, Axios, DOMPurify, Vitest, Vue Test Utils, Tailwind CSS, vue-i18n.

---

## File Structure

**Create:**

- `backend/internal/handler/page_store.go`: validated paths, UTF-8/size checks, atomic writes, reads, and deletes.
- `backend/internal/handler/page_store_test.go`: storage, traversal, symlink, UTF-8, size, and failed-rename coverage.
- `backend/internal/handler/admin/setting_handler_custom_menu_test.go`: menu content-type normalization and validation.
- `backend/internal/handler/dto/settings_custom_menu_test.go`: legacy JSON parsing and normalized public DTO coverage.
- `frontend/src/utils/staticHtml.ts`: the only static HTML sanitizer, CSP builder, and link transformer.
- `frontend/src/utils/__tests__/staticHtml.spec.ts`: executable markup, CSS, CSP, and link-policy tests.
- `frontend/src/components/common/StaticHtmlFrame.vue`: shared sandboxed `srcdoc` iframe.
- `frontend/src/components/common/__tests__/StaticHtmlFrame.spec.ts`: sandbox and source binding tests.
- `frontend/src/api/pages.ts`: authenticated HTML page read API.
- `frontend/src/api/admin/pages.ts`: admin HTML source read/write/delete API.
- `frontend/src/api/__tests__/pages.spec.ts`: user/admin API contract tests.
- `frontend/src/components/admin/settings/StaticHtmlEditorDialog.vue`: upload, source edit, preview, and discard confirmation.
- `frontend/src/components/admin/settings/__tests__/StaticHtmlEditorDialog.spec.ts`: editor and file validation tests.
- `frontend/src/views/user/__tests__/CustomPageView.spec.ts`: HTML mode and URL/Markdown regression tests.

**Modify:**

- `backend/internal/handler/dto/settings.go`: content-type metadata and compatibility helpers.
- `backend/internal/handler/admin/setting_handler_update.go`: normalize and validate all menu modes.
- `backend/internal/handler/page_handler.go`: HTML handlers, type-aware visibility, and routes.
- `backend/internal/handler/page_handler_test.go`: lifecycle, visibility, media type, and delete-reference tests.
- `backend/internal/server/router.go`: pass audit middleware into page routes.
- `frontend/src/types/index.ts`: menu content type and page slug fields.
- `frontend/src/api/index.ts`, `frontend/src/api/admin/index.ts`: export page APIs.
- `frontend/src/views/admin/SettingsView.vue`: content controls, drafts, ordered save, and cleanup.
- `frontend/src/views/admin/__tests__/SettingsView.spec.ts`: ordering, retry, compatibility, and deletion tests.
- `frontend/src/views/user/CustomPageView.vue`: fetch HTML and use the shared frame.
- Chinese and English settings/misc locale files: all new visible text.
- `README_CN.md`, `deploy/README.md`: persistent location and volume-removal warning.

**Do not modify:** third-party iframe behavior, database schema, Markdown storage format, or existing Docker volume definitions.

### Task 1: Canonical Custom Menu Content Types

**Files:**
- Modify: `backend/internal/handler/dto/settings.go`
- Modify: `backend/internal/handler/admin/setting_handler_update.go:1110-1195`
- Create: `backend/internal/handler/admin/setting_handler_custom_menu_test.go`
- Create: `backend/internal/handler/dto/settings_custom_menu_test.go`

- [ ] **Step 1: Write failing normalization and validation tests**

Use the existing `newStepUpSwitchTestHandler` and `doUpdateSettings` helpers. Assert stored JSON, not only the response:

```go
func TestUpdateSettingsNormalizesCustomMenuContentTypes(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	rec := doUpdateSettings(t, h, map[string]any{
		"custom_menu_items": []map[string]any{
			{"id": "docs", "label": "Docs", "url": "https://example.com", "visibility": "user"},
			{"id": "guide", "label": "Guide", "url": "md:guide", "visibility": "user"},
			{"id": "about", "label": "About", "url": "", "content_type": "html", "page_slug": "html-about", "visibility": "admin"},
		},
	}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	var items []dto.CustomMenuItem
	require.NoError(t, json.Unmarshal([]byte(repo.values[service.SettingKeyCustomMenuItems]), &items))
	require.Equal(t, dto.CustomMenuContentURL, items[0].ContentType)
	require.Equal(t, dto.CustomMenuContentMarkdown, items[1].ContentType)
	require.Equal(t, "guide", items[1].PageSlug)
	require.Equal(t, dto.CustomMenuContentHTML, items[2].ContentType)
	require.Equal(t, "html:html-about", items[2].URL)
}

func TestUpdateSettingsRejectsInvalidCustomMenuPageReference(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, map[string]string{})
	for _, item := range []map[string]any{
		{"id": "x", "label": "X", "content_type": "video", "url": "", "visibility": "user"},
		{"id": "x", "label": "X", "content_type": "html", "page_slug": "../x", "visibility": "user"},
		{"id": "x", "label": "X", "content_type": "html", "page_slug": "", "visibility": "user"},
	} {
		rec := doUpdateSettings(t, h, map[string]any{"custom_menu_items": []map[string]any{item}}, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code)
	}
}

func TestParseCustomMenuItemsNormalizesLegacyReferences(t *testing.T) {
	items := ParseCustomMenuItems(`[{"id":"docs","url":"https://example.com"},{"id":"guide","url":"md:guide"},{"id":"about","url":"html:about"}]`)
	require.Equal(t, CustomMenuContentURL, items[0].ContentType)
	require.Equal(t, CustomMenuContentMarkdown, items[1].ContentType)
	require.Equal(t, "guide", items[1].PageSlug)
	require.Equal(t, CustomMenuContentHTML, items[2].ContentType)
	require.Equal(t, "about", items[2].PageSlug)
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `cd backend && go test -tags=unit ./internal/handler/admin -run 'TestUpdateSettings(Normalizes|RejectsInvalidCustomMenu)' -count=1`

Expected: FAIL because `ContentType` and its constants do not exist and HTML is rejected as a URL.

- [ ] **Step 3: Add DTO compatibility helpers**

```go
const (
	CustomMenuContentURL      = "url"
	CustomMenuContentMarkdown = "markdown"
	CustomMenuContentHTML     = "html"
)

type CustomMenuItem struct {
	ID string `json:"id"`; Label string `json:"label"`; IconSVG string `json:"icon_svg"`
	URL string `json:"url"`; ContentType string `json:"content_type,omitempty"`
	PageSlug string `json:"page_slug,omitempty"`; Visibility string `json:"visibility"`
	SortOrder int `json:"sort_order"`
}

func (item CustomMenuItem) EffectiveContentType() string {
	if item.ContentType != "" { return strings.ToLower(strings.TrimSpace(item.ContentType)) }
	url := strings.TrimSpace(item.URL)
	if strings.HasPrefix(url, "html:") { return CustomMenuContentHTML }
	if strings.HasPrefix(url, "md:") || item.PageSlug != "" { return CustomMenuContentMarkdown }
	return CustomMenuContentURL
}

func (item CustomMenuItem) EffectivePageSlug() string {
	if slug := strings.TrimSpace(item.PageSlug); slug != "" { return slug }
	url := strings.TrimSpace(item.URL)
	if strings.HasPrefix(url, "html:") { return strings.TrimPrefix(url, "html:") }
	if strings.HasPrefix(url, "md:") { return strings.TrimPrefix(url, "md:") }
	return ""
}

func NormalizeCustomMenuItem(item CustomMenuItem) CustomMenuItem {
	item.ContentType = item.EffectiveContentType()
	if item.ContentType == CustomMenuContentMarkdown || item.ContentType == CustomMenuContentHTML {
		item.PageSlug = item.EffectivePageSlug()
	}
	return item
}
```

Use normal one-field-per-line Go formatting in the implementation; the compact struct above only highlights the exact fields.
Update `ParseCustomMenuItems` to call `NormalizeCustomMenuItem` for every decoded record. That makes admin and public API DTOs explicit even when stored JSON is legacy; first-load frontend injection remains compatible through the same client-side inference used for older deployments.

- [ ] **Step 4: Normalize settings items before marshaling**

Define `menuPageSlugPattern = regexp.MustCompile("^[a-zA-Z0-9][a-zA-Z0-9_-]*$")` next to `menuItemIDPattern`, then use an exhaustive switch:

```go
contentType := item.EffectiveContentType()
switch contentType {
case dto.CustomMenuContentURL:
	urlTrimmed := strings.TrimSpace(item.URL)
	if urlTrimmed == "" || config.ValidateAbsoluteHTTPURL(urlTrimmed) != nil {
		response.BadRequest(c, "Custom menu item URL must be an absolute http(s) URL"); return
	}
	item.URL, item.PageSlug = urlTrimmed, ""
case dto.CustomMenuContentMarkdown, dto.CustomMenuContentHTML:
	slug := item.EffectivePageSlug()
	if len(slug) > 64 || !menuPageSlugPattern.MatchString(slug) {
		response.BadRequest(c, "Custom menu item page slug is invalid"); return
	}
	item.PageSlug = slug
	if contentType == dto.CustomMenuContentMarkdown { item.URL = "md:" + slug } else { item.URL = "html:" + slug }
default:
	response.BadRequest(c, "Custom menu item content type must be 'url', 'markdown', or 'html'"); return
}
item.ContentType = contentType
items[i] = item
```

- [ ] **Step 5: Run focused tests and commit**

Run: `cd backend && go test -tags=unit ./internal/handler/admin ./internal/handler/dto -run 'Test(UpdateSettings(Normalizes|RejectsInvalidCustomMenu)|ParseCustomMenuItemsNormalizes)' -count=1`

Expected: PASS.

```bash
git add backend/internal/handler/dto/settings.go backend/internal/handler/dto/settings_custom_menu_test.go backend/internal/handler/admin/setting_handler_update.go backend/internal/handler/admin/setting_handler_custom_menu_test.go
git commit -m "feat(settings): define custom menu content types"
```

### Task 2: Persistent Static HTML Page Store

**Files:**
- Create: `backend/internal/handler/page_store.go`
- Create: `backend/internal/handler/page_store_test.go`
- Modify: `backend/internal/handler/page_handler.go`

- [ ] **Step 1: Write failing store tests**

Cover lifecycle, invalid UTF-8, the 1 MiB boundary, invalid slugs, symlink reads, and forced rename failure:

```go
func TestPageStoreWriteHTMLAtomically(t *testing.T) {
	store := newPageStore(t.TempDir())
	require.NoError(t, store.writeHTML("about", []byte("<h1>old</h1>")))
	originalRename := store.renameFile
	store.renameFile = func(_, _ string) error { return errors.New("rename failed") }
	require.ErrorContains(t, store.writeHTML("about", []byte("<h1>new</h1>")), "rename failed")
	store.renameFile = originalRename
	got, err := store.read("about", pageExtensionHTML)
	require.NoError(t, err)
	require.Equal(t, "<h1>old</h1>", string(got))
}

func TestPageStoreRejectsInvalidHTML(t *testing.T) {
	store := newPageStore(t.TempDir())
	require.ErrorIs(t, store.writeHTML("about", nil), errPageEmpty)
	require.ErrorIs(t, store.writeHTML("about", []byte{0xff}), errPageInvalidUTF8)
	require.ErrorIs(t, store.writeHTML("../about", []byte("x")), errInvalidPageSlug)
	require.ErrorIs(t, store.writeHTML("about", bytes.Repeat([]byte("x"), maxPageFileSize+1)), errPageTooLarge)
}
```

- [ ] **Step 2: Run store tests and verify RED**

Run: `cd backend && go test -tags=unit ./internal/handler -run 'TestPageStore' -count=1`

Expected: FAIL because the store and errors are undefined.

- [ ] **Step 3: Implement the page store**

```go
const ( maxPageFileSize = 1 << 20; pageExtensionHTML = ".html"; pageExtensionMD = ".md" )
var (
	errInvalidPageSlug = errors.New("invalid page slug")
	errPageNotFound = errors.New("page not found")
	errPageTooLarge = errors.New("page too large")
	errPageInvalidUTF8 = errors.New("page must be valid UTF-8")
	errPageEmpty = errors.New("page cannot be empty")
)

type pageStore struct { pagesDir string; renameFile func(string, string) error }
func newPageStore(dataDir string) *pageStore {
	return &pageStore{pagesDir: filepath.Join(dataDir, "pages"), renameFile: os.Rename}
}
var validPageSlugPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)
func validPageSlug(slug string) bool { return len(slug) <= 64 && validPageSlugPattern.MatchString(slug) }

func (s *pageStore) writeHTML(slug string, content []byte) error {
	if !validPageSlug(slug) { return errInvalidPageSlug }
	if len(content) == 0 || strings.TrimSpace(string(content)) == "" { return errPageEmpty }
	if len(content) > maxPageFileSize { return errPageTooLarge }
	if !utf8.Valid(content) { return errPageInvalidUTF8 }
	if err := os.MkdirAll(s.pagesDir, 0o755); err != nil { return fmt.Errorf("create pages directory: %w", err) }
	target := filepath.Join(s.pagesDir, slug+pageExtensionHTML)
	tmp, err := os.CreateTemp(s.pagesDir, "."+slug+"-*.tmp")
	if err != nil { return fmt.Errorf("create temporary page: %w", err) }
	tmpName := tmp.Name(); defer os.Remove(tmpName)
	if err := tmp.Chmod(0o644); err != nil { tmp.Close(); return err }
	if _, err := tmp.Write(content); err != nil { tmp.Close(); return err }
	if err := tmp.Sync(); err != nil { tmp.Close(); return err }
	if err := tmp.Close(); err != nil { return err }
	if err := s.renameFile(tmpName, target); err != nil { return fmt.Errorf("replace page: %w", err) }
	return nil
}
```

`read` must use `os.Lstat`, `filepath.EvalSymlinks`, and `isPathWithinBase` before `os.ReadFile`. `deleteHTML` removes only `<slug>.html`. Move the existing slug regex and size constant from `page_handler.go` into this storage boundary.

Replace the handler's raw directory field with the store and use `h.store.pagesDir` in the existing image helper:

```go
type PageHandler struct { store *pageStore; settingService *service.SettingService }
func NewPageHandler(dataDir string, settingService *service.SettingService) *PageHandler {
	return &PageHandler{store: newPageStore(dataDir), settingService: settingService}
}
```

- [ ] **Step 4: Run storage and existing image path tests**

Run: `cd backend && go test -tags=unit ./internal/handler -run 'Test(PageStore|CleanPageImage|ResolvePageImage)' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the store**

```bash
git add backend/internal/handler/page_store.go backend/internal/handler/page_store_test.go backend/internal/handler/page_handler.go
git commit -m "feat(pages): add persistent static HTML store"
```

### Task 3: Guarded HTML Page APIs

**Files:**
- Modify: `backend/internal/handler/page_handler.go`
- Modify: `backend/internal/handler/page_handler_test.go`
- Modify: `backend/internal/server/router.go:127`

- [ ] **Step 1: Write failing lifecycle and visibility tests**

Build a real `SettingService` around a repo stub and directly exercise Gin handlers:

```go
func TestPageHandlerHTMLLifecycleAndVisibility(t *testing.T) {
	repo := &pageSettingRepoStub{values: map[string]string{
		service.SettingKeyCustomMenuItems: `[{"id":"about","label":"About","url":"html:about","content_type":"html","page_slug":"about","visibility":"user"}]`,
	}}
	h := NewPageHandler(t.TempDir(), service.NewSettingService(repo, &config.Config{}))
	put := performPageRequest(t, h.PutAdminHTMLPage, http.MethodPut, "/api/v1/admin/pages/about/html", "about", "<h1>About</h1>", service.RoleAdmin)
	require.Equal(t, http.StatusOK, put.Code)
	get := performPageRequest(t, h.GetHTMLPage, http.MethodGet, "/api/v1/pages/about/html", "about", "", service.RoleUser)
	require.Equal(t, http.StatusOK, get.Code)
	require.Equal(t, "text/plain; charset=utf-8", get.Header().Get("Content-Type"))
	require.Equal(t, "<h1>About</h1>", get.Body.String())
	blocked := performPageRequest(t, h.DeleteAdminHTMLPage, http.MethodDelete, "/api/v1/admin/pages/about/html", "about", "", service.RoleAdmin)
	require.Equal(t, http.StatusConflict, blocked.Code)
}
```

Define the fixture in the same test file so no database is required:

```go
type pageSettingRepoStub struct {
	service.SettingRepository
	values map[string]string
}

func (r *pageSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok { return "", service.ErrSettingNotFound }
	return value, nil
}

func performPageRequest(t *testing.T, handler func(*gin.Context), method, target, slug, body, role string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "slug", Value: slug}}
	c.Set(string(middleware.ContextKeyUserRole), role)
	c.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "text/html; charset=utf-8")
	handler(c)
	return rec
}
```

Add table tests for admin visibility, wrong content type with the same slug, unreferenced source, oversized body, invalid UTF-8, and admin orphan reads.

- [ ] **Step 2: Run handler tests and verify RED**

Run: `cd backend && go test -tags=unit ./internal/handler -run 'TestPageHandlerHTML' -count=1`

Expected: FAIL because HTML methods and type-aware lookup do not exist.

- [ ] **Step 3: Make references content-type aware**

```go
func (h *PageHandler) findPageReference(c *gin.Context, slug, contentType string) (string, bool) {
	if h.settingService == nil { return "", false }
	for _, item := range dto.ParseCustomMenuItems(h.settingService.GetCustomMenuItemsRaw(c.Request.Context())) {
		if item.EffectiveContentType() == contentType && item.EffectivePageSlug() == slug {
			return item.Visibility, true
		}
	}
	return "", false
}
```

Use `markdown` explicitly in existing Markdown and image authorization so an HTML reference cannot grant Markdown access.

- [ ] **Step 4: Implement admin and user handlers**

```go
func (h *PageHandler) PutAdminHTMLPage(c *gin.Context) {
	slug := c.Param("slug")
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxPageFileSize+1))
	if err != nil { response.BadRequest(c, "Failed to read HTML page"); return }
	if err := h.store.writeHTML(slug, body); err != nil { h.writePageError(c, err); return }
	response.Success(c, gin.H{"slug": slug})
}

func (h *PageHandler) GetHTMLPage(c *gin.Context) {
	slug := c.Param("slug")
	if !h.checkPageVisibility(c, slug, dto.CustomMenuContentHTML) {
		c.JSON(http.StatusNotFound, gin.H{"error": "page not found"}); return
	}
	content, err := h.store.read(slug, pageExtensionHTML)
	if err != nil { h.writePageError(c, err); return }
	c.Data(http.StatusOK, "text/plain; charset=utf-8", content)
}
```

`DeleteAdminHTMLPage` returns 409 while any normalized HTML item references the slug. Once unreferenced, it deletes only `.html`. `GetAdminHTMLPage` reads without a reference so orphan recovery works.

Map every store error explicitly and do not expose filesystem paths:

```go
func (h *PageHandler) writePageError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errInvalidPageSlug), errors.Is(err, errPageInvalidUTF8), errors.Is(err, errPageEmpty):
		response.BadRequest(c, err.Error())
	case errors.Is(err, errPageTooLarge):
		response.Error(c, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, errPageNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "page not found"})
	default:
		response.InternalError(c, "failed to access page")
	}
}

func (h *PageHandler) htmlPageReferenced(c *gin.Context, slug string) bool {
	for _, item := range dto.ParseCustomMenuItems(h.settingService.GetCustomMenuItemsRaw(c.Request.Context())) {
		if item.EffectiveContentType() == dto.CustomMenuContentHTML && item.EffectivePageSlug() == slug { return true }
	}
	return false
}
```

- [ ] **Step 5: Register authenticated and audited routes**

Change `RegisterPageRoutes` to accept `auditLog gin.HandlerFunc`:

```go
pages.GET("/:slug/html", h.GetHTMLPage)
adminPages := v1.Group("/admin/pages")
adminPages.Use(adminAuth, auditLog, middleware2.AdminComplianceGuard(settingService))
adminPages.GET("/:slug/html", h.GetAdminHTMLPage)
adminPages.PUT("/:slug/html", h.PutAdminHTMLPage)
adminPages.DELETE("/:slug/html", h.DeleteAdminHTMLPage)
```

Pass `gin.HandlerFunc(auditLog)` from `router.go`. Keep existing Markdown and image routes unchanged.

- [ ] **Step 6: Run focused/package tests and commit**

Run:

```bash
cd backend
go test -tags=unit ./internal/handler -run 'Test(PageHandlerHTML|PageStore|CleanPageImage|ResolvePageImage)' -count=1
go test -tags=unit ./internal/handler ./internal/server -run 'Page|APIContract' -count=1
```

Expected: PASS; the second command proves server wiring compiles.

```bash
git add backend/internal/handler/page_handler.go backend/internal/handler/page_handler_test.go backend/internal/server/router.go
git commit -m "feat(pages): expose guarded static HTML APIs"
```

### Task 4: Shared Sanitizer And Sandbox Frame

**Files:**
- Create: `frontend/src/utils/staticHtml.ts`
- Create: `frontend/src/utils/__tests__/staticHtml.spec.ts`
- Create: `frontend/src/components/common/StaticHtmlFrame.vue`
- Create: `frontend/src/components/common/__tests__/StaticHtmlFrame.spec.ts`

- [ ] **Step 1: Write failing sanitizer and link tests**

```ts
it('keeps static layout while removing active content', () => {
  const doc = buildStaticHtmlDocument(`<style>h1{color:red}</style><h1 onclick="alert(1)">Hello</h1><script>alert(1)</script><form><input></form>`)
  expect(doc).toContain('h1{color:red}')
  expect(doc).toContain('<h1>Hello</h1>')
  expect(doc).not.toMatch(/script|onclick|<form|<input/i)
  expect(doc).toContain("script-src 'none'")
})

it('normalizes supported links', () => {
  const doc = parseBuiltDocument(`<a id="f" href="#part">F</a><a id="app" href="/orders">A</a><a id="ext" href="https://example.com">E</a><a id="rel" href="other.html">R</a><a id="bad" href="javascript:alert(1)">B</a>`)
  expect(doc.querySelector('#f')?.getAttribute('target')).toBeNull()
  expect(doc.querySelector('#app')?.getAttribute('target')).toBe('_top')
  expect(doc.querySelector('#ext')?.getAttribute('target')).toBe('_blank')
  expect(doc.querySelector('#ext')?.getAttribute('rel')).toBe('noopener noreferrer')
  expect(doc.querySelector('#rel')?.hasAttribute('href')).toBe(false)
  expect(doc.querySelector('#bad')?.hasAttribute('href')).toBe(false)
})

function parseBuiltDocument(source: string): Document {
  return new DOMParser().parseFromString(buildStaticHtmlDocument(source), 'text/html')
}
```

- [ ] **Step 2: Run utility tests and verify RED**

Run: `cd frontend && pnpm test:run src/utils/__tests__/staticHtml.spec.ts`

Expected: FAIL because the utility is missing.

- [ ] **Step 3: Implement the only sanitizer/document builder**

```ts
export const STATIC_HTML_SANDBOX = 'allow-popups allow-popups-to-escape-sandbox allow-top-navigation-by-user-activation'
const CSP = ["default-src 'none'", "script-src 'none'", "style-src 'unsafe-inline'", 'img-src http: https: data:', 'media-src http: https: data:', 'font-src http: https: data:', "connect-src 'none'", "frame-src 'none'", "object-src 'none'", "form-action 'none'", "base-uri 'none'"].join('; ')

export function buildStaticHtmlDocument(source: string): string {
  const sanitized = DOMPurify.sanitize(source, {
    WHOLE_DOCUMENT: true,
    ADD_TAGS: ['style'],
    FORBID_TAGS: ['script', 'iframe', 'object', 'embed', 'base', 'meta', 'link', 'form', 'input', 'button', 'select', 'textarea', 'option', 'svg', 'math'],
    FORBID_ATTR: ['srcdoc', 'action', 'formaction'],
    ALLOW_DATA_ATTR: false,
  })
  const doc = new DOMParser().parseFromString(sanitized, 'text/html')
  normalizeStaticLinks(doc)
  const meta = doc.createElement('meta'); meta.httpEquiv = 'Content-Security-Policy'; meta.content = CSP; doc.head.prepend(meta)
  return `<!doctype html>\n${doc.documentElement.outerHTML}`
}
```

`normalizeStaticLinks` implements fragment, root-relative application, HTTP(S), mail/tel, and rejected-relative behavior. Export `hasVisibleStaticHtml` using sanitized body text plus static media selectors.

Implement those helpers without URL-string substitution:

```ts
function normalizeStaticLinks(doc: Document): void {
  doc.querySelectorAll<HTMLAnchorElement>('a').forEach((anchor) => {
    anchor.removeAttribute('ping'); anchor.removeAttribute('download')
    const href = anchor.getAttribute('href')?.trim() || ''
    anchor.removeAttribute('target'); anchor.removeAttribute('rel')
    if (href.startsWith('#')) return
    if (href.startsWith('/') && !href.startsWith('//')) { anchor.target = '_top'; return }
    try {
      const parsed = new URL(href)
      if (parsed.protocol === 'http:' || parsed.protocol === 'https:') {
        anchor.target = '_blank'; anchor.rel = 'noopener noreferrer'; return
      }
      if (parsed.protocol === 'mailto:' || parsed.protocol === 'tel:') {
        anchor.target = '_blank'; anchor.rel = 'noopener noreferrer'; return
      }
    } catch { /* relative and malformed links are removed below */ }
    anchor.removeAttribute('href')
  })
}

export function hasVisibleStaticHtml(source: string): boolean {
  const doc = new DOMParser().parseFromString(buildStaticHtmlDocument(source), 'text/html')
  return Boolean(doc.body.textContent?.trim() || doc.body.querySelector('img, audio, video, table, hr'))
}
```

- [ ] **Step 4: Write the failing frame test**

```ts
const wrapper = mount(StaticHtmlFrame, { props: { source: '<h1>Hello</h1>', title: 'Preview' } })
const iframe = wrapper.get('iframe')
expect(iframe.attributes('sandbox')).toBe(STATIC_HTML_SANDBOX)
expect(iframe.attributes('sandbox')).not.toContain('allow-scripts')
expect(iframe.attributes('sandbox')).not.toContain('allow-same-origin')
expect(iframe.attributes('srcdoc')).toContain('<h1>Hello</h1>')
```

Run: `cd frontend && pnpm test:run src/components/common/__tests__/StaticHtmlFrame.spec.ts`

Expected: FAIL because the component is missing.

- [ ] **Step 5: Implement the stable frame, run tests, and commit**

```vue
<template><iframe class="block h-full w-full border-0 bg-white" :title="title" :sandbox="STATIC_HTML_SANDBOX" :srcdoc="srcdoc" referrerpolicy="no-referrer" /></template>
<script setup lang="ts">
import { computed } from 'vue'
import { buildStaticHtmlDocument, STATIC_HTML_SANDBOX } from '@/utils/staticHtml'
const props = defineProps<{ source: string; title: string }>()
const srcdoc = computed(() => buildStaticHtmlDocument(props.source))
</script>
```

Run: `cd frontend && pnpm test:run src/utils/__tests__/staticHtml.spec.ts src/components/common/__tests__/StaticHtmlFrame.spec.ts`

Expected: PASS.

```bash
git add frontend/src/utils/staticHtml.ts frontend/src/utils/__tests__/staticHtml.spec.ts frontend/src/components/common/StaticHtmlFrame.vue frontend/src/components/common/__tests__/StaticHtmlFrame.spec.ts
git commit -m "feat(frontend): add sandboxed static HTML renderer"
```

### Task 5: Typed HTML Page API Clients

**Files:**
- Modify: `frontend/src/types/index.ts:170-178`
- Create: `frontend/src/api/pages.ts`
- Create: `frontend/src/api/admin/pages.ts`
- Create: `frontend/src/api/__tests__/pages.spec.ts`
- Modify: `frontend/src/api/index.ts`
- Modify: `frontend/src/api/admin/index.ts`

- [ ] **Step 1: Write failing API contract tests**

Mock `apiClient` and lock down URL encoding, raw bodies, and media type:

```ts
const { get, put, del } = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), del: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get, put, delete: del } }))

it('reads user and admin HTML source as text', async () => {
  get.mockResolvedValue({ data: '<h1>About</h1>' })
  await expect(pagesAPI.getHtml('about page')).resolves.toBe('<h1>About</h1>')
  await expect(adminPagesAPI.getHtml('about page')).resolves.toBe('<h1>About</h1>')
  expect(get).toHaveBeenNthCalledWith(1, '/pages/about%20page/html', { responseType: 'text' })
  expect(get).toHaveBeenNthCalledWith(2, '/admin/pages/about%20page/html', { responseType: 'text' })
})

it('writes and deletes admin HTML source', async () => {
  put.mockResolvedValue({ data: { slug: 'about' } }); del.mockResolvedValue({ data: { slug: 'about' } })
  await adminPagesAPI.putHtml('about', '<h1>About</h1>'); await adminPagesAPI.deleteHtml('about')
  expect(put).toHaveBeenCalledWith('/admin/pages/about/html', '<h1>About</h1>', {
    headers: { 'Content-Type': 'text/html; charset=utf-8' },
  })
  expect(del).toHaveBeenCalledWith('/admin/pages/about/html')
})
```

- [ ] **Step 2: Run API tests and verify RED**

Run: `cd frontend && pnpm test:run src/api/__tests__/pages.spec.ts`

Expected: FAIL because both modules are missing.

- [ ] **Step 3: Add types and API modules**

```ts
export type CustomMenuContentType = 'url' | 'markdown' | 'html'
export interface CustomMenuItem {
  id: string; label: string; icon_svg: string; url: string
  content_type?: CustomMenuContentType; page_slug?: string
  visibility: 'user' | 'admin'; sort_order: number
}

export const pagesAPI = {
  async getHtml(slug: string): Promise<string> {
    const { data } = await apiClient.get<string>(`/pages/${encodeURIComponent(slug)}/html`, { responseType: 'text' })
    return data
  },
}

export const adminPagesAPI = {
  async getHtml(slug: string): Promise<string> {
    const { data } = await apiClient.get<string>(`/admin/pages/${encodeURIComponent(slug)}/html`, { responseType: 'text' })
    return data
  },
  async putHtml(slug: string, source: string): Promise<void> {
    await apiClient.put(`/admin/pages/${encodeURIComponent(slug)}/html`, source, { headers: { 'Content-Type': 'text/html; charset=utf-8' } })
  },
  async deleteHtml(slug: string): Promise<void> {
    await apiClient.delete(`/admin/pages/${encodeURIComponent(slug)}/html`)
  },
}
```

Export `pagesAPI` from the user barrel and expose `adminPagesAPI` as `adminAPI.pages`.

- [ ] **Step 4: Run tests/typecheck and commit**

Run: `cd frontend && pnpm test:run src/api/__tests__/pages.spec.ts && pnpm typecheck`

Expected: PASS.

```bash
git add frontend/src/types/index.ts frontend/src/api/pages.ts frontend/src/api/admin/pages.ts frontend/src/api/__tests__/pages.spec.ts frontend/src/api/index.ts frontend/src/api/admin/index.ts
git commit -m "feat(api): add static HTML page clients"
```

### Task 6: Static HTML Editor Dialog

**Files:**
- Create: `frontend/src/components/admin/settings/StaticHtmlEditorDialog.vue`
- Create: `frontend/src/components/admin/settings/__tests__/StaticHtmlEditorDialog.spec.ts`
- Modify: `frontend/src/i18n/locales/zh/admin/settings.ts`
- Modify: `frontend/src/i18n/locales/en/admin/settings.ts`

- [ ] **Step 1: Write failing editor tests**

Stub `BaseDialog`, `ConfirmDialog`, and `StaticHtmlFrame`. Cover paste/apply, production preview, valid replacement, wrong extension, over 1 MiB, invalid UTF-8, and unsaved close:

```ts
function mountEditor(source: string) {
  return mount(StaticHtmlEditorDialog, {
    props: { show: true, title: 'About', source },
    global: { mocks: { $t: (key: string) => key } },
  })
}

async function selectFile(wrapper: ReturnType<typeof mountEditor>, file: File) {
  const input = wrapper.get('input[type="file"]')
  Object.defineProperty(input.element, 'files', { configurable: true, value: [file] })
  await input.trigger('change')
  await flushPromises()
}

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

it('uploads UTF-8 HTML, previews it, and applies the source', async () => {
  const wrapper = mountEditor('<p>old</p>')
  await selectFile(wrapper, new File(['<h1>new</h1>'], 'about.html', { type: 'text/html' }))
  await wrapper.get('[data-testid="html-preview-tab"]').trigger('click')
  expect(wrapper.getComponent(StaticHtmlFrame).props('source')).toBe('<h1>new</h1>')
  await wrapper.get('[data-testid="html-editor-apply"]').trigger('click')
  expect(wrapper.emitted('apply')?.[0]).toEqual(['<h1>new</h1>'])
})

it('rejects invalid files without replacing the draft', async () => {
  const wrapper = mountEditor('<p>keep</p>')
  await selectFile(wrapper, new File(['x'], 'about.txt'))
  expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('<p>keep</p>')
  expect(wrapper.text()).toContain('admin.settings.customMenu.html.invalidFile')
})
```

- [ ] **Step 2: Run tests and verify RED**

Run: `cd frontend && pnpm test:run src/components/admin/settings/__tests__/StaticHtmlEditorDialog.spec.ts`

Expected: FAIL because the component is missing.

- [ ] **Step 3: Implement upload/source/preview behavior**

```ts
const MAX_HTML_BYTES = 1 << 20
async function onFileSelected(event: Event) {
  error.value = ''
  const file = (event.target as HTMLInputElement).files?.[0]
  if (!file) return
  if (!file.name.toLowerCase().endsWith('.html') || file.size > MAX_HTML_BYTES) {
    error.value = t('admin.settings.customMenu.html.invalidFile'); return
  }
  try {
    draft.value = new TextDecoder('utf-8', { fatal: true }).decode(await file.arrayBuffer())
  } catch {
    error.value = t('admin.settings.customMenu.html.invalidEncoding')
  }
}

function apply() {
  if (!hasVisibleStaticHtml(draft.value)) {
    error.value = t('admin.settings.customMenu.html.emptyAfterSanitize'); return
  }
  emit('apply', draft.value)
}
```

Use upload/close icons, a source/preview segmented control, and stable editor/preview heights. Render discard confirmation with the existing `ConfirmDialog`; preview only through `StaticHtmlFrame`.

Use this public component contract and reset the local draft whenever a new source is opened:

```ts
const props = defineProps<{ show: boolean; title: string; source: string }>()
const emit = defineEmits<{ (e: 'apply', source: string): void; (e: 'close'): void }>()
watch(() => [props.show, props.source] as const, ([show, source]) => {
  if (show) { draft.value = source; initialSource.value = source; error.value = '' }
}, { immediate: true })
```

- [ ] **Step 4: Add complete Chinese/English strings**

Add content types, edit, source, preview, upload, apply, saved/unsaved, invalid extension/size, invalid UTF-8, sanitized-empty, load failure, and discard confirmation strings. Leave no hard-coded visible English.

- [ ] **Step 5: Run tests/typecheck and commit**

Run: `cd frontend && pnpm test:run src/components/admin/settings/__tests__/StaticHtmlEditorDialog.spec.ts && pnpm typecheck`

Expected: PASS.

```bash
git add frontend/src/components/admin/settings/StaticHtmlEditorDialog.vue frontend/src/components/admin/settings/__tests__/StaticHtmlEditorDialog.spec.ts frontend/src/i18n/locales/zh/admin/settings.ts frontend/src/i18n/locales/en/admin/settings.ts
git commit -m "feat(settings): add static HTML editor dialog"
```

### Task 7: Admin Settings Save And Cleanup Flow

**Files:**
- Modify: `frontend/src/views/admin/SettingsView.vue:5995-6198, 8940-8947, 9860-9890, 10046-10135, 10344-10965`
- Modify: `frontend/src/views/admin/__tests__/SettingsView.spec.ts`

- [ ] **Step 1: Extend mocks and write failing ordered-save tests**

Add `getHtml`, `putHtml`, and `deleteHtml` to the hoisted admin API mock. Lock down invocation order:

```ts
function findCustomMenuCard(wrapper: ReturnType<typeof mountView>) {
  const card = wrapper.findAll('.card').find((node) => node.text().includes('admin.settings.customMenu.title'))
  expect(card).toBeDefined()
  return card!
}

it('uploads dirty HTML before saving its menu reference', async () => {
  const wrapper = mountView(); await flushPromises()
  const card = findCustomMenuCard(wrapper)
  await card.get('[data-testid="add-custom-menu-item"]').trigger('click')
  await card.get('[data-testid="custom-menu-content-type"]').setValue('html')
  await card.get('[data-testid="edit-custom-menu-html"]').trigger('click')
  await wrapper.getComponent(StaticHtmlEditorDialog).vm.$emit('apply', '<h1>About</h1>')
  await wrapper.find('form').trigger('submit.prevent'); await flushPromises()
  expect(putHtml).toHaveBeenCalledWith(expect.any(String), '<h1>About</h1>')
  expect(putHtml.mock.invocationCallOrder[0]).toBeLessThan(updateSettings.mock.invocationCallOrder[0])
  expect(updateSettings).toHaveBeenCalledWith(expect.objectContaining({
    custom_menu_items: [expect.objectContaining({ content_type: 'html', url: expect.stringMatching(/^html:/), page_slug: expect.any(String) })],
  }))
})
```

Also prove: PUT failure prevents settings update; update failure retains the draft for retry; removal updates settings before delete; cleanup failure reports after valid update; URL and `md:slug` require no HTML call.

- [ ] **Step 2: Run focused SettingsView tests and verify RED**

Run: `cd frontend && pnpm test:run src/views/admin/__tests__/SettingsView.spec.ts -t 'custom menu HTML'`

Expected: FAIL because mode controls and orchestration are absent.

- [ ] **Step 3: Add canonical helpers and draft state**

```ts
function newCustomMenuID(): string {
  const uuid = globalThis.crypto?.randomUUID?.().replaceAll('-', '')
  return (uuid || `${Date.now().toString(36)}${Math.random().toString(36).slice(2)}`).slice(0, 24)
}
function menuContentType(item: CustomMenuItem): CustomMenuContentType {
  if (item.content_type) return item.content_type
  if (item.url.startsWith('html:')) return 'html'
  if (item.url.startsWith('md:') || item.page_slug) return 'markdown'
  return 'url'
}
const htmlDrafts = reactive<Record<string, string>>({})
const dirtyHtmlSlugs = ref(new Set<string>())
const originalHtmlSlugs = ref(new Set<string>())
```

Generate IDs in `addMenuItem`. HTML selection ensures a slug, explicit type, and `html:<slug>` URL. Record original HTML slugs after load and successful save.

- [ ] **Step 4: Replace URL-only UI with three modes**

URL renders the existing URL input. Markdown renders a slug input and serializes `md:<slug>`. HTML renders edit and saved/unsaved status; mount one editor outside the repeated block.

Use these selectors:

```html
data-testid="custom-menu-content-type"
data-testid="edit-custom-menu-html"
data-testid="custom-menu-html-status"
data-testid="add-custom-menu-item"
```

Opening loads the admin source once unless a draft exists. Treat 404 as a blank new document; report other failures.

- [ ] **Step 5: Implement upload-before-settings and cleanup-after-settings**

```ts
const activeHtmlItems = form.custom_menu_items.filter((item) => menuContentType(item) === 'html')
for (const item of activeHtmlItems) {
  const slug = item.page_slug || ''
  if (!slug) throw new Error(t('admin.settings.customMenu.html.missingSlug'))
  if (!originalHtmlSlugs.value.has(slug) && !dirtyHtmlSlugs.value.has(slug)) {
    throw new Error(t('admin.settings.customMenu.html.sourceRequired'))
  }
  if (dirtyHtmlSlugs.value.has(slug)) {
    const source = htmlDrafts[slug] || ''
    if (!hasVisibleStaticHtml(source)) throw new Error(t('admin.settings.customMenu.html.emptyAfterSanitize'))
    await adminAPI.pages.putHtml(slug, source)
  }
}
const updated = await settingsStepUp.run(() => adminAPI.settings.updateSettings(payload))
const currentSlugs = new Set(updated.custom_menu_items.filter((item) => menuContentType(item) === 'html').map((item) => item.page_slug!))
const removedSlugs = [...originalHtmlSlugs.value].filter((slug) => !currentSlugs.has(slug))
const cleanupResults = await Promise.allSettled(removedSlugs.map((slug) => adminAPI.pages.deleteHtml(slug)))
```

Clear dirty state only after settings succeeds. Preserve it on failure. Cleanup rejection reports a localized warning without rolling back the valid menu.

- [ ] **Step 6: Run SettingsView tests/typecheck and commit**

Run: `cd frontend && pnpm test:run src/views/admin/__tests__/SettingsView.spec.ts && pnpm typecheck`

Expected: PASS with existing URL/Markdown behavior unchanged.

```bash
git add frontend/src/views/admin/SettingsView.vue frontend/src/views/admin/__tests__/SettingsView.spec.ts
git commit -m "feat(settings): manage static HTML menu pages"
```

### Task 8: User Static HTML Page Rendering

**Files:**
- Modify: `frontend/src/views/user/CustomPageView.vue`
- Create: `frontend/src/views/user/__tests__/CustomPageView.spec.ts`
- Modify: `frontend/src/i18n/locales/zh/misc.ts`
- Modify: `frontend/src/i18n/locales/en/misc.ts`

- [ ] **Step 1: Write failing HTML mode/regression tests**

Mock route, stores, and `pagesAPI`. Prove shared frame use, inert errors, and URL/Markdown behavior:

```ts
const routeState = reactive({ params: { id: 'about' } })
const appSettings = reactive<{ custom_menu_items: CustomMenuItem[] }>({ custom_menu_items: [] })
const getHtml = vi.fn()

vi.mock('vue-router', () => ({ useRoute: () => routeState }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale: ref('zh-CN') }) }))
vi.mock('@/api', () => ({ pagesAPI: { getHtml } }))
vi.mock('@/stores', () => ({
  useAppStore: () => ({
    cachedPublicSettings: appSettings,
    publicSettingsLoaded: true,
    fetchPublicSettings: vi.fn(),
  }),
}))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isAdmin: false, token: 'jwt', user: { id: 1 } }),
}))
vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({ customMenuItems: [] }),
}))

function mountView(id: string) {
  routeState.params.id = id
  return mount(CustomPageView, {
    global: {
      stubs: { AppLayout: { template: '<main><slot /></main>' } },
      mocks: { $t: (key: string) => key },
    },
  })
}

it('fetches HTML and renders the shared frame', async () => {
  appSettings.custom_menu_items = [{ id: 'about', label: 'About', icon_svg: '', url: 'html:about', content_type: 'html', page_slug: 'about', visibility: 'user', sort_order: 0 }]
  getHtml.mockResolvedValue('<style>h1{color:red}</style><h1>About</h1>')
  const wrapper = mountView('about'); await flushPromises()
  expect(getHtml).toHaveBeenCalledWith('about')
  expect(wrapper.getComponent(StaticHtmlFrame).props('source')).toContain('<h1>About</h1>')
  expect(wrapper.find('.custom-embed-frame').exists()).toBe(false)
})

it('shows an inert failure state', async () => {
  getHtml.mockRejectedValue(new Error('<script>alert(1)</script>'))
  const wrapper = mountView('about'); await flushPromises()
  expect(wrapper.text()).toContain('customPage.htmlUnavailable')
  expect(wrapper.html()).not.toContain('<script>')
})
```

- [ ] **Step 2: Run tests and verify RED**

Run: `cd frontend && pnpm test:run src/views/user/__tests__/CustomPageView.spec.ts`

Expected: FAIL because HTML falls through to invalid URL.

- [ ] **Step 3: Add explicit mode detection and HTML loading**

```ts
const contentType = computed<CustomMenuContentType>(() => {
  const item = menuItem.value
  if (!item) return 'url'
  if (item.content_type) return item.content_type
  if (item.url?.startsWith('html:')) return 'html'
  if (item.url?.startsWith('md:') || item.page_slug) return 'markdown'
  return 'url'
})
const htmlSlug = computed(() => contentType.value === 'html'
  ? menuItem.value?.page_slug || menuItem.value?.url.slice(5) || ''
  : '')
```

Watch the slug, call `pagesAPI.getHtml`, store raw source separately from Markdown output, and use a monotonically increasing request generation so stale route requests cannot win. Never render exception text.

- [ ] **Step 4: Render the shared frame before URL mode**

```vue
<StaticHtmlFrame v-else-if="isHtmlMode && htmlSource" class="h-full w-full" :source="htmlSource" :title="menuItem.label" />
<div v-else-if="isHtmlMode && htmlLoadFailed" data-testid="html-page-unavailable">
  {{ t('customPage.htmlUnavailable') }}
</div>
```

Keep Markdown TOC and external iframe branches unchanged. Add Chinese/English loading and unavailable text.

- [ ] **Step 5: Run combined tests/typecheck and commit**

Run: `cd frontend && pnpm test:run src/views/user/__tests__/CustomPageView.spec.ts src/utils/__tests__/staticHtml.spec.ts src/components/common/__tests__/StaticHtmlFrame.spec.ts src/views/admin/__tests__/SettingsView.spec.ts && pnpm typecheck`

Expected: PASS.

```bash
git add frontend/src/views/user/CustomPageView.vue frontend/src/views/user/__tests__/CustomPageView.spec.ts frontend/src/i18n/locales/zh/misc.ts frontend/src/i18n/locales/en/misc.ts
git commit -m "feat(pages): render static HTML custom pages"
```

### Task 9: Persistence Documentation And End-to-End Verification

**Files:**
- Modify: `README_CN.md`
- Modify: `deploy/README.md`

- [ ] **Step 1: Document persistence**

Add this beside Docker storage guidance, with equivalent Chinese text in `README_CN.md`:

```text
Managed custom HTML pages are stored in /app/data/pages/*.html. Normal image pulls and
container recreation preserve them because /app/data is a named volume or bind mount.
Include this directory in migrations and backups. `docker compose down -v` deletes the
named volume and therefore deletes managed HTML pages.
```

Do not add a new volume or backup mechanism.

- [ ] **Step 2: Format and run backend verification**

Run:

```bash
gofmt -w backend/internal/handler/page_store.go backend/internal/handler/page_store_test.go backend/internal/handler/page_handler.go backend/internal/handler/page_handler_test.go backend/internal/handler/dto/settings.go backend/internal/handler/admin/setting_handler_update.go backend/internal/handler/admin/setting_handler_custom_menu_test.go backend/internal/server/router.go
cd backend
go test -tags=unit ./internal/handler/admin ./internal/handler ./internal/server -run 'CustomMenu|Page|APIContract' -count=1
go test -tags=unit ./internal/handler/admin ./internal/handler -count=1
```

Expected: PASS.

- [ ] **Step 3: Run frontend verification**

Run:

```bash
cd frontend
pnpm test:run src/api/__tests__/pages.spec.ts src/utils/__tests__/staticHtml.spec.ts src/components/common/__tests__/StaticHtmlFrame.spec.ts src/components/admin/settings/__tests__/StaticHtmlEditorDialog.spec.ts src/views/user/__tests__/CustomPageView.spec.ts src/views/admin/__tests__/SettingsView.spec.ts
pnpm lint:check
pnpm typecheck
pnpm build
```

Expected: all tests PASS, ESLint has zero errors, typecheck exits 0, and the production build succeeds.

- [ ] **Step 4: Perform manual browser verification**

Run the existing development stack and verify desktop/mobile widths:

1. add by pasted source;
2. preview inline CSS, fragment, `/orders`, and external links;
3. upload a replacement `.html` and save;
4. confirm no application CSS leakage or executable script;
5. remove the item and confirm its route is unavailable;
6. run `docker compose -f deploy/docker-compose.yml config` and confirm `/app/data` remains backed by the `sub2api_data` named volume; do not stop an unrelated running deployment.

Capture one editor screenshot and one user-page screenshot. Check browser console output for CSP, navigation, sanitizer, and layout errors.

- [ ] **Step 5: Check the diff and commit documentation**

Run: `git diff --check && git status --short`

Expected: no whitespace errors and only intended files.

```bash
git add README_CN.md deploy/README.md
git commit -m "docs: document custom HTML page persistence"
```

## Completion Checklist

- [ ] Legacy HTTP(S), `md:slug`, and `html:slug` normalize correctly.
- [ ] HTML management is admin-only, audited, UTF-8-only, and limited to 1 MiB.
- [ ] User reads require authentication plus visibility and return `text/plain`.
- [ ] Writes are atomic and referenced files cannot be deleted.
- [ ] Admin preview and user rendering share one sanitizer/frame.
- [ ] Scripts, events, forms, nested frames, redirects, and dangerous URLs are inert.
- [ ] All approved link classes behave as designed.
- [ ] Settings upload before publishing and delete only after removing references.
- [ ] `/app/data/pages` persistence and `down -v` risk are documented.
- [ ] Focused backend/frontend tests, lint, typecheck, and build pass.
