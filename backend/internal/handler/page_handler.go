package handler

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type PageHandler struct {
	store          *pageStore
	settingService *service.SettingService
}

func NewPageHandler(dataDir string, settingService *service.SettingService) *PageHandler {
	return &PageHandler{store: newPageStore(dataDir), settingService: settingService}
}

// GetPageContent serves raw markdown content for a given slug.
// GET /api/v1/pages/:slug
func (h *PageHandler) GetPageContent(c *gin.Context) {
	slug := c.Param("slug")
	if !validPageSlug(slug) {
		response.BadRequest(c, "Invalid page slug")
		return
	}

	// Visibility check: slug must be configured in custom_menu_items
	// and the user must have permission based on visibility setting
	if !h.checkPageVisibility(c, slug, dto.CustomMenuContentMarkdown) {
		c.JSON(http.StatusNotFound, gin.H{"error": "page not found"})
		return
	}

	content, err := h.store.read(slug, pageExtensionMD)
	if errors.Is(err, errPageNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "page not found"})
		return
	}
	if errors.Is(err, errPageTooLarge) {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "page too large"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read page"})
		return
	}

	c.Data(http.StatusOK, "text/markdown; charset=utf-8", content)
}

// GetHTMLPage serves a referenced static HTML page as inert plain text.
func (h *PageHandler) GetHTMLPage(c *gin.Context) {
	slug := c.Param("slug")
	if !h.checkPageVisibility(c, slug, dto.CustomMenuContentHTML) {
		c.JSON(http.StatusNotFound, gin.H{"error": "page not found"})
		return
	}
	content, err := h.store.read(slug, pageExtensionHTML)
	if err != nil {
		h.writePageError(c, err)
		return
	}
	c.Data(http.StatusOK, "text/plain; charset=utf-8", content)
}

// GetAdminHTMLPage returns source even when it is not yet referenced by a menu item.
func (h *PageHandler) GetAdminHTMLPage(c *gin.Context) {
	content, err := h.store.read(c.Param("slug"), pageExtensionHTML)
	if err != nil {
		h.writePageError(c, err)
		return
	}
	c.Data(http.StatusOK, "text/plain; charset=utf-8", content)
}

// PutAdminHTMLPage atomically creates or replaces one static HTML document.
func (h *PageHandler) PutAdminHTMLPage(c *gin.Context) {
	slug := c.Param("slug")
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxPageFileSize+1))
	if err != nil {
		response.BadRequest(c, "Failed to read HTML page")
		return
	}
	if err := h.store.writeHTML(slug, body); err != nil {
		h.writePageError(c, err)
		return
	}
	response.Success(c, gin.H{"slug": slug})
}

// DeleteAdminHTMLPage removes an HTML source only after menu references are gone.
func (h *PageHandler) DeleteAdminHTMLPage(c *gin.Context) {
	slug := c.Param("slug")
	if h.htmlPageReferenced(c, slug) {
		response.Error(c, http.StatusConflict, "HTML page is still referenced by a custom menu item")
		return
	}
	if err := h.store.deleteHTML(slug); err != nil {
		h.writePageError(c, err)
		return
	}
	response.Success(c, gin.H{"slug": slug})
}

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

// ListPages returns available page slugs.
// GET /api/v1/pages
func (h *PageHandler) ListPages(c *gin.Context) {
	entries, err := os.ReadDir(h.store.pagesDir)
	if err != nil {
		response.Success(c, []string{})
		return
	}

	slugs := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".md") {
			slugs = append(slugs, strings.TrimSuffix(name, ".md"))
		}
	}
	response.Success(c, slugs)
}

// ServePageImage serves images from data/pages/{slug}/ directory.
// GET /api/v1/pages/:slug/images/*filename
// No JWT required (browser img tags can't carry tokens), but visibility is checked.
func (h *PageHandler) ServePageImage(c *gin.Context) {
	slug := c.Param("slug")
	filename := c.Param("filename")
	filename = strings.TrimPrefix(filename, "/")

	if !validPageSlug(slug) {
		c.Status(http.StatusNotFound)
		return
	}

	if !h.checkImagePageVisibility(c, slug) {
		c.Status(http.StatusNotFound)
		return
	}

	imagesDir := filepath.Join(h.store.pagesDir, slug)
	cleaned, ok := resolvePageImagePath(h.store.pagesDir, imagesDir, filename)
	if !ok {
		c.Status(http.StatusNotFound)
		return
	}

	info, err := os.Stat(cleaned)
	if err != nil || info.IsDir() {
		c.Status(http.StatusNotFound)
		return
	}

	c.File(cleaned)
}

