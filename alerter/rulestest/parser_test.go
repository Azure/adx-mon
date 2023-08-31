package rulestest

import (
	"fmt"
	"testing"

	"github.com/lithammer/dedent"
	"github.com/stretchr/testify/require"
)

func TestGetDatatableStmt(t *testing.T) {
	testCases := []struct {
		name           string
		input          string // NOTE: Be careful to use spaces and not tabs when indenting.
		liftedLabels   []string
		expectedOutput string
		expectedErr    error
	}{
		{
			name: "test with lifted labels",
			input: dedent.Dedent(`
				name: basic-test1
				values:
				- 'CPUContainerSpecShares{Underlay="cx-test1", Overlay="cx-test2", ClusterID="0000000000"} 0x3 _  1+2x2'
				interval: 2m
				expected_alerts:
				- name: FakeAlert1
				  eval_at: 14m`),
			liftedLabels: []string{"Underlay", "RPTenant"},
			expectedOutput: `let CPUContainerSpecShares = datatable(Timestamp:datetime, SeriesId:long, Labels:dynamic, Value:real, Underlay:string, RPTenant:string) [
	datetime(0001-01-01T00:00:00Z), 0xa7ef2e91d03a1c0c, dynamic({"ClusterID":"0000000000","Overlay":"cx-test2"}), 0.000000,"cx-test1","",
	datetime(0001-01-01T00:02:00Z), 0xa7ef2e91d03a1c0c, dynamic({"ClusterID":"0000000000","Overlay":"cx-test2"}), 0.000000,"cx-test1","",
	datetime(0001-01-01T00:04:00Z), 0xa7ef2e91d03a1c0c, dynamic({"ClusterID":"0000000000","Overlay":"cx-test2"}), 0.000000,"cx-test1","",
	datetime(0001-01-01T00:06:00Z), 0xa7ef2e91d03a1c0c, dynamic({"ClusterID":"0000000000","Overlay":"cx-test2"}), 0.000000,"cx-test1","",
	datetime(0001-01-01T00:10:00Z), 0xa7ef2e91d03a1c0c, dynamic({"ClusterID":"0000000000","Overlay":"cx-test2"}), 1.000000,"cx-test1","",
	datetime(0001-01-01T00:12:00Z), 0xa7ef2e91d03a1c0c, dynamic({"ClusterID":"0000000000","Overlay":"cx-test2"}), 3.000000,"cx-test1","",
	datetime(0001-01-01T00:14:00Z), 0xa7ef2e91d03a1c0c, dynamic({"ClusterID":"0000000000","Overlay":"cx-test2"}), 5.000000,"cx-test1","",
];
`,
		},
		{
			name: "test with no lifted labels",
			input: dedent.Dedent(`
				name: basic-test1
				values:
				- 'CPUContainerSpecShares{Underlay="cx-test1", Overlay="cx-test2", ClusterID="0000000000"} 0x3 _  1+2x2'
				interval: 2m
				expected_alerts:
				- name: FakeAlert1
				  eval_at: 14m`),
			expectedOutput: `let CPUContainerSpecShares = datatable(Timestamp:datetime, SeriesId:long, Labels:dynamic, Value:real) [
	datetime(0001-01-01T00:00:00Z), 0xa7ef2e91d03a1c0c, dynamic({"ClusterID":"0000000000","Overlay":"cx-test2","Underlay":"cx-test1"}), 0.000000,
	datetime(0001-01-01T00:02:00Z), 0xa7ef2e91d03a1c0c, dynamic({"ClusterID":"0000000000","Overlay":"cx-test2","Underlay":"cx-test1"}), 0.000000,
	datetime(0001-01-01T00:04:00Z), 0xa7ef2e91d03a1c0c, dynamic({"ClusterID":"0000000000","Overlay":"cx-test2","Underlay":"cx-test1"}), 0.000000,
	datetime(0001-01-01T00:06:00Z), 0xa7ef2e91d03a1c0c, dynamic({"ClusterID":"0000000000","Overlay":"cx-test2","Underlay":"cx-test1"}), 0.000000,
	datetime(0001-01-01T00:10:00Z), 0xa7ef2e91d03a1c0c, dynamic({"ClusterID":"0000000000","Overlay":"cx-test2","Underlay":"cx-test1"}), 1.000000,
	datetime(0001-01-01T00:12:00Z), 0xa7ef2e91d03a1c0c, dynamic({"ClusterID":"0000000000","Overlay":"cx-test2","Underlay":"cx-test1"}), 3.000000,
	datetime(0001-01-01T00:14:00Z), 0xa7ef2e91d03a1c0c, dynamic({"ClusterID":"0000000000","Overlay":"cx-test2","Underlay":"cx-test1"}), 5.000000,
];
`,
		},
		{
			name: "test with all lifted labels",
			input: dedent.Dedent(`
				name: basic-test1
				values:
				- 'CPUContainerSpecShares{Underlay="cx-test1", Overlay="cx-test2", ClusterID="0000000000"} 0x3 _  1+2x2'
				interval: 2m
				expected_alerts:
				- name: FakeAlert1
				  eval_at: 14m`),
			liftedLabels: []string{"Underlay", "Overlay", "ClusterID", "RPTenant"},

			expectedOutput: `let CPUContainerSpecShares = datatable(Timestamp:datetime, SeriesId:long, Labels:dynamic, Value:real, Underlay:string, Overlay:string, ClusterID:string, RPTenant:string) [
	datetime(0001-01-01T00:00:00Z), 0xa7ef2e91d03a1c0c, dynamic({}), 0.000000,"cx-test1","cx-test2","0000000000","",
	datetime(0001-01-01T00:02:00Z), 0xa7ef2e91d03a1c0c, dynamic({}), 0.000000,"cx-test1","cx-test2","0000000000","",
	datetime(0001-01-01T00:04:00Z), 0xa7ef2e91d03a1c0c, dynamic({}), 0.000000,"cx-test1","cx-test2","0000000000","",
	datetime(0001-01-01T00:06:00Z), 0xa7ef2e91d03a1c0c, dynamic({}), 0.000000,"cx-test1","cx-test2","0000000000","",
	datetime(0001-01-01T00:10:00Z), 0xa7ef2e91d03a1c0c, dynamic({}), 1.000000,"cx-test1","cx-test2","0000000000","",
	datetime(0001-01-01T00:12:00Z), 0xa7ef2e91d03a1c0c, dynamic({}), 3.000000,"cx-test1","cx-test2","0000000000","",
	datetime(0001-01-01T00:14:00Z), 0xa7ef2e91d03a1c0c, dynamic({}), 5.000000,"cx-test1","cx-test2","0000000000","",
];
`,
		},
		{
			name: "test with no labels",
			input: dedent.Dedent(`
				name: basic-test1
				values:
				- 'CPUContainerSpecShares 0+3x3'
				interval: 2m
				expected_alerts:
				- name: FakeAlert1
				  eval_at: 14m`),
			liftedLabels: []string{"Underlay"},

			expectedOutput: `let CPUContainerSpecShares = datatable(Timestamp:datetime, SeriesId:long, Labels:dynamic, Value:real, Underlay:string) [
	datetime(0001-01-01T00:00:00Z), 0xef46db3751d8e999, dynamic({}), 0.000000,"",
	datetime(0001-01-01T00:02:00Z), 0xef46db3751d8e999, dynamic({}), 3.000000,"",
	datetime(0001-01-01T00:04:00Z), 0xef46db3751d8e999, dynamic({}), 6.000000,"",
	datetime(0001-01-01T00:06:00Z), 0xef46db3751d8e999, dynamic({}), 9.000000,"",
];
`,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: %s", i, tc.name), func(t *testing.T) {
			trequire := require.New(t)
			actual, err := parse(tc.input)
			trequire.NoError(err)
			stmt, err := actual.getDatatableStmt(tc.liftedLabels)
			trequire.Equal(err, tc.expectedErr)
			trequire.Equal(stmt, tc.expectedOutput)
			// now - interval.... engine.NewQueryContext
		})
	}
}
