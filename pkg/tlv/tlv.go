package tlv

import (
	"encoding/binary"
	"io"
	"math/big"

	"github.com/Azure/adx-mon/pkg/pool"
)

type TLV struct {
	Tag    Tag
	Length uint32
	Value  []byte
}

type Tag uint16

var buf = pool.NewBytes(1024)

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
	var b []byte

	// First create our header
	v := append(big.NewInt(int64(len(tlvs))).Bytes(), byte(0))
	header := &TLV{
		Tag:    0x1,
		Length: uint32(len(v)),
		Value:  v,
	}
	b = append(b, header.Encode()...)

	// Now append all our elements
	for _, t := range tlvs {
		b = append(b, t.Encode()...)
	}

	return b
}

func Decode(data []byte) []*TLV {
	// First determine how many TLVs are in the data
	header := &TLV{
		Tag:    Tag(binary.BigEndian.Uint16(data[0:])),
		Length: binary.BigEndian.Uint32(data[binary.MaxVarintLen16:]),
	}
	header.Value = data[binary.MaxVarintLen16+binary.MaxVarintLen32 : binary.MaxVarintLen16+binary.MaxVarintLen32+header.Length]
	elements := int(big.NewInt(0).SetBytes(header.Value[:header.Length-1]).Int64())

	// Now decode all the TLVs
	var (
		tlvs   []*TLV
		offset = binary.MaxVarintLen16 + binary.MaxVarintLen32 + int(header.Length)
	)
	for i := 0; i < elements; i++ {
		t := &TLV{}
		t.Tag = Tag(binary.BigEndian.Uint16(data[offset:]))
		offset += binary.MaxVarintLen16
		t.Length = binary.BigEndian.Uint32(data[offset:])
		offset += binary.MaxVarintLen32
		if offset+int(t.Length) > len(data) {
			break
		}
		t.Value = data[offset : offset+int(t.Length)]
		offset += int(t.Length)
		tlvs = append(tlvs, t)
	}

	return tlvs
}

func DecodeSeeker(s io.ReadSeeker) ([]*TLV, error) {
	data := buf.Get(1024)
	defer buf.Put(data)
	data = data[0:]

	_, err := s.Read(data)
	if err != nil {
		return nil, err
	}

	header := &TLV{
		Tag:    Tag(binary.BigEndian.Uint16(data[0:])),
		Length: binary.BigEndian.Uint32(data[binary.MaxVarintLen16:]),
	}
	header.Value = data[binary.MaxVarintLen16+binary.MaxVarintLen32 : binary.MaxVarintLen16+binary.MaxVarintLen32+header.Length]
	elements := int(big.NewInt(0).SetBytes(header.Value[:header.Length-1]).Int64())

	// Now decode all the TLVs
	var (
		tlvs   []*TLV
		offset = binary.MaxVarintLen16 + binary.MaxVarintLen32 + int(header.Length)
	)
	for i := 0; i < elements; i++ {
		t := &TLV{}
		t.Tag = Tag(binary.BigEndian.Uint16(data[offset:]))
		offset += binary.MaxVarintLen16
		t.Length = binary.BigEndian.Uint32(data[offset:])
		offset += binary.MaxVarintLen32
		if offset+int(t.Length) > len(data) {
			break
		}
		t.Value = data[offset : offset+int(t.Length)]
		offset += int(t.Length)
		tlvs = append(tlvs, t)
	}

	// Seek past our TLVs and at the beginning of our payload
	if _, err := s.Seek(int64(offset), io.SeekStart); err != nil {
		return nil, err
	}

	return tlvs, nil
}
