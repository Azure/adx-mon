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
	defaultLabels := map[string]string{"region": "eastus", "environment": "prod"}

	cases := []struct {
		name   string
		rule   v1.SummaryRule
		labels map[string]string // optional override of cluster labels
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
		// Casing behavior tests
		{
			name:   "criteria map insensitive - upper label lower criteria key",
			labels: map[string]string{"REGION": "EASTUS", "environment": "prod"},
			rule:   v1.SummaryRule{Spec: v1.SummaryRuleSpec{Database: "db", Table: "t", Interval: metav1.Duration{Duration: time.Minute}, Criteria: map[string][]string{"region": {"eastus"}}}},
			expect: true,
		},
		{
			name:   "criteria map insensitive - mixed case label & criteria key",
			labels: map[string]string{"Region": "EastUS", "environment": "prod"},
			rule:   v1.SummaryRule{Spec: v1.SummaryRuleSpec{Database: "db", Table: "t", Interval: metav1.Duration{Duration: time.Minute}, Criteria: map[string][]string{"REGION": {"EASTUS"}}}},
			expect: true, // map matching ignores case for both key & value
		},
		{
			name:   "criteriaExpression sensitive - mismatched case identifiers",
			labels: map[string]string{"region": "eastus", "environment": "prod"},
			rule:   v1.SummaryRule{Spec: v1.SummaryRuleSpec{Database: "db", Table: "t", Interval: metav1.Duration{Duration: time.Minute}, CriteriaExpression: "REGION == 'eastus'"}},
			expect: false, // REGION undefined
		},
		{
			name:   "criteriaExpression sensitive - matching TitleCase identifier",
			labels: map[string]string{"Region": "eastus", "environment": "prod"},
			rule:   v1.SummaryRule{Spec: v1.SummaryRuleSpec{Database: "db", Table: "t", Interval: metav1.Duration{Duration: time.Minute}, CriteriaExpression: "Region == 'eastus'"}},
			expect: true,
		},
		{
			name:   "criteriaExpression value mismatch case",
			labels: map[string]string{"region": "eastus", "environment": "prod"},
			rule:   v1.SummaryRule{Spec: v1.SummaryRuleSpec{Database: "db", Table: "t", Interval: metav1.Duration{Duration: time.Minute}, CriteriaExpression: "region == 'EASTUS'"}},
			expect: false,
		},
		{
			name:   "criteriaExpression uppercase identifier matches",
			labels: map[string]string{"REGION": "eastus", "environment": "prod"},
			rule:   v1.SummaryRule{Spec: v1.SummaryRuleSpec{Database: "db", Table: "t", Interval: metav1.Duration{Duration: time.Minute}, CriteriaExpression: "REGION == 'eastus'"}},
			expect: true,
		},
		{
			name:   "criteriaExpression uppercase value matches",
			labels: map[string]string{"region": "EASTUS", "environment": "prod"},
			rule:   v1.SummaryRule{Spec: v1.SummaryRuleSpec{Database: "db", Table: "t", Interval: metav1.Duration{Duration: time.Minute}, CriteriaExpression: "region == 'EASTUS'"}},
			expect: true,
		},
		{
			name:   "combined - criteria insensitive expression identifier mismatch",
			labels: map[string]string{"region": "eastus", "environment": "prod"},
			rule:   v1.SummaryRule{Spec: v1.SummaryRuleSpec{Database: "db", Table: "t", Interval: metav1.Duration{Duration: time.Minute}, Criteria: map[string][]string{"region": {"eastus"}}, CriteriaExpression: "REGION == 'eastus'"}},
			expect: false,
		},
		{
			name:   "combined - criteria insensitive mixed key cases expression true",
			labels: map[string]string{"region": "eastus", "environment": "prod"},
			rule:   v1.SummaryRule{Spec: v1.SummaryRuleSpec{Database: "db", Table: "t", Interval: metav1.Duration{Duration: time.Minute}, Criteria: map[string][]string{"Region": {"EASTUS"}}, CriteriaExpression: "environment == 'prod' && region == 'eastus'"}},
			expect: true,
		},
		{
			name:   "expression unknown variable due to different label key case",
			labels: map[string]string{"REGION": "eastus", "environment": "prod"},
			rule:   v1.SummaryRule{Spec: v1.SummaryRuleSpec{Database: "db", Table: "t", Interval: metav1.Duration{Duration: time.Minute}, CriteriaExpression: "region == 'eastus'"}},
			expect: false,
		},
	}

	for _, c := range cases {
		c := c
		labels := defaultLabels
		if c.labels != nil {
			labels = c.labels
		}
		Task := &SummaryRuleTask{ClusterLabels: labels, kustoCli: fakeStatementExecutor{db: "db"}}
		ok := Task.shouldProcessRule(c.rule)
		require.Equal(t, c.expect, ok, c.name)
	}
}
