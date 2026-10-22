package kernel

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/google/uuid"
	"github.com/siderolabs/go-kmsg"
	"github.com/stretchr/testify/require"
)

const (
	testBootID         = "3a3a0609-7ee9-4e0c-8d73-ef21be1acc12"
	testPreviousBootID = "90d08893-686a-420e-9690-f5fdc36a9f01"
)

func testBootIDPath(t testing.TB) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "boot_id")
	require.NoError(t, os.WriteFile(path, []byte(testBootID+"\n"), 0600))
	return path
}

func TestKernelSourceCheckpoints(t *testing.T) {
	for _, tc := range []struct {
		name      string
		bootID    string
		sequence  uint64
		format    string
		sequences []int64
		want      []uint64
	}{
		{
			name: "missing checkpoint", format: "missing",
			sequences: []int64{0, 1, 2}, want: []uint64{0, 1, 2},
		},
		{
			name: "same boot restart", bootID: testBootID, sequence: 5,
			sequences: []int64{0, 4, 5, 6, 7}, want: []uint64{6, 7},
		},
		{
			name: "reboot resets sequence", bootID: testPreviousBootID, sequence: 5000,
			sequences: []int64{0, 1, 2, 3}, want: []uint64{0, 1, 2, 3},
		},
		{
			name: "new boot already exceeds old sequence", bootID: testPreviousBootID, sequence: 5,
			sequences: []int64{0, 5, 6}, want: []uint64{0, 5, 6},
		},
		{
			name: "acknowledged sequence zero", bootID: testBootID,
			sequences: []int64{0, 1}, want: []uint64{1},
		},
		{
			name: "previous boot sequence zero", bootID: testPreviousBootID,
			sequences: []int64{0, 1}, want: []uint64{0, 1},
		},
		{
			name: "legacy checkpoint", sequence: 5000, format: "legacy",
			sequences: []int64{0, 1, 2}, want: []uint64{0, 1, 2},
		},
		{
			name: "corrupt checkpoint", format: "corrupt",
			sequences: []int64{0, 1, 2}, want: []uint64{0, 1, 2},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, SafeFilename("Logs", "Kernel"))
			switch tc.format {
			case "missing":
			case "legacy":
				data := binary.LittleEndian.AppendUint64(nil, tc.sequence)
				require.NoError(t, os.WriteFile(path, data, 0600))
			case "corrupt":
				require.NoError(t, os.WriteFile(path, []byte("bad"), 0600))
			default:
				require.NoError(t, writeOffset(path, tc.sequence, uuid.MustParse(tc.bootID)))
			}

			source, err := NewKernelSource(KernelSourceConfig{
				CursorDirectory: dir,
				Targets:         []KernelTargetConfig{{Database: "Logs", Table: "Kernel"}},
			})
			require.NoError(t, err)
			source.bootIDPath = testBootIDPath(t)
			require.NoError(t, source.loadOffsets())

			packets := make(chan kmsg.Packet, len(tc.sequences))
			for _, sequence := range tc.sequences {
				packets <- kmsg.Packet{Message: kmsg.Message{
					SequenceNumber: sequence,
					Timestamp:      time.Now(),
					Message:        "current boot message",
					Priority:       kmsg.Info,
				}}
			}
			close(packets)
			source.reader = &mockKernelReader{ch: packets}
			output := make(chan *types.Log, len(tc.sequences))
			source.readKernelLogs(t.Context(), output)
			close(output)

			var got []uint64
			for log := range output {
				value, ok := log.GetAttributeValue(kernelSequenceAttr)
				require.True(t, ok)
				sequence, ok := value.(uint64)
				require.True(t, ok)
				got = append(got, sequence)
				source.ackSequence(sequence, source.targets[0].cursorFile)
				log.Reset()
				types.LogPool.Put(log)
			}
			require.Equal(t, tc.want, got)

			offset, err := readOffset(path, uuid.MustParse(testBootID))
			require.NoError(t, err)
			require.Equal(t, tc.want[len(tc.want)-1], offset)
			require.NoError(t, source.loadOffsets())
			require.NotNil(t, source.targets[0].processed)
			require.Equal(t, offset, *source.targets[0].processed)
		})
	}
}