func resolvePageImagePath(pagesDir, imagesDir, filename string) (string, bool) {
	relPath, ok := cleanPageImageRelativePath(filename)
	if !ok {
		return "", false
	}

	cleanedPagesDir := filepath.Clean(pagesDir)
	cleanedImagesDir := filepath.Clean(imagesDir)
	cleanedTarget := filepath.Clean(filepath.Join(cleanedImagesDir, relPath))
	if !isPathWithinBase(cleanedTarget, cleanedImagesDir) {
		return "", false
	}

	realPagesDir, err := filepath.EvalSymlinks(cleanedPagesDir)
	if err != nil {
		return "", false
	}
	realImagesDir, err := filepath.EvalSymlinks(cleanedImagesDir)
	if err != nil || !isPathWithinBase(realImagesDir, realPagesDir) {
		return "", false
	}
	realTarget, err := filepath.EvalSymlinks(cleanedTarget)
	if err != nil || !isPathWithinBase(realTarget, realImagesDir) {
		return "", false
	}
	return realTarget, true
}

func cleanPageImageRelativePath(filename string) (string, bool) {
	if filename == "" {
		return "", false
	}
	if strings.HasPrefix(filename, "/") {
		return "", false
	}
	decoded, err := url.PathUnescape(filename)
	if err != nil {
		return "", false
	}
	if decoded == "" || strings.HasPrefix(decoded, "/") || strings.Contains(decoded, "\\") || strings.ContainsRune(decoded, 0) {
		return "", false
	}

	parts := make([]string, 0)
	for _, part := range strings.Split(decoded, "/") {
		switch part {
		case "", ".":
			continue
		case "..":
			return "", false
		default:
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return "", false
	}

	relPath := filepath.Join(parts...)
	if filepath.IsAbs(relPath) || filepath.VolumeName(relPath) != "" {
		return "", false
	}
	return relPath, true
}

func isPathWithinBase(path, base string) bool {
	rel, err := filepath.Rel(filepath.Clean(base), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// findPageReference looks up a page reference with an exact content type.
func (h *PageHandler) findPageReference(c *gin.Context, slug, contentType string) (string, bool) {
	if h.settingService == nil {
		return "", false
	}
	for _, item := range dto.ParseCustomMenuItems(h.settingService.GetCustomMenuItemsRaw(c.Request.Context())) {
		if item.EffectiveContentType() == contentType && item.EffectivePageSlug() == slug {
			return item.Visibility, true
		}
	}
	return "", false
}

// checkPageVisibility verifies the referenced page is visible to the current role.
func (h *PageHandler) checkPageVisibility(c *gin.Context, slug, contentType string) bool {
	visibility, found := h.findPageReference(c, slug, contentType)
	if !found {
		return false
	}
	if visibility == "admin" {
		role, _ := middleware2.GetUserRoleFromContext(c)
		return role == service.RoleAdmin
	}
	return true
}

// checkImagePageVisibility checks Markdown visibility for image requests (no JWT available).
// Only allows user-visible pages; admin-only pages are blocked.
func (h *PageHandler) checkImagePageVisibility(c *gin.Context, slug string) bool {
	visibility, found := h.findPageReference(c, slug, dto.CustomMenuContentMarkdown)
	if !found {
		return false
	}
	return visibility != "admin"
}

func (h *PageHandler) htmlPageReferenced(c *gin.Context, slug string) bool {
	if h.settingService == nil {
		return false
	}
	for _, item := range dto.ParseCustomMenuItems(h.settingService.GetCustomMenuItemsRaw(c.Request.Context())) {
		if item.EffectiveContentType() == dto.CustomMenuContentHTML && item.EffectivePageSlug() == slug {
			return true
		}
	}
	return false
}

// RegisterPageRoutes registers page routes on a router group.
func RegisterPageRoutes(v1 *gin.RouterGroup, dataDir string, jwtAuth gin.HandlerFunc, adminAuth gin.HandlerFunc, auditLog gin.HandlerFunc, settingService *service.SettingService) {
	h := NewPageHandler(dataDir, settingService)

	// Authenticated page content (JWT required + visibility check)
	pages := v1.Group("/pages")
	pages.Use(jwtAuth)
	{
		pages.GET("/:slug", h.GetPageContent)
		pages.GET("/:slug/html", h.GetHTMLPage)
	}

	// Images: no JWT (browser img tags can't carry tokens), visibility check in handler
	pageImages := v1.Group("/pages")
	{
		pageImages.GET("/:slug/images/*filename", h.ServePageImage)
	}

	// Admin-only: list all available pages
	adminPages := v1.Group("/pages")
	adminPages.Use(adminAuth)
	adminPages.Use(middleware2.AdminComplianceGuard(settingService))
	{
		adminPages.GET("", h.ListPages)
	}

	managedPages := v1.Group("/admin/pages")
	managedPages.Use(adminAuth, auditLog, middleware2.AdminComplianceGuard(settingService))
	{
		managedPages.GET("/:slug/html", h.GetAdminHTMLPage)
		managedPages.PUT("/:slug/html", h.PutAdminHTMLPage)
		managedPages.DELETE("/:slug/html", h.DeleteAdminHTMLPage)
	}
}
