package rulestest

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	testCases := []struct {
		name        string
		input       string
		expected    Test
		expectedErr error
	}{
		{
			name: "valid test",
			input: `
name: basic-test1
values:
- 'CPUContainerSpecShares{Underlay="cx-test1", Overlay="cx-test2", ClusterID="0000000000"} 0x15 1+1x20'
interval: 2m
`,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: %s", i, tc.name), func(t *testing.T) {
			actual, err := Parse(tc.input)
			if err != tc.expectedErr {
				t.Errorf("Expected error %v, got %v", tc.expectedErr, err)
			}
			if !reflect.DeepEqual(actual, tc.expected) {
				t.Errorf("Expected %v, got %v", tc.expected, actual)
			}
		})
	}
}

func TestGetDatatableStmt(t *testing.T) {
	testCases := []struct {
		name           string
		input          string
		expectedOutput string
		expectedErr    error
	}{
		{
			name: "valid test",
			input: `
name: basic-test1
values:
- 'CPUContainerSpecShares{Underlay="cx-test1", Overlay="cx-test2", ClusterID="0000000000"} 0x15 1+1x20'
interval: 2m
`,
			expectedOutput: `let CPUContainerSpecShares datatable(Date:datetime, Event:string, MoreData:dynamic) [];`,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: %s", i, tc.name), func(t *testing.T) {
			trequire := require.New(t)
			actual, err := Parse(tc.input)
			trequire.NoError(err)
			stmt, err := actual.GetDatatableStmt(nil)
			trequire.Equal(stmt, tc.expectedOutput)
		})
	}
}

func TestValidate(t *testing.T) {
	testCases := []struct {
		name        string
		input       Test
		expectedErr error
	}{}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d: %s", i, tc.name), func(t *testing.T) {
			err := tc.input.Validate()
			if err != tc.expectedErr {
				t.Errorf("Expected error %v, got %v", tc.expectedErr, err)
			}
		})
	}
}
