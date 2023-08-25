package flake

import (
	"testing"

	"github.com/davidnarayan/go-flake"
	"github.com/stretchr/testify/require"
)

func TestParseFlakeID(t *testing.T) {
	idgen, err := flake.New()
	require.NoError(t, err)

	id := idgen.NextId()
	createdAt, err := ParseFlakeID(id.String())
	require.NoError(t, err)

	id1 := idgen.NextId()
	require.NoError(t, err)
	createdAt1, err := ParseFlakeID(id1.String())

	require.True(t, createdAt1.After(createdAt))

}
