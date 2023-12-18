package tlv

import (
	"bytes"
	"encoding/binary"
	"io"

	gbp "github.com/libp2p/go-buffer-pool"
)

type TLV struct {
	Tag    Tag
	Length uint32
	Value  []byte
}

type Tag uint16

var (
	marker = Tag(0xFEDA)
)

const (
	sizeOfHeader = binary.MaxVarintLen16 /* T */ + binary.MaxVarintLen32 /* L */ + binary.MaxVarintLen32 /* V */

	// PayloadTag is used to indicate the length of the payload.
	PayloadTag = Tag(0xCB)
)

func New(tag Tag, value []byte) *TLV {

	return &TLV{
		Tag:    tag,
		Length: uint32(len(value)),
		Value:  value,
	}
}

func (t *TLV) Encode() []byte {
	b := make([]byte, binary.MaxVarintLen16+binary.MaxVarintLen32+t.Length)
	binary.BigEndian.PutUint16(b[0:], uint16(t.Tag))
	binary.BigEndian.PutUint32(b[binary.MaxVarintLen16:], t.Length)
	copy(b[binary.MaxVarintLen16+binary.MaxVarintLen32:], t.Value)
	return b
}

// Encode the TLVs by prefixing a TLV as a header that
// contains the number of TLVs contained within.
func Encode(tlvs ...*TLV) []byte {
	var b bytes.Buffer

	for _, t := range tlvs {
		b.Write(t.Encode())
	}

	// Header is TLV where V is a uint32 instead of a byte slice.
	// T is a magic number 0x1
	// L is the number of TLVs
	// V is the size in bytes of all the TLVs
	v := gbp.Get(sizeOfHeader)
	defer gbp.Put(v)
	binary.BigEndian.PutUint16(v, uint16(marker))
	binary.BigEndian.PutUint32(v[binary.MaxVarintLen16:], uint32(b.Len()))                         // L
	binary.BigEndian.PutUint32(v[binary.MaxVarintLen16+binary.MaxVarintLen32:], uint32(len(tlvs))) // V

	return append(v, b.Bytes()...)
}

type ReaderOption func(*Reader)

func WithPreserve(preserve bool) ReaderOption {
	return func(r *Reader) {
		r.preserve = preserve
	}
}

func WithBufferSize(size int) ReaderOption {
	return func(r *Reader) {
		r.bufferSize = size
	}
}

type Reader struct {
	source io.Reader
	header []TLV
	buf    []byte

	// underRunIndex is the index of the first byte
	// in buf that marks the end of the last TLV bytes.
	//
	// This value is set when our buffer contains a TLV
	// marker but does not have enough bytes to extract
	// the full TLV sequence.
	// Upon our next Read invocation, we'll reference the
	// underRunIndex to continue filling the buffer from
	// that point.
	underRunIndex int

	// preserve indicates that we should not discard TLV
	// read from the source.
	preserve bool

	// bufferSize is the size of the buffer used for the
	// lifetime of the Reader.
	bufferSize int
}

func NewReader(r io.Reader, opts ...ReaderOption) *Reader {
	// Our buffer is necessary because we'll need to reslice
	// the provided slice in order to remove TLVs. Since slices
	// are references to an underlying array, any reslicing
	// we do within the body of Read won't translate to the
	// caller's slice.
	tlvr := &Reader{source: r, bufferSize: 4096}
	for _, opt := range opts {
		opt(tlvr)
	}
	tlvr.buf = gbp.Get(tlvr.bufferSize)
	return tlvr
}

func (r *Reader) Header() []TLV {
	return r.header
}

