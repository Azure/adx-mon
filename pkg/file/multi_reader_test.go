package file

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewMultiReader_NotExists(t *testing.T) {
	mr, err := NewMultiReader("testdata/1", "testdata/2")
	require.True(t, os.IsNotExist(err))
	require.Nil(t, mr)
}
