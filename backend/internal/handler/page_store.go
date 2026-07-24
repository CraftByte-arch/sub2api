package handler

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxPageFileSize   = 1 << 20
	pageExtensionHTML = ".html"
	pageExtensionMD   = ".md"
)

var (
	errInvalidPageSlug = errors.New("invalid page slug")
	errPageNotFound    = errors.New("page not found")
	errPageTooLarge    = errors.New("page too large")
	errPageInvalidUTF8 = errors.New("page must be valid UTF-8")
	errPageEmpty       = errors.New("page cannot be empty")

	validPageSlugPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)
)

type pageStore struct {
	pagesDir   string
	renameFile func(string, string) error
}

func newPageStore(dataDir string) *pageStore {
	return &pageStore{
		pagesDir:   filepath.Join(dataDir, "pages"),
		renameFile: os.Rename,
	}
}

func validPageSlug(slug string) bool {
	return len(slug) <= 64 && validPageSlugPattern.MatchString(slug)
}

func validPageExtension(extension string) bool {
	return extension == pageExtensionHTML || extension == pageExtensionMD
}

func (s *pageStore) writeHTML(slug string, content []byte) error {
	if !validPageSlug(slug) {
		return errInvalidPageSlug
	}
	if len(content) == 0 {
		return errPageEmpty
	}
	if len(content) > maxPageFileSize {
		return errPageTooLarge
	}
	if !utf8.Valid(content) {
		return errPageInvalidUTF8
	}
	if strings.TrimSpace(string(content)) == "" {
		return errPageEmpty
	}
	if err := os.MkdirAll(s.pagesDir, 0o755); err != nil {
		return fmt.Errorf("create pages directory: %w", err)
	}

	target := filepath.Join(s.pagesDir, slug+pageExtensionHTML)
	tmp, err := os.CreateTemp(s.pagesDir, "."+slug+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary page: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set temporary page permissions: %w", err)
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary page: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary page: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary page: %w", err)
	}
	if err := s.renameFile(tmpName, target); err != nil {
		return fmt.Errorf("replace page: %w", err)
	}
	return nil
}

func (s *pageStore) read(slug, extension string) ([]byte, error) {
	if !validPageSlug(slug) || !validPageExtension(extension) {
		return nil, errInvalidPageSlug
	}

	target := filepath.Join(s.pagesDir, slug+extension)
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errPageNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("inspect page: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errPageNotFound
	}
	if info.Size() > maxPageFileSize {
		return nil, errPageTooLarge
	}

	realBase, err := filepath.EvalSymlinks(s.pagesDir)
	if err != nil {
		return nil, fmt.Errorf("resolve pages directory: %w", err)
	}
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil || !isPathWithinBase(realTarget, realBase) {
		return nil, errPageNotFound
	}

	content, err := os.ReadFile(realTarget)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errPageNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read page: %w", err)
	}
	if len(content) > maxPageFileSize {
		return nil, errPageTooLarge
	}
	return content, nil
}

func (s *pageStore) deleteHTML(slug string) error {
	if !validPageSlug(slug) {
		return errInvalidPageSlug
	}
	target := filepath.Join(s.pagesDir, slug+pageExtensionHTML)
	if err := os.Remove(target); errors.Is(err, os.ErrNotExist) {
		return errPageNotFound
	} else if err != nil {
		return fmt.Errorf("delete page: %w", err)
	}
	return nil
}
