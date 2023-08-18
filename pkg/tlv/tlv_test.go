package tlv_test

import (
	"bytes"
	"io"
	"os"
	"reflect"
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

	r := tlv.NewReader(tf)
	tlvs, err := r.Header()
	require.NoError(t, err)
	require.Equal(t, 1, len(tlvs))
	require.Equal(t, v, string(tlvs[0].Value))

	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, randomBytes, data)
}

func TestReader(t *testing.T) {
	tests := []struct {
		Name       string
		HeaderLen  int
		SkipHeader bool
	}{
		{
			Name:      "single header entry",
			HeaderLen: 1,
		},
		{
			Name: "this payload contains no tlv header",
		},
		{
			Name:       "Invoke Read without first invoking Header even though TLVs exist",
			HeaderLen:  2,
			SkipHeader: true,
		},
		{
			Name:      "Several header entries",
			HeaderLen: 5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			b := []byte(tt.Name)

			if tt.HeaderLen != 0 {
				var tlvs []*tlv.TLV
				for i := 0; i < tt.HeaderLen; i++ {
					tlvs = append(tlvs, tlv.New(tlv.Tag(i), []byte(tt.Name)))
				}
				b = append(tlv.Encode(tlvs...), b...)
			}

			source := bytes.NewBuffer(b)
			r := tlv.NewReader(source)

			if !tt.SkipHeader {
				h, err := r.Header()
				require.NoError(t, err)
				require.Equal(t, tt.HeaderLen, len(h))
				for _, metadata := range h {
					require.Equal(t, tt.Name, string(metadata.Value))
				}
			}

			have, err := io.ReadAll(r)
			require.NoError(t, err)
			require.Equal(t, tt.Name, string(have))
		})
	}
}

func TestUnluckyMagicNumber(t *testing.T) {
	var b bytes.Buffer
	_, err := b.Write([]byte{0x1})
	require.NoError(t, err)
	_, err = b.WriteString("I kind of look like a TLV")
	require.NoError(t, err)
	r := tlv.NewReader(bytes.NewBuffer(b.Bytes()))
	have, err := io.ReadAll(r)
	require.NoError(t, err)
	require.True(t, reflect.DeepEqual(have, b.Bytes()))
}

func BenchmarkReader(b *testing.B) {
	t := tlv.New(tlv.Tag(0x2), []byte("some tag payload"))
	h := tlv.Encode(t)
	p := bytes.NewReader(append(h, []byte("body payload")...))
	r := tlv.NewReader(p)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		io.ReadAll(r)
	}
}
