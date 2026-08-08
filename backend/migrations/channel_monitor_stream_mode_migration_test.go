package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorStreamModeMigration(t *testing.T) {
	content, err := FS.ReadFile("197_channel_monitor_stream_mode.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS stream BOOLEAN NOT NULL DEFAULT TRUE")
}
