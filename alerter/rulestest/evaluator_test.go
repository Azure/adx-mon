package rulestest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/alerter/engine"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeClient struct {
}

func (c *fakeClient) Query(ctx context.Context, qc *engine.QueryContext) ([]*alert.Alert, error) {
	if !strings.HasPrefix(qc.Rule.Query, "let fake-table = datatable(") {
		return nil, fmt.Errorf("query expected to start with let statement: %s", qc.Rule.Query)
	}
	now := time.Time{}
	switch qc.Rule.Name {
	case "rule-1":
		if qc.EndTime == now.Add(time.Minute*30) {
			return []*alert.Alert{
				{
					Title: "alert-1",
				},
			}, nil
		}
		return nil, nil
	case "additional-alerts-1":
		return []*alert.Alert{
			{
				Title: "extra-alert",
			},
		}, nil
	case "missing-alerts-1":
		return []*alert.Alert{
			{
				Title: "alert-1",
			},
		}, nil
	case "failing-rule":
		return nil, fmt.Errorf("failed to execute failing-rule")
	}
	return nil, nil
}

func TestRunAndPrintTestsFails(t *testing.T) {
	testRules := []*rules.Rule{
		{
			Name:  "rule-1",
			Query: "fake-table | limit 1",
		},
		{
			Name:  "faling-rule",
			Query: "fake-table | limit 1",
		},
		{
			Name:  "missing-alerts-1",
			Query: "fake-table | limit 1",
		},
		{
			Name:  "additional-alerts-1",
			Query: "fake-table | limit 1",
		},
	}

	tests := []*Test{
		{
			// test passes, because there are no alerts in expectedAlerts
			name: "test-1-no-alerts",
			timeseries: map[string][]Timeseries{
				"fake-table": {
					{
						metric: "fake-table",
						values: []parser.SequenceValue{{Value: 0.1}},
					},
				},
			},
			expectedAlerts: []ExpectedAlerts{
				{
					Name: "rule-1",
				},
			},
		},
		{
			// test fails because there are no timeseries for fake-table
			name: "test-missing-let-statements",
			expectedAlerts: []ExpectedAlerts{
				{
					Name: "rule-1",
				},
			},
		},
		{
			// test fails because "missing-rule" does not exist in the rules.
			name: "test-1-missing-rule",
			timeseries: map[string][]Timeseries{
				"fake-table": {},
			},
			expectedAlerts: []ExpectedAlerts{
				{
					Name: "missing-rule",
				},
			},
		},
		{
			// test fails because "failing-rule" is configured to return an error from the fake client
			name: "test-1-query-fails",
			timeseries: map[string][]Timeseries{
				"fake-table": {},
			},
			expectedAlerts: []ExpectedAlerts{
				{
					Name: "failing-rule",
				},
			},
		},
		{
			// fails because the fake-client is configured to return the wrong set of alerts for each below rule.
			name: "test-alerts-mismatch",
			timeseries: map[string][]Timeseries{
				"fake-table": {},
			},
			expectedAlerts: []ExpectedAlerts{
				{
					Name: "additional-alerts-1",
					Alerts: []*alert.Alert{
						{
							Title: "alert-1",
						},
						{
							Title: "alert-2",
						},
					},
				},
				{
					Name: "missing-alerts-1",
					Alerts: []*alert.Alert{
						{
							Title: "alert-1",
						},
						{
							Title: "alert-2",
						},
					},
				},
			},
		},
	}

	e, err := NewEvaluator(&fakeClient{}, testRules, tests)
	require.NoError(t, err)
	buf := &strings.Builder{}
	err = e.RunAndPrintTests(buf)
	assert.EqualError(t, err, fmt.Sprintf("%d tests failed out of %d", 5, 6))

	expectedOutput := `
Test test-1-no-alerts - rule-1 @ 0s PASS
Test test-missing-let-statements - rule-1 @ 0s FAIL:
		failed to execute query: query expected to start with let statement: 
fake-table | limit 1
Test test-1-missing-rule - missing-rule @ 0s FAIL:
		rule ""missing-rule"" not found
Test test-1-query-fails - failing-rule @ 0s FAIL:
		rule ""failing-rule"" not found
Test test-alerts-mismatch - additional-alerts-1 @ 0s FAIL:
	
		Exepected alerts that were not raised alerts:
		
			Title: alert-1
			Summary: 
			Description: 
			Severity: 0
			Source: 
			Destination: 
			CorrelationID: 
			CustomFields: map[]
		
			Title: alert-2
			Summary: 
			Description: 
			Severity: 0
			Source: 
			Destination: 
			CorrelationID: 
			CustomFields: map[]
		
		Additional alerts raised that were not expected:
		
			Title: extra-alert
			Summary: 
			Description: 
			Severity: 0
			Source: 
			Destination: 
			CorrelationID: 
			CustomFields: map[]
		
Test test-alerts-mismatch - missing-alerts-1 @ 0s FAIL:
	
		Exepected alerts that were not raised alerts:
		
			Title: alert-2
			Summary: 
			Description: 
			Severity: 0
			Source: 
			Destination: 
			CorrelationID: 
			CustomFields: map[]
		`
	assert.Equal(t, expectedOutput, buf.String())
}

func TestRunAndPrintTestsSucceeds(t *testing.T) {
	testRules := []*rules.Rule{
		{
			Name:  "rule-1",
			Query: "fake-table | limit 1",
		},
	}

	tests := []*Test{
		{
			name: "test-1-no-alerts",
			timeseries: map[string][]Timeseries{
				"fake-table": {},
			},
			expectedAlerts: []ExpectedAlerts{
				{
					Name: "rule-1",
				},
				{
					Name:   "rule-1",
					EvalAt: time.Minute * 30,
					Alerts: []*alert.Alert{
						{
							Title: "alert-1",
						},
					},
				},
			},
		},
	}

	e, err := NewEvaluator(&fakeClient{}, testRules, tests)
	require.NoError(t, err)
	buf := &strings.Builder{}
	err = e.RunAndPrintTests(buf)
	assert.NoError(t, err)
	assert.Equal(t, "test-1-no-alerts: ok\n", buf.String())
}
