package service

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFirstTokenStatsRecorderProviderSetContract(t *testing.T) {
	content, err := os.ReadFile("wire.go")
	require.NoError(t, err)
	source := string(content)
	require.Contains(t, source, "NewFirstTokenTimeoutStatsRecorder,")
	require.Contains(t, source, "wire.Bind(new(FirstTokenStatsRecorder), new(*FirstTokenTimeoutStatsRecorder))")
	require.Equal(t, 1, strings.Count(source, "NewFirstTokenTimeoutStatsRecorder,"))
}
