package dto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCustomMenuItemsNormalizesLegacyReferences(t *testing.T) {
	items := ParseCustomMenuItems(`[{"id":"docs","url":"https://example.com"},{"id":"guide","url":"md:guide"},{"id":"about","url":"html:about"}]`)

	require.Len(t, items, 3)
	require.Equal(t, CustomMenuContentURL, items[0].ContentType)
	require.Empty(t, items[0].PageSlug)
	require.Equal(t, CustomMenuContentMarkdown, items[1].ContentType)
	require.Equal(t, "guide", items[1].PageSlug)
	require.Equal(t, CustomMenuContentHTML, items[2].ContentType)
	require.Equal(t, "about", items[2].PageSlug)
}