func TestKernelSourceCheckpointMultipleTargets(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeOffset(filepath.Join(dir, SafeFilename("Logs", "A")), 5, uuid.MustParse(testBootID)))
	require.NoError(t, writeOffset(filepath.Join(dir, SafeFilename("Logs", "B")), 5000, uuid.MustParse(testPreviousBootID)))
	source, err := NewKernelSource(KernelSourceConfig{
		CursorDirectory: dir,
		Targets: []KernelTargetConfig{
			{Database: "Logs", Table: "A"},
			{Database: "Logs", Table: "B", PriorityFilter: "err"},
		},
	})
	require.NoError(t, err)
	source.bootIDPath = testBootIDPath(t)
	require.NoError(t, source.loadOffsets())

	packets := make(chan kmsg.Packet, 4)
	for _, message := range []kmsg.Message{
		{SequenceNumber: 1, Priority: kmsg.Info},
		{SequenceNumber: 2, Priority: kmsg.Err},
		{SequenceNumber: 6, Priority: kmsg.Err},
		{SequenceNumber: 7, Priority: kmsg.Debug},
	} {
		packets <- kmsg.Packet{Message: message}
	}
	close(packets)
	source.reader = &mockKernelReader{ch: packets}
	output := make(chan *types.Log, 8)
	source.readKernelLogs(t.Context(), output)
	close(output)

	var tables []string
	var sequences []uint64
	for log := range output {
		tables = append(tables, types.StringOrEmpty(log.GetAttributeValue(types.AttributeTableName)))
		value, ok := log.GetAttributeValue(kernelSequenceAttr)
		require.True(t, ok)
		sequence, ok := value.(uint64)
		require.True(t, ok)
		sequences = append(sequences, sequence)
		log.Reset()
		types.LogPool.Put(log)
	}
	require.Equal(t, []string{"B", "A", "B"}, tables)
	require.Equal(t, []uint64{2, 6, 6}, sequences)
}

func TestKernelSourceWithoutCursorDirectory(t *testing.T) {
	source, err := NewKernelSource(KernelSourceConfig{
		Targets: []KernelTargetConfig{{Database: "Logs", Table: "Kernel", processed: new(uint64(5000))}},
	})
	require.NoError(t, err)
	source.bootIDPath = filepath.Join(t.TempDir(), "missing")
	require.NoError(t, source.loadOffsets())
	require.Nil(t, source.targets[0].processed)
}

func TestKernelSourceBootIDFailure(t *testing.T) {
	for _, contents := range []string{"", "not-a-uuid", uuid.Nil.String()} {
		t.Run(contents, func(t *testing.T) {
			source, err := NewKernelSource(KernelSourceConfig{CursorDirectory: t.TempDir()})
			require.NoError(t, err)
			source.bootIDPath = filepath.Join(t.TempDir(), "boot_id")
			require.NoError(t, os.WriteFile(source.bootIDPath, []byte(contents), 0600))
			require.ErrorContains(t, source.Open(t.Context()), "read kernel boot ID")
		})
	}
	t.Run("missing", func(t *testing.T) {
		source, err := NewKernelSource(KernelSourceConfig{CursorDirectory: t.TempDir()})
		require.NoError(t, err)
		source.bootIDPath = filepath.Join(t.TempDir(), "missing")
		require.ErrorIs(t, source.Open(t.Context()), os.ErrNotExist)
	})
}

func TestOffsetRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor")
	for _, bootID := range []uuid.UUID{uuid.MustParse(testPreviousBootID), uuid.MustParse(testBootID)} {
		for _, sequence := range []uint64{0, 5000, ^uint64(0)} {
			require.NoError(t, writeOffset(path, sequence, bootID))
			offset, err := readOffset(path, bootID)
			require.NoError(t, err)
			require.Equal(t, sequence, offset)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestReadOffsetInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor")
	bootID := uuid.MustParse(testBootID)
	require.NoError(t, writeOffset(path, 5000, bootID))
	valid, err := os.ReadFile(path)
	require.NoError(t, err)
	unsupported := slices.Clone(valid)
	unsupported[8] = offsetVersion + 1
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"legacy", valid[:8]},
		{"truncated", valid[:len(valid)-1]},
		{"trailing data", append(slices.Clone(valid), 0)},
		{"unsupported version", unsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, os.WriteFile(path, tc.data, 0600))
			_, err := readOffset(path, bootID)
			require.Error(t, err)
		})
	}
	require.NoError(t, os.WriteFile(path, valid, 0600))
	_, err = readOffset(path, uuid.MustParse(testPreviousBootID))
	require.ErrorContains(t, err, "different boot")
	_, err = readOffset(filepath.Join(t.TempDir(), "missing"), bootID)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestWriteOffsetFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cursor")
	require.NoError(t, os.Mkdir(path, 0700))
	require.ErrorContains(t, writeOffset(path, 1, uuid.MustParse(testBootID)), "replace offset file")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.ErrorContains(t, writeOffset(filepath.Join(dir, "missing", "cursor"), 1, uuid.MustParse(testBootID)), "create offset file")
}
