package rules

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

var alertruleExample = `apiVersion: adx-mon.azure.com/v1
kind: AlertRule
metadata:
  name: bar
  namespace: foo
spec:
  database: SomeDB
  interval: 1h
  query: |
    BadThings
    | where stuff  > 1
    | summarize count() by region
    | extend Severity=3
    | extend  Title = "Bad Things!"
    | extend  Summary  =  "Bad Things! Oh no"
    | extend CorrelationId = region
    | extend TSG="http://gofixit.com"
  autoMitigateAfter: 2h
  destination: "peoplewhocare"
`

func TestFromPath(t *testing.T) {
	testdir := t.TempDir()
	testfile := filepath.Join(testdir, "test.yaml")
	err := ioutil.WriteFile(testfile, []byte(alertruleExample), 0644)
	require.NoError(t, err)

	type testcase struct {
		name      string
		path      string
		expectErr bool
	}

	testcases := []testcase{
		{
			name:      "valid directory",
			path:      testdir,
			expectErr: false,
		},
		{
			name:      "invalid directory",
			path:      "/tmp/doesnotexist",
			expectErr: true,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			store, err := FromPath(tc.path, "eastus")
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, "foo", store.rules[0].Namespace)
				require.Equal(t, "bar", store.rules[0].Name)
			}
		})
	}
}
