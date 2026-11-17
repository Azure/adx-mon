package export

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
	"github.com/tinylib/msgp/msgp"
)

type fluentWriteConn struct {
	net.Conn
	err     error
	partial bool
	writes  int
	payload []byte
	done    chan struct{}
	once    sync.Once
}

func (c *fluentWriteConn) Read([]byte) (int, error) { <-c.done; return 0, io.EOF }
func (c *fluentWriteConn) Write(b []byte) (int, error) {
	c.writes++
	if c.err != nil {
		if c.partial {
			return 1, c.err
		}
		return 0, c.err
	}
	c.payload = append(c.payload, b...)
	return len(b), nil
}
func (c *fluentWriteConn) SetWriteDeadline(time.Time) error { return nil }
func (c *fluentWriteConn) Close() error                     { c.once.Do(func() { close(c.done) }); return nil }

type fluentRetryDialer struct {
	conn  net.Conn
	calls int
}

func (d *fluentRetryDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	d.calls++
	return d.conn, nil
}

func TestFluentRetriesStaleConnection(t *testing.T) {
	for _, kind := range []string{"zero write", "partial write", "retry fails", "canceled"} {
		t.Run(kind, func(t *testing.T) {
			stale := &fluentWriteConn{err: errors.New("stale socket"), done: make(chan struct{}), partial: kind == "partial write"}
			fresh := &fluentWriteConn{done: make(chan struct{})}
			defer fresh.Close()
			if kind == "retry fails" {
				fresh.err = errors.New("replacement failed")
			}
			dialer := &fluentRetryDialer{conn: fresh}
			e, err := NewLogToFluentExporter(LogToFluentExporterOpts{Destination: "unix:///fluent.sock", TagAttribute: "destTag"})
			require.NoError(t, err)
			e.conn, e.connection = stale, &fluentConnection{conn: stale}
			e.dialer = dialer
			defer e.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "canceled" {
				cancel()
			}
			err = e.Send(ctx, newTestFluentBatch())
			if kind == "zero write" {
				require.NoError(t, err, "retry the same batch, not just the next one")
				expected, err := newFluentEncoder("destTag").encode(newTestFluentBatch())
				require.NoError(t, err)
				require.Equal(t, expected, fresh.payload)
			} else {
				require.Error(t, err)
			}
			if kind == "partial write" || kind == "canceled" {
				require.Zero(t, dialer.calls, "do not replay possibly delivered bytes or canceled work")
			} else {
				require.Equal(t, 1, dialer.calls, "retry at most once")
				require.Equal(t, 1, fresh.writes)
			}
			require.Equal(t, 1, stale.writes)
		})
	}
}

func TestFluentUnixRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fluent.sock")
	e, err := NewLogToFluentExporter(LogToFluentExporterOpts{Destination: "unix://" + path, TagAttribute: "destTag"})
	require.NoError(t, err)
	defer e.Close()
	batch := newTestFluentBatch()
	expected, err := newFluentEncoder("destTag").encode(batch)
	require.NoError(t, err)
	for range 2 {
		// Closing and recreating the listener models the peer process restarting
		// at the same socket path. The first Send after each start must arrive.
		ln, err := net.Listen("unix", path)
		require.NoError(t, err)
		t.Cleanup(func() { _ = ln.Close() })
		received := make(chan []byte, 1)
		result := make(chan error, 1)
		go func() {
			conn, err := ln.Accept()
			if err != nil {
				result <- err
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			payload := make([]byte, len(expected))
			_, err = io.ReadFull(conn, payload)
			_ = conn.Close()
			received <- payload
			result <- err
		}()
		require.NoError(t, e.Send(t.Context(), batch))
		require.NoError(t, <-result)
		require.Equal(t, expected, <-received)
		require.NoError(t, ln.Close())
	}
}

// startFluentTestServer starts a TCP server on an ephemeral port that counts
// the number of distinct accepted connections. Accepted connections are drained
// in the background. If closeAfterFirstWrite is true, the connection is closed
// after the first read (simulating a peer that drops the connection); the
// server keeps accepting new connections so reconnection works.
func startFluentTestServer(t *testing.T, closeAfterFirstWrite bool) (addr string, connections *int32) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	connCount := int32(0)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			atomic.AddInt32(&connCount, 1)
			go func() {
				buf := make([]byte, 4096)
				for {
					_, err := conn.Read(buf)
					if err != nil {
						return
					}
					if closeAfterFirstWrite {
						// Peer closed the connection after the first write.
						_ = conn.Close()
						return
					}
				}
			}()
		}
	}()

	return ln.Addr().String(), &connCount
}

