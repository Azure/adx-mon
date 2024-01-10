package wal

import (
	"context"
	"crypto/sha256"
	"fmt"
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

func TestRemoveSample(t *testing.T) {
	sm := NewSampleMetadata(t.TempDir())
	require.NoError(t, sm.Open(context.Background()))
	require.NoError(t, sm.AddSample(Metric, 10, []byte("testfile")))
	require.NoError(t, sm.RemoveSample([]byte("testfile")))

	// Check if the first bit is set
	require.Equal(t, uint8(0x0), sm.data[0]&0x80>>7)

	require.NoError(t, sm.Close())
}

func TestMetadataFromSample(t *testing.T) {
	sm := NewSampleMetadata(t.TempDir())
	require.NoError(t, sm.Open(context.Background()))
	require.NoError(t, sm.AddSample(Log, 238, []byte("testfile")))

	isActive, sampleType, samples, filenameHash := metadataFromSample(sm.data[:sampleMetadataSize])
	require.Equal(t, true, isActive)
	require.Equal(t, Log, sampleType)
	require.Equal(t, uint16(238), samples)
	sum := sha256.Sum256([]byte("testfile"))
	require.Equal(t, sum[:], filenameHash)
}

func TestAddManySamples(t *testing.T) {
	sm := NewSampleMetadata(t.TempDir())
	require.NoError(t, sm.Open(context.Background()))

	for i := 0; i < 1048; i++ {
		require.NoError(t, sm.AddSample(Log, uint16(i), []byte(fmt.Sprintf("testfile%d", i))))
	}

	// Ensure we've grown the metadata file
	require.Greater(t, len(sm.data), initialSampleMetadataSize)
}

func TestAddSamplesSameEntry(t *testing.T) {
	sm := NewSampleMetadata(t.TempDir())
	require.NoError(t, sm.Open(context.Background()))

	require.NoError(t, sm.AddSample(Log, 8, []byte("testfile")))
	require.NoError(t, sm.AddSample(Log, 2, []byte("testfile")))

	_, _, samples, _ := metadataFromSample(sm.data[:sampleMetadataSize])
	require.Equal(t, uint16(10), samples)
}

func TestSerializeDeserialize(t *testing.T) {
	// Create a new SampleMetadata instance, write some
	// metadata to it, and then close it.
	dir := t.TempDir()
	sm := NewSampleMetadata(dir)
	require.NoError(t, sm.Open(context.Background()))

	require.NoError(t, sm.AddSample(Log, 8, []byte("testfile")))
	require.NoError(t, sm.Close())

	// Now let's open a new SampleMetadata instance and
	// ensure that the metadata is the same.
	sm2 := NewSampleMetadata(dir)
	require.NoError(t, sm2.Open(context.Background()))

	// Verify that the metadata is the same
	isActive, sampleType, samples, filenameHash := metadataFromSample(sm2.data[:sampleMetadataSize])
	require.Equal(t, true, isActive)
	require.Equal(t, Log, sampleType)
	require.Equal(t, uint16(8), samples)
	sum := sha256.Sum256([]byte("testfile"))
	require.Equal(t, sum[:], filenameHash)

	require.NoError(t, sm2.Close())
}
