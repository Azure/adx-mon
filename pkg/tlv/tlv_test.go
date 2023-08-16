package tlv_test

import (
	"io"
	"os"
	"testing"

	"github.com/Azure/adx-mon/pkg/tlv"
	"github.com/stretchr/testify/require"
)

func TestTLV(t *testing.T) {
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

	tlvs, err := tlv.Decode(tf)
	require.NoError(t, err)
	require.Equal(t, 1, len(tlvs))
	require.Equal(t, v, string(tlvs[0].Value))

	data, err := io.ReadAll(tf)
	require.NoError(t, err)
	require.Equal(t, randomBytes, data)
}