func newTestFluentBatch() *types.LogBatch {
	batch := &types.LogBatch{}
	batch.AddLiterals([]*types.LogLiteral{
		{
			Timestamp: timeOne,
			Attributes: map[string]any{
				"destTag": "tag1",
			},
			Body: map[string]any{
				"key1": "value1",
			},
		},
	})
	freezeLogsInBatch(batch)
	return batch
}

// TestLogToFluentExporterPersistentConnection verifies that the exporter
// maintains a single persistent connection across multiple Send calls instead
// of dialing a fresh connection for every batch.
func TestLogToFluentExporterPersistentConnection(t *testing.T) {
	addr, connections := startFluentTestServer(t, false)

	exporter, err := NewLogToFluentExporter(LogToFluentExporterOpts{
		Destination:  fmt.Sprintf("tcp://%s", addr),
		TagAttribute: "destTag",
	})
	require.NoError(t, err)

	const numBatches = 5
	for i := 0; i < numBatches; i++ {
		require.NoError(t, exporter.Send(context.Background(), newTestFluentBatch()))
	}

	require.Eventually(t, func() bool { return atomic.LoadInt32(connections) == 1 }, time.Second, time.Millisecond,
		"expected a single persistent connection reused across %d sends", numBatches)

	require.NoError(t, exporter.Close())
}

// TestLogToFluentExporterReconnect verifies that after the peer closes the
// connection, the next Send dials a fresh connection and succeeds.
func TestLogToFluentExporterReconnect(t *testing.T) {
	addr, connections := startFluentTestServer(t, true)

	exporter, err := NewLogToFluentExporter(LogToFluentExporterOpts{
		Destination:  fmt.Sprintf("tcp://%s", addr),
		TagAttribute: "destTag",
	})
	require.NoError(t, err)

	// The server closes its side after receiving the first batch. Wait for
	// the background reader to observe EOF, then Send must reconnect.
	require.NoError(t, exporter.Send(context.Background(), newTestFluentBatch()))
	require.Eventually(t, func() bool {
		exporter.connMu.Lock()
		defer exporter.connMu.Unlock()
		return exporter.connection != nil && exporter.connection.closed.Load()
	}, time.Second, time.Millisecond, "expected the peer EOF to be observed")
	require.NoError(t, exporter.Send(context.Background(), newTestFluentBatch()))

	require.Eventually(t, func() bool { return atomic.LoadInt32(connections) == 2 }, time.Second, time.Millisecond,
		"expected the exporter to reconnect after peer EOF")

	require.NoError(t, exporter.Close())
}

func TestNewLogToFluentExporter(t *testing.T) {
	tests := []struct {
		name        string
		destination string
		tagAttr     string
		expectErr   bool
	}{
		{
			name:        "valid tcp",
			destination: "tcp://localhost:24224",
			tagAttr:     "log_tag",
			expectErr:   false,
		},
		{
			name:        "valid unix",
			destination: "unix:///tmp/fluent.sock",
			tagAttr:     "log_tag",
			expectErr:   false,
		},
		{
			name:        "invalid tag attribute",
			destination: "unix:///tmp/fluent.sock",
			tagAttr:     "",
			expectErr:   true,
		},
		{
			name:        "invalid destination",
			destination: "invalid://localhost:24224",
			tagAttr:     "log_tag",
			expectErr:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opts := LogToFluentExporterOpts{
				Destination:  test.destination,
				TagAttribute: test.tagAttr,
			}
			exporter, err := NewLogToFluentExporter(opts)
			if test.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, exporter)
		})
	}
}

func BenchmarkEncoder(b *testing.B) {
	logbatch := types.LogBatch{}
	logbatch.AddLiterals(testlogs)

	e := newFluentEncoder("destTag")

	for b.Loop() {
		_, _ = e.encode(&logbatch)
	}
}

