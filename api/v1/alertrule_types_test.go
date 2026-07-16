package v1_test

import (
	"encoding/json"
	"testing"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/stretchr/testify/require"
)

func TestAlertRuleSpec_UnmarshalJSON_CriteriaList(t *testing.T) {
	data := `{"autoMitigateAfter":"1h","criteria":{"cloud":["AzureCloud","AGC"]},"database":"DB","destination":"Destination","interval":"5m","query":"Query\n"}`
	a := &v1.AlertRuleSpec{}
	err := a.UnmarshalJSON([]byte(data))
	require.NoError(t, err)
	require.Equal(t, "1h0m0s", a.AutoMitigateAfter.Duration.String())
	require.Equal(t, "DB", a.Database)
	require.Equal(t, "Destination", a.Destination)
	require.Equal(t, "5m0s", a.Interval.Duration.String())
	require.Equal(t, "Query\n", a.Query)
	require.Equal(t, map[string][]string{"cloud": {"AzureCloud", "AGC"}}, a.Criteria)
}

func TestAlertRuleSpec_UnmarshalJSON_CriteriaString(t *testing.T) {
	// This is an example of the old format for the Criteria field.  It is a map[string]string instead of a map[string][]string.
	data := `{"autoMitigateAfter":"1h","criteria":{"cloud":"AzureCloud"},"database":"DB","destination":"Destination","interval":"5m","query":"Query\n"}`
	a := &v1.AlertRuleSpec{}
	err := a.UnmarshalJSON([]byte(data))
	require.NoError(t, err)
	require.Equal(t, "1h0m0s", a.AutoMitigateAfter.Duration.String())
	require.Equal(t, "DB", a.Database)
	require.Equal(t, "Destination", a.Destination)
	require.Equal(t, "5m0s", a.Interval.Duration.String())
	require.Equal(t, "Query\n", a.Query)
	require.Equal(t, map[string][]string{"cloud": {"AzureCloud"}}, a.Criteria)
}

func TestAlertRuleSpec_UnmarshalJSON_OmittedOptionalFields(t *testing.T) {
	a := &v1.AlertRuleSpec{}

	require.NoError(t, a.UnmarshalJSON([]byte(`{"database":"DB","query":"Query"}`)))
	require.Equal(t, "DB", a.Database)
	require.Equal(t, "Query", a.Query)
	require.Empty(t, a.Destination)
}

func TestAlertRuleSpec_UnmarshalJSON_OptionalFieldsAndTypes(t *testing.T) {
	for _, tt := range []struct {
		name string
		data string
		err  string
	}{
		{name: "invalid database", data: `{"database":42,"query":"Query"}`, err: `field "database" must be a string`},
		{name: "invalid query", data: `{"database":"DB","query":42}`, err: `field "query" must be a string`},
		{name: "criteria array", data: `{"criteria":["cloud"]}`, err: `field "criteria" must be an object`},
		{name: "criteria string", data: `{"criteria":"cloud"}`, err: `field "criteria" must be an object`},
		{name: "criteria null", data: `{"criteria":null}`, err: `field "criteria" must be an object`},
		{name: "criteria numeric value", data: `{"criteria":{"cloud":42}}`, err: `field "criteria.cloud" must be a string or list of strings`},
		{name: "criteria object value", data: `{"criteria":{"cloud":{"name":"AzureCloud"}}}`, err: `field "criteria.cloud" must be a string or list of strings`},
		{name: "criteria non-string list member", data: `{"criteria":{"cloud":["AzureCloud",42]}}`, err: `field "criteria.cloud" list values must be strings`},
		{name: "invalid criteria expression", data: `{"criteriaExpression":42}`, err: `field "criteriaExpression" must be a string`},
		{name: "null criteria expression", data: `{"criteriaExpression":null}`, err: `field "criteriaExpression" must be a string`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := &v1.AlertRuleSpec{}
			require.ErrorContains(t, a.UnmarshalJSON([]byte(tt.data)), tt.err)
		})
	}
}

func TestAlertRuleSpec_ZeroValueRoundTrip(t *testing.T) {
	original := v1.AlertRuleSpec{}
	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded v1.AlertRuleSpec
	require.NoError(t, json.Unmarshal(data, &decoded))
	require.Equal(t, original, decoded)

	require.NoError(t, json.Unmarshal([]byte(`{}`), &decoded))
	require.Equal(t, v1.AlertRuleSpec{}, decoded)
}