func (r *Reader) Read(p []byte) (n int, err error) {
	// Upon return, if we're at EOF, we'll return our buffer to the pool.
	defer func() {
		if err == io.EOF {
			gbp.Put(r.buf)
		}
	}()

	// Read from our source into our buffer. If the parameter slice has less
	// capacity than our own buffer, we'll use a limit reader so as to be capable
	// of returning the entire slice.
	if len(p) < r.bufferSize {
		n, err = io.LimitReader(r.source, int64(len(p))).Read(r.buf[r.underRunIndex:])
	} else {
		n, err = r.source.Read(r.buf[r.underRunIndex:])
	}

	if err == io.EOF {
		// If there's anything remaining in our buffer, copy it over
		if r.underRunIndex > 0 {
			n = copy(p, r.buf[0:r.underRunIndex])
		}
		return
	}

	// Initialize our index state
	var (
		head = 0
		tail = r.underRunIndex + n

		// `stop` is returned by `next` and denotes no additional buffer procssing
		stop bool
		// `markerHead` and `markerTail` is returned by `next` and denotes the indices
		// where TLV has been found.
		markerHead, markerTail int
		// `dstIndex` is the index of the next byte to copy into the destination slice.
		dstIndex int
	)

	// Reset our `underRunIndex`
	r.underRunIndex = 0

	// Also reset `n`, which will be updated as we copy bytes into `p`
	n = 0

	// process our buffer
	for {
		// Find our next TLV
		stop, markerHead, markerTail = r.next(head, tail)

		// If `markerTail` != `tail` yet `stop` is true, we have
		// insufficient space in our buffer to fully evaluate
		// the presence of TLV. So we're going to move the remaining
		// bytes to the beginning of our buffer and set `underRunIndex`
		// to the end of our moved bytes such that the next time Read
		// is called, we'll fill the remainder of our buffer starting
		// at `underRunIndex`.
		//
		//                       tail
		//                         │
		//                         ▼
		// ┌───────────────┬────────┐
		// │               │        │
		// └───────────────┴────────┘
		//                 ▲
		//                 │
		//             markerTail
		//
		// ┌──────┬─────────────────┐
		// │      │                 │
		// └──────┴─────────────────┘
		//        ▲
		//        │
		//     underRunIndex
		if stop && markerTail != tail {
			// An edge case is where we have TLV but not enough buffer to finish
			// reading the full TLV context, but we've not advanced the head index.
			// Fortunately, we already return the starting point for where we've
			// found the TLV marker, so we can set the head index to that value.
			if head == 0 {
				head = markerHead
			}
			r.underRunIndex = copy(r.buf[0:], r.buf[head:tail])
			break
		}

		if !r.preserve {
			// If we don't want to preserve TLV
			// ┌───────────────────────────┐
			// └───────────────────────────┘
			// ▲    ▲     ▲
			// │    │     │
			// d   mh     mt
			//
			// We want to copy into `p` from `dstIndex` to `markerHead`
			if markerHead == -1 {
				// If we didn't find TLV in the remaining bytes of our buffer,
				// we'll copy the remaining bytes into `p` and return.
				markerHead = tail
			}
			dstIndex += copy(p[dstIndex:], r.buf[head:markerHead])
			n = dstIndex
		} else {
			// If we want to preserve TLV
			// ┌───────────────────────────┐
			// └───────────────────────────┘
			//  ▲    ▲     ▲
			//  │    │     │
			//  d   mh     mt
			//
			// We want to copy into `p` from `dstIndex` to `markerTail`
			dstIndex += copy(p[dstIndex:], r.buf[head:markerTail])
			n = dstIndex
		}

		// Advance our index state
		//
		//           head
		//             │
		//             ▼
		//  ┌───────────────────────────┐
		//  └───────────────────────────┘
		//  ▲    ▲     ▲
		//  │    │     │
		//  d   mh     mt
		//
		head = markerTail

		if stop {
			break
		}
	}

	return
}

func (r *Reader) next(start, end int) (stop bool, markerHead, markerTail int) {
	// If our buffer has insufficient remaining bytes to read a TLV header,
	// we'll set underRunIndex so we can continue filling the buffer from
	// that point on the next call to Read.
	if start+sizeOfHeader > end {
		stop = true
		return
	}

	// Find our marker
	markerHead = bytes.Index(r.buf[start:end], []byte{0xFE, 0xDA})
	stop = markerHead == -1
	if stop {
		// Advance `markerTail` to the end of our buffer, no more TLV
		markerTail = end
		return
	}

	// Since we're indexing into the buffer from `start`, we need to
	// advance `markerHead` by `start` to get the actual index of the
	// marker in the buffer.
	//
	//                 markerHead [5]
	//                   │
	//                   ▼
	// ┌─────────────────────────────┐
	// └─────────────────────────────┘
	//           ▲
	//           │
	//         start [10]
	markerHead += start

	// Read the header
	markerTail = markerHead + binary.MaxVarintLen16
	sizeOfElements := binary.BigEndian.Uint32(r.buf[markerTail:])
	markerTail += binary.MaxVarintLen32
	elements := int(binary.BigEndian.Uint32(r.buf[markerTail:]))
	markerTail += binary.MaxVarintLen32

	// At this point we have a TLV header. If there is insufficient
	// space in the buffer to extract the full TLV, we'll set underRunIndex
	// so we can continue filling the buffer from that point on the next
	// call to Read.
	stop = markerTail+int(sizeOfElements) > end
	if stop {
		return
	}

	// We have a TLV header and enough bytes in our buffer to extract
	// the full TLV.
	for i := 0; i < elements; i++ {
		t := TLV{}
		t.Tag = Tag(binary.BigEndian.Uint16(r.buf[markerTail:]))
		markerTail += binary.MaxVarintLen16
		t.Length = binary.BigEndian.Uint32(r.buf[markerTail:])
		markerTail += binary.MaxVarintLen32
		t.Value = append(t.Value, r.buf[markerTail:markerTail+int(t.Length)]...)
		markerTail += int(t.Length)
		r.header = append(r.header, t)
	}

	return
}
