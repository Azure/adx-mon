package tlv

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/Azure/adx-mon/pkg/pool"
)

type TLV struct {
	Tag    Tag
	Length uint32
	Value  []byte
}

type Tag uint16

var (
	buf    = pool.NewBytes(1024)
	magicn = Tag(0x1)
)

const sizeOfHeader = binary.MaxVarintLen16 /* T */ + binary.MaxVarintLen32 /* L */ + binary.MaxVarintLen32 /* V */

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
	v := buf.Get(sizeOfHeader)
	defer buf.Put(v)
	binary.BigEndian.PutUint16(v, uint16(magicn))                                                  // T
	binary.BigEndian.PutUint32(v[binary.MaxVarintLen16:], uint32(b.Len()))                         // L
	binary.BigEndian.PutUint32(v[binary.MaxVarintLen16+binary.MaxVarintLen32:], uint32(len(tlvs))) // V

	return append(v, b.Bytes()...)
}

type Reader struct {
	source     io.Reader
	discovered bool
	header     []TLV
	buf        []byte
}

func NewReader(r io.Reader) *Reader {
	return &Reader{source: r}
}

func (r *Reader) Read(p []byte) (n int, err error) {
	// extract our header
	if !r.discovered {
		if err := r.decode(); err != nil {
			return 0, err
		}
	}
	// drain
	if len(r.buf) != 0 {
		n = copy(p, r.buf)
		r.buf = r.buf[n:]
		return
	}
	// fast path
	n, err = r.source.Read(p)
	return
}

func (r *Reader) Header() ([]TLV, error) {
	if r.discovered {
		return r.header, nil
	}

	if err := r.decode(); err != nil {
		return nil, err
	}

	return r.header, nil
}

func (r *Reader) decode() error {
	p := buf.Get(sizeOfHeader)
	defer buf.Put(p)

	n, err := r.source.Read(p)
	if err != nil {
		return err
	}

	// source has no header
	if Tag(binary.BigEndian.Uint16(p)) != magicn {
		r.discovered = true
		// we need to keep these bytes around until someone calls Read
		r.buf = make([]byte, len(p))
		copy(r.buf, p)
		return nil
	}
	offset := binary.MaxVarintLen16

	sizeOfElements := binary.BigEndian.Uint32(p[offset:])
	offset += binary.MaxVarintLen32
	elements := int(binary.BigEndian.Uint32(p[offset:]))
	offset += binary.MaxVarintLen32

	// at this point we know how much data we need from our source, so fill the buffer
	if n < int(sizeOfElements) {
		// read the remaining bytes needed to extract our header
		l := &io.LimitedReader{R: r.source, N: int64(int(sizeOfElements))}
		var read []byte
		read, err = io.ReadAll(l)
		if err != nil {

			// we thought we had a header, but we just got unlucky
			// with the first byte being our magic number.
			if err == io.EOF {
				r.discovered = true
				r.buf = make([]byte, len(read)+n)
				copy(r.buf, p)
				copy(r.buf[n:], read)
				return nil
			}
			return err
		}
		// resize
		p = append(p, read...)
	}

	// no bounds checks are necessary, all sizes are known
	r.header = make([]TLV, elements)
	for i := 0; i < elements; i++ {
		t := TLV{}
		t.Tag = Tag(binary.BigEndian.Uint16(p[offset:]))
		offset += binary.MaxVarintLen16
		t.Length = binary.BigEndian.Uint32(p[offset:])
		offset += binary.MaxVarintLen32
		t.Value = p[offset : offset+int(t.Length)]
		offset += int(t.Length)
		r.header[i] = t
	}

	r.discovered = true
	return nil
}
