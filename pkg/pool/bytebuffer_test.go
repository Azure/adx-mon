package pool

import (
	"testing"

	gbp "github.com/libp2p/go-buffer-pool"
	"github.com/stretchr/testify/require"
)

// pooledAfter puts a buffer that grew to size bytes and returns the capacity the buffer handed out next still holds.
func pooledAfter(t *testing.T, size int) int {
	t.Helper()
	p := &bytesBufferPool{NewGeneric(1, BytesBufferPool.fn)}
	buf := p.Get(512 * 1024).(*gbp.Buffer)
	buf.Reset()
	_, err := buf.Write(make([]byte, size))
	require.NoError(t, err)
	p.Put(buf)
	return p.Get(512 * 1024).(*gbp.Buffer).Cap()
}

// A buffer put back into the pool hands back the memory it grew to while holding a request body, so the buffers the
// pool keeps hold none after a flood of large bodies.
func TestBytesBufferPoolReleasesGrownBuffers(t *testing.T) {
	require.Zero(t, pooledAfter(t, 16<<20))
}
