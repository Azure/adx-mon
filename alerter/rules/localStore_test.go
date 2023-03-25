package rules

import (
	"strings"
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

func TestFromPAth(t *testing.T) {
	s := &fileStore{}
	err := s.fromStream(strings.NewReader(alertruleExample), "newmexico")
	require.NoError(t, err)
	require.Equal(t, "foo", s.rules[0].Namespace)
	require.Equal(t, "bar", s.rules[0].Name)
}
