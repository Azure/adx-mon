package prompb

import (
	"io"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const sampleData = `# HELP http_requests_total The total number of HTTP requests.
# TYPE http_requests_total counter
http_requests_total{method="post",code="200"} 1027 1395066363000
http_requests_total{method="post",code="400"}    3 1395066363000

# Escaping in label values:
msdos_file_access_time_seconds{path="C:\\DIR\\FILE.TXT",error="Cannot find file:\n\"FILE.TXT\""} 1.458255915e9

# Minimalistic line:
metric_without_timestamp_and_labels 12.47

# A weird metric from before the epoch:
something_weird{problem="division by zero"} +Inf -3982045

# A histogram, which has a pretty complex representation in the text format:
# HELP http_request_duration_seconds A histogram of the request duration.
# TYPE http_request_duration_seconds histogram
http_request_duration_seconds_bucket{le="0.05"} 24054
http_request_duration_seconds_bucket{le="0.1"} 33444
http_request_duration_seconds_bucket{le="0.2"} 100392
http_request_duration_seconds_bucket{le="0.5"} 129389
http_request_duration_seconds_bucket{le="1"} 133988
http_request_duration_seconds_bucket{le="+Inf"} 144320
http_request_duration_seconds_sum 53423
http_request_duration_seconds_count 144320

# Finally a summary, which has a complex representation, too:
# HELP rpc_duration_seconds A summary of the RPC duration in seconds.
# TYPE rpc_duration_seconds summary
rpc_duration_seconds{quantile="0.01"} 3102
rpc_duration_seconds{quantile="0.05"} 3272
rpc_duration_seconds{quantile="0.5"} 4773
rpc_duration_seconds{quantile="0.9"} 9001
rpc_duration_seconds{quantile="0.99"} 76656
rpc_duration_seconds_sum 1.7560473e+07
rpc_duration_seconds_count 2693`

func TestIterator_ReadsLinesCorrectly(t *testing.T) {
	data := "line1\nline2\nline3\n"
	r := io.NopCloser(strings.NewReader(data))
	iter := NewIterator(r)
	defer iter.Close()

	require.True(t, iter.Next())
	require.Equal(t, "line1", iter.Value())

	require.True(t, iter.Next())
	require.Equal(t, "line2", iter.Value())

	require.True(t, iter.Next())
	require.Equal(t, "line3", iter.Value())

	require.False(t, iter.Next())
}

func TestIterator_EmptyInput(t *testing.T) {
	data := ""
	r := io.NopCloser(strings.NewReader(data))
	iter := NewIterator(r)
	defer iter.Close()

	require.False(t, iter.Next())
}

func TestIterator_SingleLine(t *testing.T) {
	data := "single line"
	r := io.NopCloser(strings.NewReader(data))
	iter := NewIterator(r)
	defer iter.Close()

	require.True(t, iter.Next())
	require.Equal(t, "single line", iter.Value())

	require.False(t, iter.Next())
}

func TestIterator_CloseReader(t *testing.T) {
	data := "line1\nline2\n"
	r := io.NopCloser(strings.NewReader(data))
	iter := NewIterator(r)
	require.NoError(t, iter.Close())
}

func TestIterator_SkipComments(t *testing.T) {
	r := io.NopCloser(strings.NewReader(sampleData))
	iter := NewIterator(r)
	defer iter.Close()

	for iter.Next() {
		if strings.HasPrefix(iter.Value(), "#") {
			t.Fatalf("unexpected comment: %s", iter.Value())
		}
	}
	require.NoError(t, iter.Close())
}

func TestIterator_TimeSeries_Malformed(t *testing.T) {
	for _, c := range []string{
		`http_requests_total`,
		`http_requests_total{ } 1027 1395066363000`,
		`http_requests_total{key`,
		`http_requests_total{key="`,
		`http_requests_total{key="a`,
		`http_requests_total{key="a"  ,`,
		`http_requests_total{key="a,`,
		`http_requests_total{key="a","}`,
		`http_requests_total{key="a",b="c"}     a`,
		`http_requests_total{key="a",b="c"}     1 a`,
		`http_requests_total    {key="a",b="c"}     1 a`,
		`http_requests_total    {}     1 a`,
		`http_requests_total    {}    `,
		`http_requests_total    {a="b",=} 1027`,
	} {
		t.Run(c, func(t *testing.T) {
			iter := NewIterator(io.NopCloser(strings.NewReader(c)))
			defer iter.Close()

			require.True(t, iter.Next())
			_, err := iter.TimeSeries()
			require.Error(t, err)
		})
	}
}

func TestIterator_TimeSeries(t *testing.T) {
	for _, c := range []struct {
		input string
		want  TimeSeries
	}{
		{
			input: `http_requests_total{method="post",code="200"} 1027 1395066363000`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("http_requests_total")},
					{Name: []byte("method"), Value: []byte("post")},
					{Name: []byte("code"), Value: []byte("200")},
				},
				Samples: []Sample{
					{Value: 1027, Timestamp: 1395066363000},
				},
			},
		},
		{
			input: `http_requests_total{method="post",code="400"} 3 1395066363000`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("http_requests_total")},
					{Name: []byte("method"), Value: []byte("post")},
					{Name: []byte("code"), Value: []byte("400")},
				},
				Samples: []Sample{
					{Value: 3, Timestamp: 1395066363000},
				},
			},
		},
		{
			input: `msdos_file_access_time_seconds{path="C:\\DIR\\FILE.TXT",error="Cannot find file:\n\"FILE.TXT\""} 1.458255915e9`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("msdos_file_access_time_seconds")},
					{Name: []byte("path"), Value: []byte("C:\\DIR\\FILE.TXT")},
					{Name: []byte("error"), Value: []byte("Cannot find file:\n\"FILE.TXT\"")},
				},
				Samples: []Sample{
					{Value: 1.458255915e9, Timestamp: 0},
				},
			},
		},
		{
			input: `metric_without_timestamp_and_labels 12.47`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("metric_without_timestamp_and_labels")},
				},
				Samples: []Sample{
					{Value: 12.47, Timestamp: 0},
				},
			},
		},
		{
			input: `something_weird{problem="division by zero"} +Inf -3982045`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("something_weird")},
					{Name: []byte("problem"), Value: []byte("division by zero")},
				},
				Samples: []Sample{
					{Value: math.Inf(1), Timestamp: -3982045},
				},
			},
		},
		{
			input: `http_request_duration_seconds_bucket{le="0.05"} 24054`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("http_request_duration_seconds_bucket")},
					{Name: []byte("le"), Value: []byte("0.05")},
				},
				Samples: []Sample{
					{Value: 24054, Timestamp: 0},
				},
			},
		},
		{
			input: `http_request_duration_seconds_bucket{le="0.1"} 33444`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("http_request_duration_seconds_bucket")},
					{Name: []byte("le"), Value: []byte("0.1")},
				},
				Samples: []Sample{
					{Value: 33444, Timestamp: 0},
				},
			},
		},
		{
			input: `http_request_duration_seconds_bucket{le="0.2"} 100392`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("http_request_duration_seconds_bucket")},
					{Name: []byte("le"), Value: []byte("0.2")},
				},
				Samples: []Sample{
					{Value: 100392, Timestamp: 0},
				},
			},
		},
		{
			input: `http_request_duration_seconds_bucket{le="0.5"} 129389`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("http_request_duration_seconds_bucket")},
					{Name: []byte("le"), Value: []byte("0.5")},
				},
				Samples: []Sample{
					{Value: 129389, Timestamp: 0},
				},
			},
		},
		{
			input: `http_request_duration_seconds_bucket{le="1"} 133988`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("http_request_duration_seconds_bucket")},
					{Name: []byte("le"), Value: []byte("1")},
				},
				Samples: []Sample{
					{Value: 133988, Timestamp: 0},
				},
			},
		},
		{
			input: `http_request_duration_seconds_bucket{le="+Inf"} 144320`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("http_request_duration_seconds_bucket")},
					{Name: []byte("le"), Value: []byte("+Inf")},
				},
				Samples: []Sample{
					{Value: 144320, Timestamp: 0},
				},
			},
		},
		{
			input: `http_request_duration_seconds_sum 53423`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("http_request_duration_seconds_sum")},
				},
				Samples: []Sample{
					{Value: 53423, Timestamp: 0},
				},
			},
		},
		{
			input: `http_request_duration_seconds_count 144320`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("http_request_duration_seconds_count")},
				},
				Samples: []Sample{
					{Value: 144320, Timestamp: 0},
				},
			},
		},
		{
			input: `rpc_duration_seconds{quantile="0.01"} 3102`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("rpc_duration_seconds")},
					{Name: []byte("quantile"), Value: []byte("0.01")},
				},
				Samples: []Sample{
					{Value: 3102, Timestamp: 0},
				},
			},
		},
		{
			input: `rpc_duration_seconds{quantile="0.05"} 3272`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("rpc_duration_seconds")},
					{Name: []byte("quantile"), Value: []byte("0.05")},
				},
				Samples: []Sample{
					{Value: 3272, Timestamp: 0},
				},
			},
		},
		{
			input: `rpc_duration_seconds{quantile="0.5"} 4773`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("rpc_duration_seconds")},
					{Name: []byte("quantile"), Value: []byte("0.5")},
				},
				Samples: []Sample{
					{Value: 4773, Timestamp: 0},
				},
			},
		},
		{
			input: `rpc_duration_seconds{quantile="0.9"} 9001`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("rpc_duration_seconds")},
					{Name: []byte("quantile"), Value: []byte("0.9")},
				},
				Samples: []Sample{
					{Value: 9001, Timestamp: 0},
				},
			},
		},
		{
			input: `rpc_duration_seconds{quantile="0.99"} 76656`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("rpc_duration_seconds")},
					{Name: []byte("quantile"), Value: []byte("0.99")},
				},
				Samples: []Sample{
					{Value: 76656, Timestamp: 0},
				},
			},
		},
		{
			input: `rpc_duration_seconds_sum 1.7560473e+07`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("rpc_duration_seconds_sum")},
				},
				Samples: []Sample{
					{Value: 1.7560473e+07, Timestamp: 0},
				},
			},
		},
		{
			input: `rpc_duration_seconds_count 2693`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("rpc_duration_seconds_count")},
				},
				Samples: []Sample{
					{Value: 2693, Timestamp: 0},
				},
			},
		},
		{
			input: `rpc_duration_seconds_count{} 2693`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("rpc_duration_seconds_count")},
				},
				Samples: []Sample{
					{Value: 2693, Timestamp: 0},
				},
			},
		},
		{
			input: `container_cpu_usage {container="liveness-probe",id="/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod14e7c01a_0774_45f2_bb6a_b10e8e7f439e.slice/cri-containerd-51fa77b79a00e161e839762b7e4cbb6abfbaec68d3d7f56bd9665b9d48816894.scope",image="mcr.microsoft.com/oss/kubernetes-csi/livenessprobe:v2."} 0 1726114705191`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("container_cpu_usage")},
					{Name: []byte("container"), Value: []byte("liveness-probe")},
					{Name: []byte("id"), Value: []byte("/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod14e7c01a_0774_45f2_bb6a_b10e8e7f439e.slice/cri-containerd-51fa77b79a00e161e839762b7e4cbb6abfbaec68d3d7f56bd9665b9d48816894.scope")},
					{Name: []byte("image"), Value: []byte("mcr.microsoft.com/oss/kubernetes-csi/livenessprobe:v2.")},
				},
				Samples: []Sample{
					{Value: 0, Timestamp: 1726114705191},
				},
			},
		},
		{
			input: `node_cpu_usage_seconds_total 157399.56 1726236687470`,
			want: TimeSeries{
				Labels: []Label{
					{Name: []byte("__name__"), Value: []byte("node_cpu_usage_seconds_total")},
				},
				Samples: []Sample{
					{Value: 157399.56, Timestamp: 1726236687470},
				},
			},
		},
	} {
		t.Run(c.input, func(t *testing.T) {
			iter := NewIterator(io.NopCloser(strings.NewReader(c.input)))
			defer iter.Close()

			require.True(t, iter.Next())
			ts, err := iter.TimeSeries()
			require.NoError(t, err)
			require.Equal(t, c.want, ts)
		})
	}
}

func BenchmarkIterator_TimeSeries(b *testing.B) {
	sampleInput := `http_requests_total{method="post",code="200"} 1027 1395066363000`
	iter := NewIterator(io.NopCloser(strings.NewReader(sampleInput)))
	require.True(b, iter.Next())
	for i := 0; i < b.N; i++ {

		_, err := iter.TimeSeries()
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
		iter.Close()
	}
}
