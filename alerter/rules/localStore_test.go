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
  name: alert-rule-one
  namespace: namespace-one
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
  name: alert-rule-two
  namespace: namespace-two
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

var invalidNameExample = `apiVersion: adx-mon.azure.com/v1
kind: AlertRule
metadata:
  name: bar-Invalid
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

var invalidNamespaceExample = `apiVersion: adx-mon.azure.com/v1
kind: AlertRule
metadata:
  name: bar
  namespace: foo-Invalid
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

var invalidLabelExample = `apiVersion: adx-mon.azure.com/v1
kind: AlertRule
metadata:
  name: bar
  namespace: foo
  labels:
    -invalid-label: foo
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

var invalidAnnotationExample = `apiVersion: adx-mon.azure.com/v1
kind: AlertRule
metadata:
  name: bar
  namespace: foo
  annotations:
    -invalid-annotation: foo
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

	invalidFileDirectory := t.TempDir()
	invalidTestfile := filepath.Join(invalidFileDirectory, "invalid.yaml")
	err = os.WriteFile(invalidTestfile, []byte(invalidRuleExample), 0644)
	require.NoError(t, err)

	invalidNameFileDirectory := t.TempDir()
	invalidNameTestfile := filepath.Join(invalidNameFileDirectory, "invalidName.yaml")
	err = os.WriteFile(invalidNameTestfile, []byte(invalidNameExample), 0644)
	require.NoError(t, err)

	invalidNamespaceFileDirectory := t.TempDir()
	invalidNamespaceTestfile := filepath.Join(invalidNamespaceFileDirectory, "invalidNamespace.yaml")
	err = os.WriteFile(invalidNamespaceTestfile, []byte(invalidNamespaceExample), 0644)
	require.NoError(t, err)

	invalidLabelFileDirectory := t.TempDir()
	invalidLabelTestfile := filepath.Join(invalidLabelFileDirectory, "invalidLabel.yaml")
	err = os.WriteFile(invalidLabelTestfile, []byte(invalidLabelExample), 0644)
	require.NoError(t, err)

	invalidAnnotationFileDirectory := t.TempDir()
	invalidAnnotationTestfile := filepath.Join(invalidAnnotationFileDirectory, "invalidAnnotation.yaml")
	err = os.WriteFile(invalidAnnotationTestfile, []byte(invalidAnnotationExample), 0644)
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
		{
			name:      "invalid name in yaml should return error",
			path:      invalidNameFileDirectory,
			expectErr: true,
		},
		{
			name:      "invalid namespace in yaml should return error",
			path:      invalidNamespaceFileDirectory,
			expectErr: true,
		},
		{
			name:      "invalid label in yaml should return error",
			path:      invalidLabelFileDirectory,
			expectErr: true,
		},
		{
			name:      "invalid annotation in yaml should return error",
			path:      invalidAnnotationFileDirectory,
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
				require.Equal(t, "namespace-one", store.rules[1].Namespace)
				require.Equal(t, "alert-rule-one", store.rules[1].Name)
				require.Equal(t, "namespace-two", store.rules[2].Namespace)
				require.Equal(t, "alert-rule-two", store.rules[2].Name)
			}
		})
	}
}
