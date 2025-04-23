package export

import (
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/stretchr/testify/require"
	"github.com/tinylib/msgp/msgp"
)

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
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
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

	rest := validateArrayHeaderForwardMode(t, b)
	rest = validateTag(t, rest, "tag1")
	rest, entries := getEntries(t, rest)
	validateEntries(t, entries, []entrytuple{
		{timestamp: timeOne, record: map[string]interface{}{"key1": "tag1value1", "key2": "tag1value2"}},
		{timestamp: timeTwo, record: map[string]interface{}{"key3": "tag1value3", "key4": "tag1value4"}},
	})

	rest = validateArrayHeaderForwardMode(t, rest)
	rest = validateTag(t, rest, "tag2")
	rest, entries = getEntries(t, rest)
	validateEntries(t, entries, []entrytuple{
		{timestamp: timeOne, record: map[string]interface{}{"key1": "value1", "key2": "value2", "object": map[string]interface{}{"key3": "value3", "key4": "value4"}}},
	})

	rest = validateArrayHeaderForwardMode(t, rest)
	rest = validateTag(t, rest, "tag3")
	rest, entries = getEntries(t, rest)
	validateEntries(t, entries, []entrytuple{
		{timestamp: timeOne, record: map[string]interface{}{"key1": "tag3value1", "key2": "tag3value2", "object": map[string]interface{}{"key1": "value1",
			"key2": "value2"}}},
		{timestamp: timeTwo, record: map[string]interface{}{"key3": "tag3value3", "key4": "tag3value4", "object": map[string]interface{}{"key1": "value1",
			"key2": "value2"}}},
	})

	// nothing more to read
	require.Equal(t, 0, len(rest))

	newLogs := []*types.LogLiteral{
		{
			Timestamp: timeTwo,
			Attributes: map[string]interface{}{
				"destTag": "tag5",
			},
			Body: map[string]interface{}{
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
	rest = validateArrayHeaderForwardMode(t, b)
	rest = validateTag(t, rest, "tag5")
	rest, entries = getEntries(t, rest)
	validateEntries(t, entries, []entrytuple{
		{timestamp: timeTwo, record: map[string]interface{}{"key1": "tag5value1"}},
	})

	// nothing more to read
	require.Equal(t, 0, len(rest))
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
			Attributes: map[string]interface{}{
				"destTag": "tag1",
			},
			Body: map[string]interface{}{
				"key1": "tag1value1",
				"key2": "tag1value2",
			},
		},
		{ // no dest tag
			Timestamp:  timeThree,
			Attributes: map[string]interface{}{},
			Body: map[string]interface{}{
				"not-routed":   "value1",
				"not-routed-2": "value2",
			},
		},
		{ // non-string dest tag
			Timestamp: timeThree,
			Attributes: map[string]interface{}{
				"destTag": 1,
			},
			Body: map[string]interface{}{
				"not-routed":   "value1",
				"not-routed-2": "value2",
			},
		},
		{
			Timestamp: timeTwo,
			Attributes: map[string]interface{}{
				"destTag": "tag1",
			},
			Body: map[string]interface{}{
				"key3": "tag1value3",
				"key4": "tag1value4",
			},
		},
		{
			Timestamp: timeOne,
			Attributes: map[string]interface{}{
				"destTag": "tag2",
			},
			Body: map[string]interface{}{
				"key1": "value1",
				"key2": "value2",
				"object": map[string]interface{}{
					"key3": "value3",
					"key4": "value4",
				},
			},
		},
		{
			Timestamp: timeOne,
			Attributes: map[string]interface{}{
				"destTag": "tag3",
			},
			Body: map[string]interface{}{
				"key1": "tag3value1",
				"key2": "tag3value2",
				"object": map[string]interface{}{
					"key1": "value1",
					"key2": "value2",
				},
			},
		},
		{
			Timestamp: timeTwo,
			Attributes: map[string]interface{}{
				"destTag": "tag3",
			},
			Body: map[string]interface{}{
				"key3": "tag3value3",
				"key4": "tag3value4",
				"object": map[string]interface{}{
					"key1": "value1",
					"key2": "value2",
				},
			},
		},
	}
}

type entrytuple struct {
	timestamp uint64
	record    map[string]interface{}
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

		var record map[string]interface{}
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

func freezeLogsInBatch(logBatch *types.LogBatch) {
	for _, log := range logBatch.Logs {
		log.Freeze()
	}
}
