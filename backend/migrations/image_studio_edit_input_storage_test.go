package migrations

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageStudioEditInputStorageMigrationAddsMetadataAndRedactsOnlyTerminalEdits(t *testing.T) {
	content, err := FS.ReadFile("175_image_studio_edit_input_storage.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS input_image_paths JSONB NOT NULL DEFAULT '[]'::jsonb")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS input_mask_path TEXT")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS input_expires_at TIMESTAMPTZ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS input_deleted_at TIMESTAMPTZ")
	require.Contains(t, sql, "request_payload - 'images' - 'mask'")
	require.Regexp(t, regexp.MustCompile(`(?is)WHERE\s+mode\s*=\s*'edit'\s+AND\s+status\s+IN\s*\(\s*'succeeded'\s*,\s*'failed'\s*\)`), sql)
	require.NotRegexp(t, regexp.MustCompile(`(?is)status\s+IN\s*\([^)]*'queued'`), sql)
	require.NotRegexp(t, regexp.MustCompile(`(?is)status\s+IN\s*\([^)]*'running'`), sql)
	require.Contains(t, sql, "CREATE INDEX IF NOT EXISTS idx_image_studio_jobs_input_cleanup")
	require.Contains(t, sql, "(input_expires_at, input_deleted_at, status, id)")
}
