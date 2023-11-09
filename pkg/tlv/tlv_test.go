package tlv_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"testing"

	v1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/collector/logs/v1"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/ingestor/transform"
	"github.com/Azure/adx-mon/pkg/otlp"
	"github.com/Azure/adx-mon/pkg/tlv"
	"github.com/Azure/adx-mon/pkg/wal"
	"github.com/Azure/adx-mon/pkg/wal/file"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestEmbedTLV(t *testing.T) {
	tests := []struct {
		TLV bool
	}{
		{
			TLV: true,
		},
		{
			TLV: false,
		},
	}
	for _, tt := range tests {
		t.Run(strconv.FormatBool(tt.TLV), func(t *testing.T) {
			// We're immulating the path a segment would take as received
			// via the /transfer endpoint in Ingestor, where a segment, as
			// represented by a WAL being streamed from a client, is written
			// to disk using the `store` package.
			storageDir := t.TempDir()
			store := storage.NewLocalStore(storage.StoreOpts{
				StorageDir: storageDir,
			})
			err := store.Open(context.Background())
			require.NoError(t, err)

			var (
				numSegments         = 10
				segmentBytesWritten = 0
				numLogs             = 0
				database            = "DBA"
				table               = "TableA"
			)

			segmentDir := t.TempDir()
			sw, err := wal.NewWAL(wal.WALOpts{StorageDir: segmentDir, Prefix: fmt.Sprintf("%s_%s", database, table)})
			require.NoError(t, err)
			err = sw.Open(context.Background())
			require.NoError(t, err)

			for i := 0; i < numSegments; i++ {

				// Transform our log into CSV
				var log v1.ExportLogsServiceRequest
				err := protojson.Unmarshal(otlpLog, &log)
				require.NoError(t, err)

				var b bytes.Buffer
				w := transform.NewCSVWriter(&b, nil)
				logs := &otlp.Logs{
					Resources: log.ResourceLogs[0].Resource.Attributes,
					Logs:      log.ResourceLogs[0].ScopeLogs[0].LogRecords,
				}
				err = w.MarshalCSV(logs)
				require.NoError(t, err)

				segmentBytes := b.Bytes()
				segmentBytesWritten += len(segmentBytes)
				var s []byte
				if tt.TLV {
					// Number of Logs in the segment
					numLogs += len(logs.Logs)
					tc := tlv.New(tlv.Tag(0x11), []byte(strconv.Itoa(len(logs.Logs))))
					// Number of bytes in the segment
					tl := tlv.New(tlv.PayloadTag, []byte(strconv.Itoa(len(segmentBytes))))
					// The segment
					s = append(tlv.Encode(tc, tl), segmentBytes...)
				} else {
					s = segmentBytes
				}

				// Write our CSV to disk as a WAL segment
				err = sw.Write(context.Background(), s)
				require.NoError(t, err)
			}
			segment := sw.Path()
			err = sw.Close()
			require.NoError(t, err)

			// Import our segments. This emulates the path where Collector has written
			// segments that _might_ contain TLV and has sent them to Ingestor via
			// the transfer handler.
			f, err := os.Open(segment)
			require.NoError(t, err)
			_, err = store.Import(segment, f)
			require.NoError(t, err)
			err = f.Close()
			require.NoError(t, err)

			// Close the store to flush the WAL
			err = store.Close()
			require.NoError(t, err)
			// Retrieve the WAL segments from disk. We only
			// expect a single file on disk because the WAL
			// is going to merge all the segments together
			// since they have a common prefix, namely the
			// database and table variable contents.
			files, err := wal.ListDir(storageDir)
			require.NoError(t, err)
			require.Equal(t, 1, len(files))
			// Create a reader and closer for each file,
			// then merge them into a single reader, thereby
			// immulating the behavior of adx::upload.
			var (
				readers []io.Reader
				closers []io.Closer
			)
			for _, fi := range files {
				f, err := wal.NewSegmentReader(fi.Path, &file.DiskProvider{})
				require.NoError(t, err)
				readers = append(readers, f)
				closers = append(closers, f)
			}
			mr := io.MultiReader(readers...)
			// Now wrap our readers with a TLV streaming reader,
			// which is going to attempt discovery of any embedded TLV.
			tlvr := tlv.NewStreaming(mr)
			// We don't actually care about the content here, we just
			// want to fully read the file.
			n, err := io.Copy(io.Discard, tlvr)
			require.NoError(t, err)
			// While we don't care about the content of the file, we do
			// want to ensure that the segment bytes written are equal
			// to the amount we just read from disk. We want to ensure
			// no TLV bytes polluted the segment bytes.
			require.Equal(t, segmentBytesWritten, int(n))
			// Shut down all our readers. While not strictly necessary
			// since the test will just delete all these, it's still a
			// good practice.
			for _, c := range closers {
				err := c.Close()
				require.NoError(t, err)
			}
			// Iteratte through all our discovered TLV. We want to
			// find all the TLV that have our special tag 0x11, which
			// is the number of logs in the segment. We want to ensure
			// that the sum of all the logs in the segment is equal to
			// the number of logs we wrote to disk.
			var (
				numLogsInTLV int
				headers      = tlvr.Header()
			)
			if !tt.TLV {
				require.Equal(t, 0, len(headers))
				return
			}
			for _, h := range headers {
				if h.Tag == tlv.Tag(0x11) {
					v, err := strconv.Atoi(string(h.Value))
					require.NoError(t, err)
					numLogsInTLV += v
				}
			}
			require.Equal(t, numLogs, numLogsInTLV)
		})
	}
}

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
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, randomBytes, data)

	tlvs := r.Header()
	require.Equal(t, 1, len(tlvs))
	require.Equal(t, v, string(tlvs[0].Value))
}

func TestReader(t *testing.T) {
	tests := []struct {
		Name      string
		HeaderLen int
	}{
		{
			Name:      "single header entry",
			HeaderLen: 1,
		},
		{
			Name: "this payload contains no tlv header",
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

			have, err := io.ReadAll(r)
			require.NoError(t, err)
			require.Equal(t, tt.Name, string(have))

			h := r.Header()
			require.Equal(t, tt.HeaderLen, len(h))
			for _, metadata := range h {
				require.Equal(t, tt.Name, string(metadata.Value))
			}
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
