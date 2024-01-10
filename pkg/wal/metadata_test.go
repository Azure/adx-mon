package wal

import (
	"context"
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAddSample(t *testing.T) {
	// Initialize a new SampleMetadata instance
	sm := NewSampleMetadata(t.TempDir())
	require.NoError(t, sm.Open(context.Background()))

	// Call the AddSample method
	require.NoError(t, sm.AddSample(Metric, 10, []byte("testfile")))

	// Check if the first bit is set
	require.Equal(t, uint8(0x1), sm.data[0]&0x80>>7)

	// Check if the sample type is set correctly
	require.Equal(t, uint8(0x0), sm.data[0]&0x1F)

	// Check if the number of samples is set correctly
	require.Equal(t, uint8(0xa), sm.data[1])

	// Check if the filename is set correctly
	sum := sha256.Sum256([]byte("testfile"))
	require.Equal(t, sum[:], sm.data[2:34])

	require.NoError(t, sm.Close())
}
