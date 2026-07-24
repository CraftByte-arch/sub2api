package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type pageSettingRepoStub struct {
	service.SettingRepository
	values map[string]string
}

func (r *pageSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	value, ok := r.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}

func newPageHandlerForTest(t *testing.T, values map[string]string) (*PageHandler, *pageSettingRepoStub) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &pageSettingRepoStub{values: values}
	settingService := service.NewSettingService(repo, &config.Config{})
	return NewPageHandler(t.TempDir(), settingService), repo
}

func performPageRequest(
	t *testing.T,
	handler func(*gin.Context),
	method, target, slug string,
	body []byte,
	role string,
) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Params = gin.Params{{Key: "slug", Value: slug}}
	if role != "" {
		c.Set(string(middleware.ContextKeyUserRole), role)
	}
	c.Request = httptest.NewRequest(method, target, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "text/html; charset=utf-8")
	handler(c)
	return rec
}

func TestPageHandlerHTMLLifecycleAndVisibility(t *testing.T) {
	h, repo := newPageHandlerForTest(t, map[string]string{
		service.SettingKeyCustomMenuItems: `[{"id":"about","label":"About","url":"html:about","content_type":"html","page_slug":"about","visibility":"user"}]`,
	})

	put := performPageRequest(t, h.PutAdminHTMLPage, http.MethodPut, "/api/v1/admin/pages/about/html", "about", []byte("<h1>About</h1>"), service.RoleAdmin)
	require.Equal(t, http.StatusOK, put.Code)

	get := performPageRequest(t, h.GetHTMLPage, http.MethodGet, "/api/v1/pages/about/html", "about", nil, service.RoleUser)
	require.Equal(t, http.StatusOK, get.Code)
	require.Equal(t, "text/plain; charset=utf-8", get.Header().Get("Content-Type"))
	require.Equal(t, "<h1>About</h1>", get.Body.String())

	adminGet := performPageRequest(t, h.GetAdminHTMLPage, http.MethodGet, "/api/v1/admin/pages/about/html", "about", nil, service.RoleAdmin)
	require.Equal(t, http.StatusOK, adminGet.Code)
	require.Equal(t, "text/plain; charset=utf-8", adminGet.Header().Get("Content-Type"))

	blocked := performPageRequest(t, h.DeleteAdminHTMLPage, http.MethodDelete, "/api/v1/admin/pages/about/html", "about", nil, service.RoleAdmin)
	require.Equal(t, http.StatusConflict, blocked.Code)

	repo.values[service.SettingKeyCustomMenuItems] = `[]`
	require.NoError(t, os.WriteFile(filepath.Join(h.store.pagesDir, "about.md"), []byte("# About"), 0o644))
	deleted := performPageRequest(t, h.DeleteAdminHTMLPage, http.MethodDelete, "/api/v1/admin/pages/about/html", "about", nil, service.RoleAdmin)
	require.Equal(t, http.StatusOK, deleted.Code)
	_, err := os.Stat(filepath.Join(h.store.pagesDir, "about.md"))
	require.NoError(t, err)
}