func TestEncode(t *testing.T) {
	logbatch := &types.LogBatch{}
	logbatch.AddLiterals(testlogs)
	freezeLogsInBatch(logbatch)

	e := newFluentEncoder("destTag")
	b, err := e.encode(logbatch)
	require.NoError(t, err)
	require.Greater(t, len(b), 0)
	requireFluentBatchCleared(t, e)
	batchCapacity := cap(e.batchToSend)
	require.Positive(t, batchCapacity)

	rest := validateArrayHeaderForwardMode(t, b)
	rest = validateTag(t, rest, "tag1")
	rest, entries := getEntries(t, rest)
	validateEntries(t, entries, []entrytuple{
		{timestamp: timeOne, record: map[string]any{"key1": "tag1value1", "key2": "tag1value2"}},
		{timestamp: timeTwo, record: map[string]any{"key3": "tag1value3", "key4": "tag1value4"}},
	})

	rest = validateArrayHeaderForwardMode(t, rest)
	rest = validateTag(t, rest, "tag2")
	rest, entries = getEntries(t, rest)
	validateEntries(t, entries, []entrytuple{
		{timestamp: timeOne, record: map[string]any{"key1": "value1", "key2": "value2", "object": map[string]any{"key3": "value3", "key4": "value4"}}},
	})

	rest = validateArrayHeaderForwardMode(t, rest)
	rest = validateTag(t, rest, "tag3")
	rest, entries = getEntries(t, rest)
	validateEntries(t, entries, []entrytuple{
		{timestamp: timeOne, record: map[string]any{"key1": "tag3value1", "key2": "tag3value2", "object": map[string]any{"key1": "value1",
			"key2": "value2"}}},
		{timestamp: timeTwo, record: map[string]any{"key3": "tag3value3", "key4": "tag3value4", "object": map[string]any{"key1": "value1",
			"key2": "value2"}}},
	})

	// nothing more to read
	require.Equal(t, 0, len(rest))

	newLogs := []*types.LogLiteral{
		{
			Timestamp: timeTwo,
			Attributes: map[string]any{
				"destTag": "tag5",
			},
			Body: map[string]any{
				"key1": "tag5value1",
			},
		},
	}
	logbatch.Reset()
	logbatch.AddLiterals(newLogs)
	freezeLogsInBatch(logbatch)

	b, err = e.encode(logbatch)
	require.NoError(t, err)
	require.Greater(t, len(b), 0)
	requireFluentBatchCleared(t, e)
	require.Equal(t, batchCapacity, cap(e.batchToSend), "scratch capacity should be retained for reuse")
	rest = validateArrayHeaderForwardMode(t, b)
	rest = validateTag(t, rest, "tag5")
	rest, entries = getEntries(t, rest)
	validateEntries(t, entries, []entrytuple{
		{timestamp: timeTwo, record: map[string]any{"key1": "tag5value1"}},
	})

	// nothing more to read
	require.Equal(t, 0, len(rest))
}

func TestEncodeEdgeCases(t *testing.T) {
	invalidLog := &types.LogLiteral{
		Attributes: map[string]any{"destTag": "tag1"},
		Body:       map[string]any{"unsupported": make(chan int)},
	}
	tests := []struct {
		name     string
		logs     []*types.LogLiteral
		wantErr  bool
		wantData bool
	}{
		{name: "empty batch"},
		{name: "filtered batch", logs: testlogs[1:3]},
		{name: "error in final tag", logs: []*types.LogLiteral{invalidLog}, wantErr: true},
		{name: "error before final tag", logs: []*types.LogLiteral{invalidLog, testlogs[4]}, wantErr: true},
		{name: "success after error", logs: testlogs[:1], wantData: true},
	}

	e := newFluentEncoder("destTag")
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			batch := &types.LogBatch{}
			batch.AddLiterals(test.logs)
			freezeLogsInBatch(batch)

			data, err := e.encode(batch)
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if test.wantData {
				require.NotEmpty(t, data)
			} else {
				require.Empty(t, data)
			}

			requireFluentBatchCleared(t, e)
		})
	}
}

func FuzzFluentExtTime(f *testing.F) {
	f.Add(uint64(1234567890343334523), make([]byte, 8), uint64(8))
	f.Add(uint64(1740111706438750665), make([]byte, 8), uint64(8))
	f.Add(uint64(1740111706438750665)%1e9, make([]byte, 8), uint64(8))   // only nanoseconds
	f.Add(uint64(1740111706438750665)/1e9, make([]byte, 8), uint64(8))   // only seconds
	f.Add(uint64(1740111706438750665)/1e9, make([]byte, 7), uint64(7))   // only seconds, not enough space
	f.Add(uint64(1740111706438750665)/1e9, make([]byte, 8), uint64(7))   // only seconds, not enough space to read
	f.Add(uint64(1740111706438750665)/1e9, make([]byte, 200), uint64(8)) // only seconds, large input
	f.Add(uint64(1740111706438750665)/1e9, []byte(nil), uint64(8))       // only seconds, nil input
	f.Fuzz(func(t *testing.T, i uint64, marshalTarget []byte, unmarshalLength uint64) {
		e := &fluentExtTime{
			UnixTsNano: i,
		}
		err := e.MarshalBinaryTo(marshalTarget)
		if len(marshalTarget) < 8 {
			require.Error(t, err)
			return
		}
		require.NoError(t, err)

		if unmarshalLength > uint64(len(marshalTarget)) {
			unmarshalLength = uint64(len(marshalTarget))
		}
		err = e.UnmarshalBinary(marshalTarget[:unmarshalLength])
		if unmarshalLength < 8 {
			require.Error(t, err)
			return
		}
		require.Equal(t, i, e.UnixTsNano)
	})
}

