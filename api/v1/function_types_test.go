package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestFunctionSpecFromYAML(t *testing.T) {
	yamlStr := `apiVersion: adx-mon.azure.com/v1
kind: Function
metadata:
  name: prom_increase
  namespace: adx-mon
spec:
  database: Metrics
  body: |
    .create-or-alter function prom_increase (T:(Timestamp:datetime, SeriesId: long, Labels:dynamic, Value:real), interval:timespan=1m) {
    T
    | where isnan(Value)==false
    | extend h=SeriesId
    | partition hint.strategy=shuffle by h (
      as Series
      | order by h, Timestamp asc
      | extend prevVal=prev(Value)
      | extend diff=Value-prevVal
      | extend Value=case(h == prev(h), case(diff < 0, next(Value)-Value, diff), real(0))
      | project-away prevVal, diff, h
    )}`

	var fn Function
	err := yaml.Unmarshal([]byte(yamlStr), &fn)
	require.NoError(t, err)
	require.Equal(t, "Metrics", fn.Spec.Database)
	require.Equal(t, "prom_increase", fn.GetName())
	require.Equal(t, "adx-mon", fn.GetNamespace())
}
