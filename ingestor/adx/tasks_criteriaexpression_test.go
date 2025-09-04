package adx

import (
	"context"
	"testing"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// fakeStatementExecutor implements StatementExecutor for tests

type fakeStatementExecutor struct{ db string }

func (f fakeStatementExecutor) Database() string { return f.db }
func (f fakeStatementExecutor) Endpoint() string { return "https://example" }
func (f fakeStatementExecutor) Mgmt(ctx context.Context, stmt kusto.Statement, options ...kusto.MgmtOption) (*kusto.RowIterator, error) {
	return &kusto.RowIterator{}, nil
}

func TestSummaryRuleTask_shouldProcessRule_CriteriaExpression(t *testing.T) {
	clusterLabels := map[string]string{"region": "eastus", "environment": "prod"}
	Task := &SummaryRuleTask{ClusterLabels: clusterLabels, kustoCli: fakeStatementExecutor{db: "db"}}

	cases := []struct {
		name   string
		rule   v1.SummaryRule
		expect bool
	}{
		{
			name:   "no criteria or expression => executes",
			rule:   v1.SummaryRule{Spec: v1.SummaryRuleSpec{Database: "db", Table: "t", Body: "", Interval: metav1.Duration{Duration: time.Minute}}},
			expect: true,
		},
		{
			name:   "criteria match only",
			rule:   v1.SummaryRule{Spec: v1.SummaryRuleSpec{Database: "db", Table: "t", Body: "", Interval: metav1.Duration{Duration: time.Minute}, Criteria: map[string][]string{"region": {"eastus"}}}},
			expect: true,
		},
		{
			name:   "criteriaExpression match only",
			rule:   v1.SummaryRule{Spec: v1.SummaryRuleSpec{Database: "db", Table: "t", Body: "", Interval: metav1.Duration{Duration: time.Minute}, CriteriaExpression: "region == 'eastus' && environment == 'prod'"}},
			expect: true,
		},
		{
			name:   "both match",
			rule:   v1.SummaryRule{Spec: v1.SummaryRuleSpec{Database: "db", Table: "t", Body: "", Interval: metav1.Duration{Duration: time.Minute}, Criteria: map[string][]string{"region": {"eastus"}}, CriteriaExpression: "environment == 'prod'"}},
			expect: true,
		},
		{
			name:   "criteria match expression false",
			rule:   v1.SummaryRule{Spec: v1.SummaryRuleSpec{Database: "db", Table: "t", Body: "", Interval: metav1.Duration{Duration: time.Minute}, Criteria: map[string][]string{"region": {"eastus"}}, CriteriaExpression: "environment == 'dev'"}},
			expect: false,
		},
		{
			name:   "criteria no match expression true",
			rule:   v1.SummaryRule{Spec: v1.SummaryRuleSpec{Database: "db", Table: "t", Body: "", Interval: metav1.Duration{Duration: time.Minute}, Criteria: map[string][]string{"region": {"westus"}}, CriteriaExpression: "environment == 'prod'"}},
			expect: false,
		},
	}

	for _, c := range cases {
		c := c
		// ensure required fields non-empty; already set above
		ok := Task.shouldProcessRule(c.rule)
		require.Equal(t, c.expect, ok, c.name)
	}
}
