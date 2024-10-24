package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

func TestFunctionSpecFromYAML(t *testing.T) {
	yamlStr := `apiVersion: adx-mon.azure.com/v1
kind: Function
metadata:
  name: prom_increase
spec:
  database: test-db
  name: test-fn
  body: |
    T
    | extend h=SeriesId
    | partition hint.strategy=shuffle by h (
      as Series
      | order by h, Timestamp asc
      | extend prevVal=prev(Value)
      | extend diff=Value-prevVal
      | extend Value=case(h == prev(h), case(diff < 0, next(Value)-Value, diff), real(0))
      | project-away prevVal, diff, h
    )
  parameters:
    - name: T
      type: record
      fields:
        - name: Timestamp
          type: datetime
        - name: SeriesId
          type: long
        - name: Labels
          type: dynamic
        - name: Value
          type: real
    - name: interval
      type: timespan
      default: 1m`

	var fn Function
	err := yaml.Unmarshal([]byte(yamlStr), &fn)
	require.NoError(t, err)
	require.Equal(t, "test-db", fn.Spec.Database)
}
