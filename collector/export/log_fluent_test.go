package export

import (
	"net"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
)

func BenchmarkMsgp(b *testing.B) {
	logbatch := types.LogBatch{
		Logs: []*types.Log{
			{
				Timestamp: 1234567890,
				Body: map[string]interface{}{
					"key1": "value1",
					"key2": "value2",
				},
			},
			{
				Timestamp: 1234567900,
				Body: map[string]interface{}{
					"key2": "value1",
					"key3": "value2",
					"object": map[string]interface{}{
						"key1": "value1",
						"key2": "value2",
					},
				},
			},
		},
	}

	e := newFluentEncoder()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = e.Encode(&logbatch)
	}
}

func TestEncode(t *testing.T) {
	logbatch := types.LogBatch{
		Logs: []*types.Log{
			{
				//Timestamp: 1234567890 * 1_000_000_000,
				Timestamp: uint64(time.Now().UnixNano()),
				Body: map[string]interface{}{
					"key1": "value1",
					"key2": "value2",
				},
			},
			{
				Timestamp: 1234567900 * 1_000_000_000,
				Body: map[string]interface{}{
					"key2": "value1",
					"key3": "value2",
					"object": map[string]interface{}{
						"key1": "value1",
						"key2": "value2",
					},
				},
			},
		},
	}

	e := newFluentEncoder()
	b, err := e.Encode(&logbatch)
	require.NoError(t, err)
	require.Greater(t, len(b), 0)

	// sz, rest, err := msgp.ReadArrayHeaderBytes(b)
	// require.NoError(t, err)
	// require.Equal(t, uint32(2), sz)
	// rest = rest

	conn, err := net.Dial("tcp", "localhost:24224")
	require.NoError(t, err)
	defer conn.Close()
	_, err = conn.Write(b)
	require.NoError(t, err)

}

func FuzzFluentExtTime(f *testing.F) {
	f.Add(uint64(1234567890343334523))
	f.Add(uint64(1740111706438750665))
	f.Fuzz(func(t *testing.T, i uint64) {
		e := &fluentExtTime{
			UnixTsNano: i,
		}
		b := make([]byte, 8)
		err := e.MarshalBinaryTo(b)
		require.NoError(t, err)
		require.Equal(t, 8, len(b))

		e.UnmarshalBinary(b)
		require.Equal(t, i, e.UnixTsNano)
	})
}
