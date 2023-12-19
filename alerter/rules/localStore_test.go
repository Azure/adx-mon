package rules

import (
	"os"
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

var multiRule = `apiVersion: adx-mon.azure.com/v1
kind: AlertRule
metadata:
  name: alertRuleOne
  namespace: namespaceOne
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

---

apiVersion: adx-mon.azure.com/v1
kind: AlertRule
metadata:
  name: alertRuleTwo
  namespace: namespaceTwo
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

// has incorrect indentation, which leads to unknown field errors
var invalidRuleExample = `apiVersion: adx-mon.azure.com/v1
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
	validFileDirectory := t.TempDir()
	testfile := filepath.Join(validFileDirectory, "test.yaml")
	err := os.WriteFile(testfile, []byte(alertruleExample), 0644)
	require.NoError(t, err)

	multiTestfile := filepath.Join(validFileDirectory, "zmultitest.yaml")
	err = os.WriteFile(multiTestfile, []byte(multiRule), 0644)
	require.NoError(t, err)

	owners := filepath.Join(validFileDirectory, "owners.txt")
	err = os.WriteFile(owners, []byte("eljefe"), 0644)
	require.NoError(t, err)

	invalidFileDirectory := t.TempDir()
	invalidTestfile := filepath.Join(invalidFileDirectory, "invalid.yaml")
	err = os.WriteFile(invalidTestfile, []byte(invalidRuleExample), 0644)
	require.NoError(t, err)

	type testcase struct {
		name      string
		path      string
		expectErr bool
	}

	testcases := []testcase{
		{
			name:      "valid directory",
			path:      validFileDirectory,
			expectErr: false,
		},
		{
			name:      "invalid yaml in directory should return error",
			path:      invalidFileDirectory,
			expectErr: true,
		},
		{
			name:      "non-existant directory",
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
				require.Equal(t, 3, len(store.rules))
				require.Equal(t, "foo", store.rules[0].Namespace)
				require.Equal(t, "bar", store.rules[0].Name)
				require.Equal(t, "namespaceOne", store.rules[1].Namespace)
				require.Equal(t, "alertRuleOne", store.rules[1].Name)
				require.Equal(t, "namespaceTwo", store.rules[2].Namespace)
				require.Equal(t, "alertRuleTwo", store.rules[2].Name)
			}
		})
	}
}
