package admin

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

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
	require.Equal(t, "md:guide", items[1].URL)
	require.Equal(t, dto.CustomMenuContentHTML, items[2].ContentType)
	require.Equal(t, "html-about", items[2].PageSlug)
	require.Equal(t, "html:html-about", items[2].URL)
}

func TestUpdateSettingsRejectsInvalidCustomMenuPageReference(t *testing.T) {
	for _, item := range []map[string]any{
		{"id": "x", "label": "X", "content_type": "video", "url": "", "visibility": "user"},
		{"id": "x", "label": "X", "content_type": "html", "page_slug": "../x", "visibility": "user"},
		{"id": "x", "label": "X", "content_type": "html", "page_slug": "", "visibility": "user"},
	} {
		h, _ := newStepUpSwitchTestHandler(t, map[string]string{})
		rec := doUpdateSettings(t, h, map[string]any{"custom_menu_items": []map[string]any{item}}, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code)
	}
}
