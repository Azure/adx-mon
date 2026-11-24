package rules

import (
	"context"
	"fmt"
	"testing"
	"time"

	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestStore_ReloadRulesSkipsInvalidInterval(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, alertrulev1.AddToScheme(scheme))

	newRule := func(name string, interval time.Duration) *alertrulev1.AlertRule {
		return &alertrulev1.AlertRule{
			Namespace: "foo", Name: name,
			Spec: alertrulev1.AlertRuleSpec{
				Database:    "SomeDB",
				Interval:    metav1.Duration{Duration: interval},
				Query:       "BadThings",
				Destination: "peoplewhocare",
			},
		}
	}
	ctrlCli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			newRule("valid", time.Minute),
			newRule("zero", 0),
			newRule("below-minimum", 10*time.Second),
		).
		Build()

	store := NewStore(StoreOpts{Region: "eastus", CtrlCli: ctrlCli})
	store.ctx = context.Background()

	rules, err := store.reloadRules()
	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.Equal(t, "valid", rules[0].Name)
	require.Equal(t, time.Minute, rules[0].Interval)
}

func TestStore_ReloadRulesDestinationsAndConflictIsolation(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, alertrulev1.AddToScheme(scheme))

	tooMany := make([]string, alertrulev1.MaxAlertRuleDestinations+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("dest-%d", i)
	}

	ctrlCli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			&alertrulev1.AlertRule{
				ObjectMeta: metav1.ObjectMeta{Namespace: "monitoring", Name: "legacy-single", UID: types.UID("uid-1"), Generation: 3},
				Spec: alertrulev1.AlertRuleSpec{
					Database:    "Metrics",
					Interval:    metav1.Duration{Duration: time.Minute},
					Query:       "Cpu | take 1",
					Destination: "icm://legacy",
				},
			},
			&alertrulev1.AlertRule{
				ObjectMeta: metav1.ObjectMeta{Namespace: "monitoring", Name: "explicit-multi", UID: types.UID("uid-2"), Generation: 7},
				Spec: alertrulev1.AlertRuleSpec{
					Database:     "Metrics",
					Interval:     metav1.Duration{Duration: time.Minute},
					Query:        "Cpu | take 1",
					Destinations: []string{"pagerduty://b", "icm://a"},
				},
			},
			&alertrulev1.AlertRule{
				ObjectMeta: metav1.ObjectMeta{Namespace: "monitoring", Name: "bad-conflict"},
				Spec: alertrulev1.AlertRuleSpec{
					Database:     "Metrics",
					Interval:     metav1.Duration{Duration: time.Minute},
					Query:        "Cpu | take 1",
					Destination:  "icm://legacy",
					Destinations: []string{"pagerduty://b"},
				},
			},
			&alertrulev1.AlertRule{
				ObjectMeta: metav1.ObjectMeta{Namespace: "monitoring", Name: "bad-dup"},
				Spec: alertrulev1.AlertRuleSpec{
					Database:     "Metrics",
					Interval:     metav1.Duration{Duration: time.Minute},
					Query:        "Cpu | take 1",
					Destinations: []string{"icm://a", "icm://a"},
				},
			},
			&alertrulev1.AlertRule{
				ObjectMeta: metav1.ObjectMeta{Namespace: "monitoring", Name: "bad-empty-item"},
				Spec: alertrulev1.AlertRuleSpec{
					Database:     "Metrics",
					Interval:     metav1.Duration{Duration: time.Minute},
					Query:        "Cpu | take 1",
					Destinations: []string{"icm://a", ""},
				},
			},
			&alertrulev1.AlertRule{
				ObjectMeta: metav1.ObjectMeta{Namespace: "monitoring", Name: "bad-too-many"},
				Spec: alertrulev1.AlertRuleSpec{
					Database:     "Metrics",
					Interval:     metav1.Duration{Duration: time.Minute},
					Query:        "Cpu | take 1",
					Destinations: tooMany,
				},
			},
		).
		Build()

	store := NewStore(StoreOpts{Region: "eastus", CtrlCli: ctrlCli})
	store.ctx = context.Background()

	loaded, err := store.reloadRules()
	require.NoError(t, err)
	require.Len(t, loaded, 2)

	byName := make(map[string]*Rule, len(loaded))
	for _, r := range loaded {
		byName[r.Name] = r
	}

	single := byName["legacy-single"]
	require.NotNil(t, single)
	require.Equal(t, "uid-1", single.UID)
	require.Equal(t, int64(3), single.Generation)
	require.False(t, single.HasExplicitDestinations)
	require.False(t, single.ExplicitDestinations())
	require.Equal(t, "icm://legacy", single.Destination)
	require.Nil(t, single.Destinations)
	require.Equal(t, []string{"icm://legacy"}, single.CandidateDestinations())

	multi := byName["explicit-multi"]
	require.NotNil(t, multi)
	require.Equal(t, "uid-2", multi.UID)
	require.Equal(t, int64(7), multi.Generation)
	require.True(t, multi.HasExplicitDestinations)
	require.True(t, multi.ExplicitDestinations())
	require.Equal(t, "", multi.Destination)
	require.Equal(t, []string{"pagerduty://b", "icm://a"}, multi.Destinations)
	require.Equal(t, []string{"pagerduty://b", "icm://a"}, multi.CandidateDestinations())
	primary, explicit := multi.ResolvedMetaDestinations()
	require.Equal(t, "pagerduty://b", primary)
	require.Equal(t, []string{"pagerduty://b", "icm://a"}, explicit)
}
