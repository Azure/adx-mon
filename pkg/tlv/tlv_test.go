package tlv_test

import (
	"io"
	"os"
	"testing"

	"github.com/Azure/adx-mon/pkg/tlv"
	"github.com/stretchr/testify/require"
)

func TestTLV(t *testing.T) {
	// Create our TLVs
	t1k := tlv.Tag(0x01)
	t1v := "Tag1Value"

	t2k := tlv.Tag(0x11)
	t2v := "Tag2Value"

	tlv1 := tlv.New(t1k, []byte(t1v))
	tlv2 := tlv.New(t2k, []byte(t2v))

	// Encode our 2 TLVs, which will apply our control header
	b := tlv.Encode(tlv1, tlv2)

	// Append some random bytes to the end to make sure we can decode
	// only the bytes specified in our TLV header
	b = append(b, []byte("foo bar baz")...)

	// Decode and test
	tlvs := tlv.Decode(b)
	require.Equal(t, 2, len(tlvs))

	require.Equal(t, t1k, tlvs[0].Tag)
	require.Equal(t, t1v, string(tlvs[0].Value))

	require.Equal(t, t2k, tlvs[1].Tag)
	require.Equal(t, t2v, string(tlvs[1].Value))
}

func TestSeeker(t *testing.T) {
	// Setup our file
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "")
	require.NoError(t, err)

	// Create our TLV and write it to disk
	k := tlv.Tag(0x01)
	v := "Tag1Value"
	tt := tlv.New(k, []byte(v))
	randomBytes := []byte("foo bar baz")

	_, err = f.Write(append(tlv.Encode(tt), randomBytes...))
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// Decode our file
	tf, err := os.Open(f.Name())
	require.NoError(t, err)
	defer tf.Close()

	tlvs, err := tlv.DecodeSeeker(tf)
	require.NoError(t, err)
	require.Equal(t, 1, len(tlvs))
	require.Equal(t, v, string(tlvs[0].Value))

	data, err := io.ReadAll(tf)
	require.NoError(t, err)
	require.Equal(t, randomBytes, data)
}
