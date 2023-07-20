package v1_test

import (
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
