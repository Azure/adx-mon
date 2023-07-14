package wal

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
)

// segmentIterator is an iterator for a segment file.  It allows reading back values written to the segment in the
// same order they were written.
type segmentIterator struct {
	// f is the underlying segment file on disk.
	f *os.File

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
}

func NewSegmentIterator(f *os.File) (Iterator, error) {
	return &segmentIterator{
		f:     f,
		n:     0,
		buf:   make([]byte, 0, 4096),
		value: nil,
	}, nil
}
func (b *segmentIterator) Next() (bool, error) {
	// Each block may have multiple entries corresponding to each call to Write
	if b.n < len(b.buf) {
		return b.nextValue()
	}

	// Read the block length and CRC
	n, err := b.f.Read(b.lenCrcBuf[:8])
	if err == io.EOF {
		return false, err
	} else if err != nil || n != 8 {
		return false, fmt.Errorf("short read: expected 8, got %d", n)
	}

	// Extract the block length and expand the read buffer if it is too small.
	blockLen := binary.BigEndian.Uint32(b.lenCrcBuf[:4])
	if uint32(cap(b.buf)) < blockLen {
		b.buf = make([]byte, 0, blockLen)
	}

	// Extract the CRC value for the block
	crc := binary.BigEndian.Uint32(b.lenCrcBuf[4:8])

	// Read the expected block length bytes
	n, err = b.f.Read(b.buf[:blockLen])
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

	buf, err := decoder.DecodeAll(b.buf[:blockLen], nil)
	if err != nil {
		return false, err
	}

	// Setup internal iterator indexing on this block.
	b.buf = buf
	b.n = 0
	b.value = nil

	// Unwrap the first value in this block.
	return b.nextValue()
}

func (b *segmentIterator) nextValue() (bool, error) {
	if b.n > len(b.buf) {
		return false, io.EOF
	}
	blockLen := binary.BigEndian.Uint32(b.buf[b.n : b.n+4])
	crc := binary.BigEndian.Uint32(b.buf[b.n+4 : b.n+8])

	if int(blockLen) > len(b.buf[b.n+8:]) {
		return false, fmt.Errorf("short block read: expected %d, got %d", blockLen, len(b.buf[b.n+8:]))
	}

	value := b.buf[b.n+8 : b.n+8+int(blockLen)]

	if crc32.ChecksumIEEE(value) != crc {
		return false, fmt.Errorf("block checksum verification failed")
	}

	b.value = value
	b.n += 8 + int(blockLen)

	return true, nil
}

func (b *segmentIterator) Value() []byte {
	return b.value
}

func (b *segmentIterator) Close() error {
	return b.f.Close()
}
