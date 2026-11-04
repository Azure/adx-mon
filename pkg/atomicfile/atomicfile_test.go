package atomicfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	require.NoError(t, WriteFile(path, []byte("first"), 0644))
	require.NoError(t, WriteFile(path, []byte("second"), 0644))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "second", string(data))
	st, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0644), st.Mode().Perm())

	// No temporary files are left behind.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

// A failed write leaves the existing file unchanged.
func TestWriteFileFailureKeepsPrevious(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to read-only directories")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	require.NoError(t, WriteFile(path, []byte("first"), 0644))
	require.NoError(t, os.Chmod(dir, 0555))
	defer os.Chmod(dir, 0755)

	require.Error(t, WriteFile(path, []byte("second"), 0644))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "first", string(data))
}
