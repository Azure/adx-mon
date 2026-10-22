package rules

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// helper to build a minimal rule
func newRule(name string) *Rule {
	return &Rule{Namespace: "ns", Name: name, Interval: time.Minute}
}

func TestRuleMatches_CriteriaOnly(t *testing.T) {
	r := newRule("crit-only")
	r.Criteria = map[string][]string{"region": {"eastus", "westus"}}
	ok, err := r.Matches(map[string]string{"region": "eastus"})
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = r.Matches(map[string]string{"region": "centralus"})
	require.NoError(t, err)
	require.False(t, ok)

	// case insensitivity
	ok, err = r.Matches(map[string]string{"REGION": "EASTUS"})
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = r.Matches(map[string]string{"region": "EaStUs"})
	require.NoError(t, err)
	require.True(t, ok)
}

func TestRuleMatches_SingleCriteriaOnly(t *testing.T) {
	// Criteria only requires a single matching value
	r := newRule("crit-only")
	r.Criteria = map[string][]string{"region": {"eastus"}, "cloud": {"public"}}
	// match region: eastus
	ok, err := r.Matches(map[string]string{"region": "eastus", "cloud": "other"})
	require.NoError(t, err)
	require.True(t, ok)

	// match cloud: public
	ok, err = r.Matches(map[string]string{"region": "westus", "cloud": "public"})
	require.NoError(t, err)
	require.True(t, ok)

	// no matches
	ok, err = r.Matches(map[string]string{"region": "centralus"})
	require.NoError(t, err)
	require.False(t, ok)
}

func TestRuleMatches_ExpressionOnly(t *testing.T) {
	tests := []struct {
		name        string
		expr        string
		labels      map[string]string
		expectMatch bool
		wantErr     bool
	}{
		{
			name:        "match-capitalized-var",
			expr:        "cloud == 'Public' && region == 'eastus'",
			labels:      map[string]string{"cloud": "Public", "region": "eastus"},
			expectMatch: true,
		},
		{
			name:        "no-match-region-capitialized",
			expr:        "cloud == 'Public' && region == 'Eastus'", // not eastus
			labels:      map[string]string{"cloud": "Public", "region": "eastus"},
			expectMatch: false,
		},
		{
			name:        "no-match-lowercase-keys-and-vars",
			expr:        "cloud == 'Public' && region == 'eastus'",
			labels:      map[string]string{"cloud": "Public", "region": "westus"},
			expectMatch: false,
		},
		{
			name:    "expr-uppercase-vars-lowercase-label-keys-unknown-identifiers",
			expr:    "CLOUD == 'Public' && REGION == 'eastus'",
			labels:  map[string]string{"cloud": "Public", "region": "eastus"},
			wantErr: true, // variable names won't match label keys (case sensitive)
		},
		{
			name:        "expr-uppercase-vars-uppercase-label-keys-match",
			expr:        "CLOUD == 'Public' && REGION == 'eastus'",
			labels:      map[string]string{"CLOUD": "Public", "REGION": "eastus"},
			expectMatch: true,
		},
		{
			name:    "expr-mixedcase-vars-lowercase-keys-unknown-identifiers",
			expr:    "Cloud == 'Public' && Region == 'eastus'", // not cloud/region
			labels:  map[string]string{"cloud": "Public", "region": "eastus"},
			wantErr: true,
		},
		{
			name:        "expr-mixedcase-vars-matching-mixedcase-keys",
			expr:        "Cloud == 'Public' && Region == 'eastus'",
			labels:      map[string]string{"Cloud": "Public", "Region": "eastus"},
			expectMatch: true,
		},
	}

	for _, tc := range tests {
		cc := tc
		t.Run(cc.name, func(t *testing.T) {
			r := newRule("expr-only")
			r.CriteriaExpression = cc.expr
			ok, err := r.Matches(cc.labels)
			if cc.wantErr {
				require.Error(t, err, "expected error for case %s", cc.name)
				return
			}
			require.NoError(t, err)
			require.Equal(t, cc.expectMatch, ok)
		})
	}
}

func TestRuleMatches_ExpressionUnknownIdentifierError(t *testing.T) {
	r := newRule("expr-unknown")
	r.CriteriaExpression = "region == 'eastus' && missingKey == 'x'"
	_, err := r.Matches(map[string]string{"region": "eastus"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "missingKey")
}

func TestRuleMatches_CriteriaAndExpression(t *testing.T) {
	r := newRule("combined")
	r.Criteria = map[string][]string{"region": {"westus"}}
	r.CriteriaExpression = "cloud in ['public','other']"
	// both match
	ok, err := r.Matches(map[string]string{"region": "westus", "cloud": "public"})
	require.NoError(t, err)
	require.True(t, ok)
	// neither matches
	ok, err = r.Matches(map[string]string{"region": "centralus", "cloud": "othertwo"})
	require.NoError(t, err)
	require.False(t, ok)
	// criteria map match, not criteriaExpression
	ok, err = r.Matches(map[string]string{"region": "westus", "cloud": "othertwo"})
	require.NoError(t, err)
	require.False(t, ok)
	// criteriaExpression match, no criteria match
	ok, err = r.Matches(map[string]string{"region": "eastus", "cloud": "public"})
	require.NoError(t, err)
	require.False(t, ok)
}

func TestRuleMatches_FromWorkerExamples_MapCriteria(t *testing.T) {
	r := newRule("mapcrit")
	r.Criteria = map[string][]string{"region": {"eastus"}}
	ok, err := r.Matches(map[string]string{"region": "eastus", "env": "prod"})
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = r.Matches(map[string]string{"region": "westus"})
	require.NoError(t, err)
	require.False(t, ok)
}

func TestRuleMatches_FromWorkerExamples_MultipleValues(t *testing.T) {
	r := newRule("multiv")
	r.Criteria = map[string][]string{"region": {"eastus", "westus"}}
	ok, err := r.Matches(map[string]string{"region": "eastus"})
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = r.Matches(map[string]string{"region": "centralus"})
	require.NoError(t, err)
	require.False(t, ok)
}

func TestRuleMatches_List_Expression(t *testing.T) {
	r := newRule("exprmatch")
	r.CriteriaExpression = "cloud in ['public', 'other'] && region == 'eastus'"
	ok, err := r.Matches(map[string]string{"cloud": "public", "region": "eastus"})
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = r.Matches(map[string]string{"cloud": "other", "region": "eastus"})
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = r.Matches(map[string]string{"cloud": "public", "region": "westus"})
	require.NoError(t, err)
	require.False(t, ok)
}

func TestRuleMatches_BadExpression(t *testing.T) {
	r := newRule("bad-expr")
	r.CriteriaExpression = "region == 'eastus' &&" // syntax error
	_, err := r.Matches(map[string]string{"region": "eastus"})
	require.Error(t, err)
}

func TestRuleMatches_NoCriteria(t *testing.T) {
	r := newRule("no-criteria")
	ok, err := r.Matches(map[string]string{"region": "eastus"})
	require.NoError(t, err)
	// No criteria nor criteriaExpression, so always matches
	require.True(t, ok)
}