func TestPageHandlerHTMLVisibilityIsContentTypeAware(t *testing.T) {
	tests := []struct {
		name       string
		menuJSON   string
		role       string
		wantStatus int
	}{
		{
			name:       "user-visible HTML",
			menuJSON:   `[{"url":"html:about","visibility":"user"}]`,
			role:       service.RoleUser,
			wantStatus: http.StatusOK,
		},
		{
			name:       "admin HTML blocked from user",
			menuJSON:   `[{"url":"html:about","visibility":"admin"}]`,
			role:       service.RoleUser,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "admin HTML visible to admin",
			menuJSON:   `[{"url":"html:about","visibility":"admin"}]`,
			role:       service.RoleAdmin,
			wantStatus: http.StatusOK,
		},
		{
			name:       "same-slug Markdown does not grant HTML access",
			menuJSON:   `[{"url":"md:about","page_slug":"about","visibility":"user"}]`,
			role:       service.RoleUser,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "unreferenced HTML is hidden",
			menuJSON:   `[]`,
			role:       service.RoleUser,
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := newPageHandlerForTest(t, map[string]string{service.SettingKeyCustomMenuItems: tt.menuJSON})
			require.NoError(t, h.store.writeHTML("about", []byte("<h1>About</h1>")))

			rec := performPageRequest(t, h.GetHTMLPage, http.MethodGet, "/api/v1/pages/about/html", "about", nil, tt.role)
			require.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

func TestPageHandlerAdminCanReadOrphanHTML(t *testing.T) {
	h, _ := newPageHandlerForTest(t, map[string]string{})
	require.NoError(t, h.store.writeHTML("draft", []byte("<p>Draft</p>")))

	rec := performPageRequest(t, h.GetAdminHTMLPage, http.MethodGet, "/api/v1/admin/pages/draft/html", "draft", nil, service.RoleAdmin)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "<p>Draft</p>", rec.Body.String())
}

func TestPageHandlerPutHTMLValidatesSource(t *testing.T) {
	h, _ := newPageHandlerForTest(t, map[string]string{})

	tests := []struct {
		name       string
		slug       string
		body       []byte
		wantStatus int
	}{
		{name: "invalid slug", slug: "../about", body: []byte("x"), wantStatus: http.StatusBadRequest},
		{name: "empty", slug: "about", body: []byte(" \n"), wantStatus: http.StatusBadRequest},
		{name: "invalid UTF-8", slug: "about", body: []byte{0xff}, wantStatus: http.StatusBadRequest},
		{name: "too large", slug: "about", body: []byte(strings.Repeat("x", maxPageFileSize+1)), wantStatus: http.StatusRequestEntityTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := performPageRequest(t, h.PutAdminHTMLPage, http.MethodPut, "/api/v1/admin/pages/"+tt.slug+"/html", tt.slug, tt.body, service.RoleAdmin)
			require.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

func TestPageHandlerMarkdownReferenceDoesNotBlockHTMLDelete(t *testing.T) {
	h, _ := newPageHandlerForTest(t, map[string]string{
		service.SettingKeyCustomMenuItems: `[{"url":"md:about","page_slug":"about","visibility":"user"}]`,
	})
	require.NoError(t, h.store.writeHTML("about", []byte("<p>Old HTML</p>")))

	rec := performPageRequest(t, h.DeleteAdminHTMLPage, http.MethodDelete, "/api/v1/admin/pages/about/html", "about", nil, service.RoleAdmin)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestRegisterPageRoutesAuditsAdminHTMLWrites(t *testing.T) {
	router := gin.New()
	v1 := router.Group("/api/v1")
	pass := func(c *gin.Context) { c.Next() }
	audit := func(c *gin.Context) {
		c.Header("X-Test-Audit", "applied")
		c.Next()
	}
	RegisterPageRoutes(v1, t.TempDir(), pass, pass, audit, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/pages/about/html", strings.NewReader("<h1>About</h1>"))
	req.Header.Set("Content-Type", "text/html; charset=utf-8")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "applied", rec.Header().Get("X-Test-Audit"))
}

func TestCleanPageImageRelativePath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{name: "single filename", in: "logo.png", want: "logo.png", ok: true},
		{name: "nested path", in: "images/logo.png", want: filepath.Join("images", "logo.png"), ok: true},
		{name: "dot prefix", in: "./logo.png", want: "logo.png", ok: true},
		{name: "url escaped slash", in: "images%2Flogo.png", want: filepath.Join("images", "logo.png"), ok: true},
		{name: "parent traversal", in: "../secret.png", ok: false},
		{name: "encoded parent traversal", in: "%2e%2e/secret.png", ok: false},
		{name: "backslash traversal", in: `images\secret.png`, ok: false},
		{name: "absolute path", in: "/etc/passwd", ok: false},
		{name: "encoded absolute path", in: "%2fetc/passwd", ok: false},
		{name: "encoded nul byte", in: "logo.png%00", ok: false},
		{name: "invalid escape", in: "logo.png%zz", ok: false},
		{name: "empty path", in: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := cleanPageImageRelativePath(tt.in)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("path = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvePageImagePath(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "pages")
	base := filepath.Join(pagesDir, "guide")
	if err := os.MkdirAll(filepath.Join(base, "images"), 0755); err != nil {
		t.Fatalf("create images dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "logo.png"), []byte("fake"), 0644); err != nil {
		t.Fatalf("create direct image: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "images", "logo.png"), []byte("fake"), 0644); err != nil {
		t.Fatalf("create image: %v", err)
	}

	got, ok := resolvePageImagePath(pagesDir, base, "logo.png")
	if !ok {
		t.Fatal("expected direct image path to be accepted")
	}
	want := mustEvalSymlinks(t, filepath.Join(base, "logo.png"))
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}

	got, ok = resolvePageImagePath(pagesDir, base, "images/logo.png")
	if !ok {
		t.Fatal("expected nested image path to be accepted")
	}
	want = mustEvalSymlinks(t, filepath.Join(base, "images", "logo.png"))
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}

	if got, ok := resolvePageImagePath(pagesDir, base, "../guide.md"); ok {
		t.Fatalf("expected traversal to be rejected, got %q", got)
	}
}

func TestResolvePageImagePathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	pagesDir := filepath.Join(root, "pages")
	base := filepath.Join(pagesDir, "guide")
	outside := filepath.Join(root, "outside")

	if err := os.MkdirAll(base, 0755); err != nil {
		t.Fatalf("create page dir: %v", err)
	}
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatalf("create outside dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.png"), []byte("secret"), 0644); err != nil {
		t.Fatalf("create outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "images")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	if got, ok := resolvePageImagePath(pagesDir, base, "images/secret.png"); ok {
		t.Fatalf("expected symlink escape to be rejected, got %q", got)
	}
}

func mustEvalSymlinks(t *testing.T, path string) string {
	t.Helper()

	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("eval symlinks for %q: %v", path, err)
	}
	return realPath
}
