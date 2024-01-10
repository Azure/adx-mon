package wal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sync"
)

//          3 bit        32 Bytes
//  ┌──────┬────┬───────┬────────┐
//  │Active│Type│Samples│Filename│
//  └──────┴────┴───────┴────────┘
//   1 bit       2 bytes

type SampleMetadata struct {
	data     []byte
	filepath string
	lock     sync.Mutex
}

type SampleType uint

const (
	Metric SampleType = iota
	Trace
	Log
)

const (
	sampleMetadataSize        = 1 + 2 + 32
	initialSampleMetadataSize = sampleMetadataSize * 1024
	filename                  = "sample-metadata"
)

func NewSampleMetadata(dir string) *SampleMetadata {
	return &SampleMetadata{
		filepath: filepath.Join(dir, filename),
	}
}

func (sm *SampleMetadata) Open(ctx context.Context) error {
	sm.lock.Lock()
	defer sm.lock.Unlock()

	// Errors are not considered terminal, we will just allocate
	// a new metadata file and continue.
	f, err := os.Open(sm.filepath)
	if err != nil {
		sm.data = make([]byte, initialSampleMetadataSize)
		return nil
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		sm.data = make([]byte, initialSampleMetadataSize)
		return nil
	}

	// TODO vaidate checksum

	_, err = hex.Decode(sm.data, data)
	if err != nil {
		sm.data = make([]byte, initialSampleMetadataSize)
		return nil
	}

	return nil
}

func (sm *SampleMetadata) Close() error {
	sm.lock.Lock()
	defer sm.lock.Unlock()

	// Errors are not terminal
	f, err := os.Create(sm.filepath)
	if err != nil {
		return nil
	}
	defer f.Close()

	dumper := hex.Dumper(f)
	defer dumper.Close()

	dumper.Write(sm.data)
	return nil
}

func (sm *SampleMetadata) AddSample(t SampleType, samples uint16, filename []byte) error {
	sm.lock.Lock()
	defer sm.lock.Unlock()

	for i := 0; i < len(sm.data); i += sampleMetadataSize {
		// Find the first available slot
		if sm.data[i]&0x80 != 0 {
			continue
		}

		// Set the active bit
		sm.data[i] |= 0x80
		// Set the sample type
		sm.data[i] |= byte(t) << 5
		// Set the number of samples
		sm.data[i+1] = byte(samples)
		// Set the filename
		sum := sha256.Sum256(filename)
		copy(sm.data[i+2:], sum[:])

		// TODO emit metric

		break
	}

	return nil
}

func (sm *SampleMetadata) RemoveSample(filename []byte) error {
	sm.lock.Lock()
	defer sm.lock.Unlock()

	sum := sha256.Sum256(filename)
	for i := 0; i < len(sm.data); i += sampleMetadataSize {
		// Find a slot with the same filename
		if sm.data[i+3] != sum[0] {
			continue
		}
		// Set the active bit to 0
		sm.data[i] &= 0x7F

		// TODO emit metric
	}

	return nil
}
