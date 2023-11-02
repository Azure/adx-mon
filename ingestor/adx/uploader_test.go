package adx

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCountSamples(t *testing.T) {
	tests := []struct {
		Input    []byte
		Expected int
	}{
		{
			Input:    []byte("test"),
			Expected: 0,
		},
		{
			Input:    []byte("test\ntest"),
			Expected: 1,
		},
		{
			Input:    []byte("test\\ntest\ntest"),
			Expected: 1,
		},
		{
			Input:    []byte("test\ntest\ntest"),
			Expected: 2,
		},
		{
			Input:    []byte("test\\ntest\ntest\ntest\\ntest\n"),
			Expected: 3,
		},
		{
			Input:    bytes.NewBufferString("test\\ntest\ntest\ntest\\ntest\n").Bytes(),
			Expected: 3,
		},
		{
			Input:    []byte("test\r\ntest"),
			Expected: 0,
		},
		{
			Input:    []byte("\ntest\r\ntest\\ntest"),
			Expected: 1,
		},
		{
			Input:    append([]byte("\ntest\ntest"), bytes.Repeat([]byte("abc\r\nfoo\\nbar"), 513)...),
			Expected: 2,
		},
	}
	for _, tt := range tests {
		t.Run(string(tt.Input), func(t *testing.T) {
			var buf bytes.Buffer
			buf.Write(tt.Input)

			scr := &samplesCounter{r: &buf}
			n, err := io.ReadAll(scr)
			require.NoError(t, err)
			require.Equal(t, len(tt.Input), len(n))
			require.Equal(t, tt.Expected, scr.count)
		})
	}
}

func BenchmarkCountSamples(b *testing.B) {
	data := []byte("test\\ntest\ntest\ntest\\ntest\n")
	r := bytes.NewReader(data)
	scr := &samplesCounter{r: r}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		io.ReadAll(scr)
		r.Seek(0, io.SeekStart)
	}
}
