package tlv_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Azure/adx-mon/pkg/tlv"
	"github.com/stretchr/testify/require"
)

func TestTLVAtHead(t *testing.T) {
	tests := []struct {
		Preserve bool
	}{
		{
			Preserve: true,
		},
		{
			Preserve: false,
		},
	}
	for _, tt := range tests {
		t.Run(strconv.FormatBool(tt.Preserve), func(t *testing.T) {
			// Create a couple TLVs
			t1 := tlv.New(tlv.Tag(0x1), []byte("Tag1Value"))
			t2 := tlv.New(tlv.Tag(0x2), []byte("Tag2Value"))
			ts := []*tlv.TLV{t1, t2}
			encoded := tlv.Encode(ts...)

			// Create a payload
			payload := []byte("foo bar baz")

			// Our source stream
			r := bytes.NewReader(append(encoded, payload...))

			// Create a TLV reader
			tr := tlv.NewReader(r, tlv.WithPreserve(tt.Preserve))

			// Consume our stream
			var w bytes.Buffer
			n, err := io.Copy(&w, tr)
			require.NoError(t, err)

			if tt.Preserve {
				require.Equal(t, len(payload)+len(encoded), int(n))
			} else {
				require.Equal(t, len(payload), int(n))
			}

			// Test header
			for i, h := range tr.Header() {
				require.Equal(t, ts[i].Tag, h.Tag)
				require.Equal(t, ts[i].Length, h.Length)
				require.Equal(t, ts[i].Value, h.Value)
			}

			// Ensure payload is intact
			if tt.Preserve {
				require.Equal(t, append(encoded, payload...), w.Bytes())
			} else {
				require.Equal(t, payload, w.Bytes())
			}
		})
	}
}

func TestTLVInPayload(t *testing.T) {
	tests := []struct {
		Preserve bool
	}{
		{
			Preserve: true,
		},
		{
			Preserve: false,
		},
	}
	for _, tt := range tests {
		t.Run(strconv.FormatBool(tt.Preserve), func(t *testing.T) {
			// We want to test the case where TLV is present in
			// the stream but not at the head of the stream.
			// | Payload | TLV | Payload |

			// Create a TLV
			t1 := tlv.New(tlv.Tag(0x1), []byte("Tag1Value"))
			encoded := tlv.Encode(t1)

			// Create a payload
			payload1 := []byte("foo bar baz")
			payload2 := []byte("baz bar foo")

			// Our source stream
			streamBytes := append(payload1, encoded...)
			streamBytes = append(streamBytes, payload2...)
			r := bytes.NewReader(streamBytes)

			// Create a TLV reader
			tr := tlv.NewReader(r, tlv.WithPreserve(tt.Preserve))

			// Consume our stream
			var w bytes.Buffer
			n, err := io.Copy(&w, tr)
			require.NoError(t, err)

			if tt.Preserve {
				require.Equal(t, len(streamBytes), int(n))
			} else {
				require.Equal(t, len(payload1)+len(payload2), int(n))
			}

			// Test header
			h := tr.Header()
			require.Equal(t, 1, len(h))
			require.Equal(t, t1.Tag, h[0].Tag)
			require.Equal(t, t1.Length, h[0].Length)
			require.Equal(t, t1.Value, h[0].Value)

			// Ensure payload is intact
			if tt.Preserve {
				require.Equal(t, streamBytes, w.Bytes())
			} else {
				require.Equal(t, append(payload1, payload2...), w.Bytes())
			}
		})
	}
}

func TestMultiReadCalls(t *testing.T) {
	tests := []struct {
		Preserve bool
	}{
		{
			Preserve: true,
		},
		{
			Preserve: false,
		},
	}
	for _, tt := range tests {
		t.Run(strconv.FormatBool(tt.Preserve), func(t *testing.T) {
			// We want to test the case where Read is called multiple times.
			// This is going to test our most common case, where we have a
			// stream that contains checksums at the head and TLVs
			// scattered throughout the stream.
			// | Checksum | TLV | Payload | TLV | Payload

			checksum := []byte("checksum")
			payload1 := bytes.Repeat([]byte("payload1"), 100)
			t1 := tlv.New(tlv.Tag(0x1), []byte("Tag1Value"))
			tp1 := tlv.New(tlv.PayloadTag, []byte(strconv.Itoa(len(payload1))))

			payload2 := bytes.Repeat([]byte("payload2"), 127)
			t2 := tlv.New(tlv.Tag(0x1), []byte("Tag2Value"))
			tp2 := tlv.New(tlv.PayloadTag, []byte(strconv.Itoa(len(payload2))))

			// Our source stream
			streamBytes := append(checksum, tlv.Encode(t1, tp1)...)
			streamBytes = append(streamBytes, payload1...)

			streamBytes = append(streamBytes, tlv.Encode(t2, tp2)...)
			streamBytes = append(streamBytes, payload2...)

			r := bytes.NewReader(streamBytes)

			// Create a TLV reader
			tr := tlv.NewReader(r, tlv.WithPreserve(tt.Preserve), tlv.WithBufferSize(128))

			// Consume our stream
			var w bytes.Buffer
			n, err := io.Copy(&w, tr)
			require.NoError(t, err)

			if tt.Preserve {
				require.Equal(t, len(streamBytes), int(n))
			} else {
				require.Equal(t, len(checksum)+len(payload1)+len(payload2), int(n))
			}

			h := tr.Header()
			require.Equal(t, 4, len(h))
			tltvs := []*tlv.TLV{t1, tp1, t2, tp2}
			for i, hh := range h {
				require.Equal(t, tltvs[i].Tag, hh.Tag)
				require.Equal(t, tltvs[i].Length, hh.Length)
				require.Equal(t, tltvs[i].Value, hh.Value)
			}

		})
	}
}

func BenchmarkReader(b *testing.B) {
	dir := b.TempDir()
	fn := filepath.Join(dir, "test")
	b.Logf("writing to %s", fn)

	f, err := os.Create(fn)
	require.NoError(b, err)
	for i := 0; i < 10; i++ {
		t := tlv.New(tlv.Tag(0xAB), []byte("some tag payload"))
		payload := bytes.Repeat([]byte("payload"), 100)
		f.Write(tlv.Encode(t))
		f.Write(payload)
	}
	require.NoError(b, f.Close())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f, err := os.Open(fn)
		require.NoError(b, err)

		r := tlv.NewReader(f, tlv.WithBufferSize(128))

		io.Copy(io.Discard, r)

		require.NoError(b, f.Close())
	}
}

var otlpLog = []byte(`{
	"resourceLogs": [
		{
			"resource": {
				"attributes": [
					{
						"key": "source",
						"value": {
							"stringValue": "hostname"
						}
					}
				],
				"droppedAttributesCount": 1
			},
			"scopeLogs": [
				{
					"scope": {
						"name": "name",
						"version": "version",
						"droppedAttributesCount": 1
					},
					"logRecords": [
						{
							"timeUnixNano": "1669112524001",
							"observedTimeUnixNano": "1669112524001",
							"severityNumber": 17,
							"severityText": "Error",
							"body": {
								"stringValue": "{\"msg\":\"something happened\n\"}"
							},
							"attributes": [
								{
									"key": "kusto.table",
									"value": {
										"stringValue": "ATable"
									}
								},
								{
									"key": "kusto.database",
									"value": {
										"stringValue": "ADatabase"
									}
								}
							],
							"droppedAttributesCount": 1,
							"flags": 1,
							"traceId": "",
							"spanId": ""
						}
					],
					"schemaUrl": "scope_schema"
				}
			],
			"schemaUrl": "resource_schema"
		}
	]
}`)
