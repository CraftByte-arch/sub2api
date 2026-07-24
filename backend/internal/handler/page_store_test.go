package handler

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPageStoreHTMLLifecycle(t *testing.T) {
	dataDir := t.TempDir()
	store := newPageStore(dataDir)
	content := bytes.Repeat([]byte("x"), maxPageFileSize)

	require.NoError(t, store.writeHTML("about", content))
	got, err := store.read("about", pageExtensionHTML)
	require.NoError(t, err)
	require.Equal(t, content, got)

	markdownPath := filepath.Join(store.pagesDir, "about.md")
	assetDir := filepath.Join(store.pagesDir, "about")
	require.NoError(t, os.WriteFile(markdownPath, []byte("# About"), 0o644))
	require.NoError(t, os.Mkdir(assetDir, 0o755))
	require.NoError(t, store.deleteHTML("about"))

	_, err = store.read("about", pageExtensionHTML)
	require.ErrorIs(t, err, errPageNotFound)
	_, err = os.Stat(markdownPath)
	require.NoError(t, err)
	_, err = os.Stat(assetDir)
	require.NoError(t, err)
}

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
	temporaryFiles, err := filepath.Glob(filepath.Join(store.pagesDir, ".about-*.tmp"))
	require.NoError(t, err)
	require.Empty(t, temporaryFiles)
}

func TestPageStoreRejectsInvalidHTML(t *testing.T) {
	store := newPageStore(t.TempDir())

	tests := []struct {
		name    string
		slug    string
		content []byte
		wantErr error
	}{
		{name: "empty", slug: "about", content: nil, wantErr: errPageEmpty},
		{name: "whitespace", slug: "about", content: []byte(" \n\t"), wantErr: errPageEmpty},
		{name: "invalid UTF-8", slug: "about", content: []byte{0xff}, wantErr: errPageInvalidUTF8},
		{name: "parent traversal", slug: "../about", content: []byte("x"), wantErr: errInvalidPageSlug},
		{name: "path separator", slug: "folder/about", content: []byte("x"), wantErr: errInvalidPageSlug},
		{name: "encoded traversal", slug: "%2e%2e", content: []byte("x"), wantErr: errInvalidPageSlug},
		{name: "too long", slug: strings.Repeat("a", 65), content: []byte("x"), wantErr: errInvalidPageSlug},
		{name: "too large", slug: "about", content: bytes.Repeat([]byte("x"), maxPageFileSize+1), wantErr: errPageTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, store.writeHTML(tt.slug, tt.content), tt.wantErr)
		})
	}
}

func TestPageStoreReadRejectsSymlinkEscape(t *testing.T) {
	dataDir := t.TempDir()
	store := newPageStore(dataDir)
	require.NoError(t, os.MkdirAll(store.pagesDir, 0o755))

	outsidePath := filepath.Join(dataDir, "secret.html")
	require.NoError(t, os.WriteFile(outsidePath, []byte("secret"), 0o644))
	if err := os.Symlink(outsidePath, filepath.Join(store.pagesDir, "about.html")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	_, err := store.read("about", pageExtensionHTML)
	require.ErrorIs(t, err, errPageNotFound)
}

func TestPageStoreRejectsInvalidReadAndDeleteSlugs(t *testing.T) {
	store := newPageStore(t.TempDir())

	_, err := store.read("../about", pageExtensionHTML)
	require.ErrorIs(t, err, errInvalidPageSlug)
	require.ErrorIs(t, store.deleteHTML("../about"), errInvalidPageSlug)
	_, err = store.read("about", ".txt")
	require.ErrorIs(t, err, errInvalidPageSlug)
}
