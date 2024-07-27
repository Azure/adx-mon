package wal

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"

	"github.com/klauspost/compress/s2"
)

// segmentIterator is an iterator for a segment file.  It allows reading back values written to the segment in the
// same order they were written.
type segmentIterator struct {
	// br is the underlying segment file on disk.
	br *bufio.Reader

	f io.ReadCloser

	// n is the index into buf that allows iterating through values in a block.
	n int

	// buf is last read block from disk.  This block holds multiple values corresponding
	// to segment writes.
	buf []byte

	// value is the current value that is returned for the iterator.
	value []byte

	// lenCrcBuf is a temp buffer to re-use for extracting the 8 byte (4 len, 4 crc) values
	// when iterating.
	lenCrcBuf [8]byte

	sampleType  SampleType
	sampleCount uint32

	// decodeBuf is a temp buffer to re-use for decoding the block.
	decodeBuf []byte
}

func NewSegmentIterator(r io.ReadCloser) (Iterator, error) {
	var magicBuf [8]byte
	if _, err := io.ReadFull(r, magicBuf[:]); err != nil {
		return nil, err
	} else if !bytes.Equal(magicBuf[:6], segmentMagic[:6]) {
		return nil, ErrInvalidWALSegment
	}

	return &segmentIterator{
		f:         r,
		br:        bufio.NewReader(r),
		n:         0,
		buf:       make([]byte, 0, 4096),
		decodeBuf: make([]byte, 0, 4096),
		value:     nil,
	}, nil
}
func (b *segmentIterator) Next() (bool, error) {
	// Read the block length and CRC
	n, err := io.ReadFull(b.br, b.lenCrcBuf[:8])
	if err == io.EOF {
		return false, err
	} else if err != nil || n != 8 {
		return false, nil
	}

	// Extract the block length and expand the read buffer if it is too small.
	blockLen := binary.BigEndian.Uint32(b.lenCrcBuf[:4])
	if uint32(cap(b.buf)) < blockLen {
		b.buf = make([]byte, 0, blockLen)
	}

	// If the block length is 0, then we may have some trailing 0 bytes that we can ignore.
	// This segment could be repaired, but a Repair would just truncate this data, so we ignore
	// it anyway.
	if blockLen == 0 {
		return false, nil
	}

	// Extract the CRC value for the block
	crc := binary.BigEndian.Uint32(b.lenCrcBuf[4:8])

	// Read the expected block length bytes
	n, err = io.ReadFull(b.br, b.buf[:blockLen])
	if err != nil {
		return false, err
	}

	// Make sure we actually read the number of bytes we were expecting.
	if uint32(n) != blockLen {
		return false, fmt.Errorf("short block read: expected %d, got %d", blockLen, n)
	}

	// Validate the block checksum matches still
	if crc32.ChecksumIEEE(b.buf[:blockLen]) != crc {
		return false, fmt.Errorf("block checksum verification failed")
	}

	b.decodeBuf, err = s2.Decode(b.decodeBuf[:0], b.buf[:blockLen])
	if err != nil {
		return false, err
	}

	if HasSampleMetadata(b.decodeBuf) {
		st, sc := SampleMetadata(b.decodeBuf[3:8])
		b.sampleType = st
		b.sampleCount += sc
		b.value = b.decodeBuf[8:]
	} else {
		b.value = b.decodeBuf
	}

	return len(b.value) > 0, nil
}

func (b *segmentIterator) Value() []byte {
	return b.value
}

func (b *segmentIterator) Close() error {
	return b.f.Close()
}

// Verify iterates through the entire segment and verifies the checksums for each block.  It stops iterating
// when it reaches the end of the file or encounters a block that could be repaired/dropped.  If this does
// not return an error, then the segment can be safely read continuously to walk valid blocks.  The iterator must be
// re-created after calling this method.
func (b *segmentIterator) Verify() (int, error) {
	var blocks int
	for {
		// Read the block length and CRC
		n, err := io.ReadFull(b.br, b.lenCrcBuf[:8])
		if err == io.EOF {
			return blocks, nil
		} else if err != nil || n != 8 {
			// We don't have a full block, if this segment was repaired, we would not see this.  Instead of returning
			// an error, just stop iteration and assume we've reached the end of the segment.
			return blocks, nil
		}

		// Extract the block length and expand the read buffer if it is too small.
		blockLen := binary.BigEndian.Uint32(b.lenCrcBuf[:4])
		if uint32(cap(b.buf)) < blockLen {
			b.buf = make([]byte, 0, blockLen)
		}

		// Special case where trailing zeros may exist at the end of the file.  We dont' have a valid block, so just
		// stop iteration and assume we've reached the end of the segment.
		if blockLen == 0 {
			return blocks, nil
		}

		// Extract the CRC value for the block
		crc := binary.BigEndian.Uint32(b.lenCrcBuf[4:8])

		// Read the expected block length bytes
		n, err = io.ReadFull(b.br, b.buf[:blockLen])
		if err != nil {
			return 0, err
		}

		// Make sure we actually read the number of bytes we were expecting.
		if uint32(n) != blockLen {
			return 0, fmt.Errorf("short block read: expected %d, got %d", blockLen, n)
		}

		// Validate the block checksum matches still
		if crc32.ChecksumIEEE(b.buf[:blockLen]) != crc {
			return 0, fmt.Errorf("block checksum verification failed")
		}
		blocks++
	}
}

func (b *segmentIterator) Metadata() (t SampleType, sampleCount uint32) {
	return b.sampleType, b.sampleCount
}
