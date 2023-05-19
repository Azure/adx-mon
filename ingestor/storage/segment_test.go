package storage_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/davidnarayan/go-flake"
	"github.com/stretchr/testify/require"
)

func TestNewSegment(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.NewSegment(dir, "Foo", nil)
	require.NoError(t, err)
	require.NoError(t, s.Write(context.Background(), []prompb.TimeSeries{newTimeSeries("Foo", map[string]string{"host": "bar"}, 0, 0)}))

	require.NotEmpty(t, s.Path())
	epoch := s.Epoch()

	s, err = storage.OpenSegment(s.Path())
	require.NoError(t, err)
	require.NoError(t, s.Write(context.Background(), []prompb.TimeSeries{newTimeSeries("Foo", map[string]string{"host": "bar"}, 0, 0)}))

	require.NotEmpty(t, s.Path())
	require.Equal(t, epoch, s.Epoch())
}

func TestSegment_CreatedAt(t *testing.T) {
	idgen, err := flake.New()
	require.NoError(t, err)

	id := idgen.NextId()

	num, err := strconv.ParseInt(id.String(), 16, 64)
	require.NoError(t, err)
	num = num >> (flake.HostBits + flake.SequenceBits)

	ts := flake.Epoch.Add(time.Duration(num) * time.Millisecond)
	println(ts.String(), time.Now().UTC().String())
}

func TestSegment_Corrupted(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.NewSegment(dir, "Foo", nil)
	require.NoError(t, err)
	require.NoError(t, s.Write(context.Background(), []prompb.TimeSeries{newTimeSeries("Foo", map[string]string{"host": "bar"}, 0, 0)}))
	require.NoError(t, s.Close())

	f, err := os.OpenFile(s.Path(), os.O_APPEND|os.O_RDWR, 0600)
	require.NoError(t, err)
	_, err = f.WriteString("a,")
	require.NoError(t, err)

	s, err = storage.OpenSegment(s.Path())

	b, err := s.Bytes()
	require.NoError(t, err)
	require.Equal(t, uint8('\n'), b[len(b)-1])

}

func TestSegment_Corrupted_BigFile(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.NewSegment(dir, "Foo", nil)
	require.NoError(t, err)
	require.NoError(t, s.Write(context.Background(), []prompb.TimeSeries{newTimeSeries("Foo", map[string]string{"host": "bar"}, 0, 0)}))
	require.NoError(t, s.Close())

	f, err := os.OpenFile(s.Path(), os.O_APPEND|os.O_RDWR, 0600)
	require.NoError(t, err)
	str := strings.Repeat("a", 8092)
	_, err = f.WriteString(fmt.Sprintf("%s,", str))
	require.NoError(t, err)

	s, err = storage.OpenSegment(s.Path())

	b, err := s.Bytes()
	require.NoError(t, err)
	require.Equal(t, uint8('\n'), b[len(b)-1])

	require.NoError(t, s.Write(context.Background(), []prompb.TimeSeries{newTimeSeries("Foo", map[string]string{"host": "bar"}, 1, 1)}))
	b, err = s.Bytes()
	require.NoError(t, err)
	require.Equal(t, `1970-01-01T00:00:00Z,5965899759670904239,"{""host"":""bar""}",0.000000000
1970-01-01T00:00:00.001Z,5965899759670904239,"{""host"":""bar""}",1.000000000
`, string(b))
}
