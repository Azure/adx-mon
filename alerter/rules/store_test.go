package rules

import (
	"context"
	"testing"
	"time"

	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestStore_ReloadRulesSkipsInvalidInterval(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, alertrulev1.AddToScheme(scheme))

	newRule := func(name string, interval time.Duration) *alertrulev1.AlertRule {
		return &alertrulev1.AlertRule{
			ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: name},
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