var timeOne uint64
var timeTwo uint64
var timeThree uint64 = uint64(1740529377*1e9 + 123456789)

var testlogs []*types.LogLiteral

func init() {
	val, err := time.Parse(time.RFC3339Nano, "2022-06-01T12:34:56.789012345Z")
	if err != nil {
		panic(err)
	}
	timeOne = uint64(val.UnixNano())

	val, err = time.Parse(time.RFC3339Nano, "2022-06-01T13:45:26.987012345Z")
	if err != nil {
		panic(err)
	}
	timeTwo = uint64(val.UnixNano())

	testlogs = []*types.LogLiteral{
		{
			Timestamp: timeOne,
			Attributes: map[string]any{
				"destTag": "tag1",
			},
			Body: map[string]any{
				"key1": "tag1value1",
				"key2": "tag1value2",
			},
		},
		{ // no dest tag
			Timestamp:  timeThree,
			Attributes: map[string]any{},
			Body: map[string]any{
				"not-routed":   "value1",
				"not-routed-2": "value2",
			},
		},
		{ // non-string dest tag
			Timestamp: timeThree,
			Attributes: map[string]any{
				"destTag": 1,
			},
			Body: map[string]any{
				"not-routed":   "value1",
				"not-routed-2": "value2",
			},
		},
		{
			Timestamp: timeTwo,
			Attributes: map[string]any{
				"destTag": "tag1",
			},
			Body: map[string]any{
				"key3": "tag1value3",
				"key4": "tag1value4",
			},
		},
		{
			Timestamp: timeOne,
			Attributes: map[string]any{
				"destTag": "tag2",
			},
			Body: map[string]any{
				"key1": "value1",
				"key2": "value2",
				"object": map[string]any{
					"key3": "value3",
					"key4": "value4",
				},
			},
		},
		{
			Timestamp: timeOne,
			Attributes: map[string]any{
				"destTag": "tag3",
			},
			Body: map[string]any{
				"key1": "tag3value1",
				"key2": "tag3value2",
				"object": map[string]any{
					"key1": "value1",
					"key2": "value2",
				},
			},
		},
		{
			Timestamp: timeTwo,
			Attributes: map[string]any{
				"destTag": "tag3",
			},
			Body: map[string]any{
				"key3": "tag3value3",
				"key4": "tag3value4",
				"object": map[string]any{
					"key1": "value1",
					"key2": "value2",
				},
			},
		},
	}
}

type entrytuple struct {
	timestamp uint64
	record    map[string]any
}

func validateArrayHeaderForwardMode(t *testing.T, b []byte) []byte {
	t.Helper()

	// [<tag>, <entries>]
	sz, rest, err := msgp.ReadArrayHeaderBytes(b)
	require.NoError(t, err)
	require.Equal(t, uint32(2), sz)
	return rest
}

func validateTag(t *testing.T, b []byte, expected string) []byte {
	t.Helper()

	tag, rest, err := msgp.ReadStringBytes(b)
	require.NoError(t, err)
	require.Equal(t, expected, tag)
	return rest
}

func getEntries(t *testing.T, b []byte) ([]byte, []entrytuple) {
	t.Helper()

	// Get number of entries
	sz, rest, err := msgp.ReadArrayHeaderBytes(b)
	require.NoError(t, err)

	entries := make([]entrytuple, 0, sz)
	for range int(sz) {
		entry := entrytuple{}

		// [<time>, <record>]
		sz, rest, err = msgp.ReadArrayHeaderBytes(rest)
		require.NoError(t, err)
		require.Equal(t, uint32(2), sz)

		// <time>
		extension := &fluentExtTime{}
		rest, err = msgp.ReadExtensionBytes(rest, extension)
		require.NoError(t, err)
		entry.timestamp = extension.UnixTsNano

		var record map[string]any
		record, rest, err = msgp.ReadMapStrIntfBytes(rest, nil)
		require.NoError(t, err)
		require.NotEmpty(t, record)
		entry.record = record

		entries = append(entries, entry)
	}
	return rest, entries
}

func validateEntries(t *testing.T, entries []entrytuple, expectedentries []entrytuple) {
	t.Helper()

	require.Equal(t, len(expectedentries), len(entries), "entry count mismatch")

	// Compare the slices ignoring their order
	require.ElementsMatch(t, expectedentries, entries, "entries differ ignoring order")
}

func requireFluentBatchCleared(t *testing.T, e *fluentEncoder) {
	t.Helper()

	require.Empty(t, e.batchToSend)
	for i, log := range e.batchToSend[:cap(e.batchToSend)] {
		require.Nil(t, log, "scratch slot %d retains a log reference", i)
	}
}

func freezeLogsInBatch(logBatch *types.LogBatch) {
	for _, log := range logBatch.Logs {
		log.Freeze()
	}
}
