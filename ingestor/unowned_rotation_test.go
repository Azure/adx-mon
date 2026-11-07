package ingestor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type stubPartitioner map[string]string

func (p stubPartitioner) Owner(b []byte) (string, string) { return p[string(b)], "" }

func TestOwnedBy(t *testing.T) {
	owned := ownedBy(stubPartitioner{"db_Mine": "ingestor-0", "db_Theirs": "ingestor-1"}, "ingestor-0")
	require.True(t, owned("db_Mine"))
	require.False(t, owned("db_Theirs"))
	require.True(t, owned("db_Unknown"), "a table whose owner is unknown is treated as owned")
}
