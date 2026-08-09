package rules

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	alertrulev1 "github.com/Azure/adx-mon/api/v1"
)

func newAlertRule(name, namespace, database string) *alertrulev1.AlertRule {
	return &alertrulev1.AlertRule{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "adx-mon.azure.com/v1",
			Kind:       "AlertRule",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: alertrulev1.AlertRuleSpec{
			Database:    database,
			Interval:    metav1.Duration{Duration: time.Minute},
			Query:       "TestTable | count",
			Destination: "test-dest",
		},
	}
}

func TestStore_ReloadRules_AllNamespaces(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, alertrulev1.AddToScheme(scheme))

	rule1 := newAlertRule("rule-a", "ns-one", "DB1")
	rule2 := newAlertRule("rule-b", "ns-two", "DB2")

	cli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(rule1, rule2).
		Build()

	store := NewStore(StoreOpts{
		Region:  "eastus",
		CtrlCli: cli,
	})

	ctx := context.Background()
	require.NoError(t, store.Open(ctx))
	defer store.Close()

	rules := store.Rules()
	require.Len(t, rules, 2)
}

func TestStore_ReloadRules_FilteredNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, alertrulev1.AddToScheme(scheme))

	rule1 := newAlertRule("rule-a", "ns-one", "DB1")
	rule2 := newAlertRule("rule-b", "ns-two", "DB2")
	rule3 := newAlertRule("rule-c", "ns-one", "DB3")

	cli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(rule1, rule2, rule3).
		Build()

	store := NewStore(StoreOpts{
		Region:    "eastus",
		Namespace: "ns-one",
		CtrlCli:   cli,
	})

	ctx := context.Background()
	require.NoError(t, store.Open(ctx))
	defer store.Close()

	rules := store.Rules()
	require.Len(t, rules, 2)
	for _, r := range rules {
		require.Equal(t, "ns-one", r.Namespace)
	}
}

func TestStore_ReloadRules_FilteredNamespace_NoMatches(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, alertrulev1.AddToScheme(scheme))

	rule1 := newAlertRule("rule-a", "ns-one", "DB1")

	cli := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(rule1).
		Build()

	store := NewStore(StoreOpts{
		Region:    "eastus",
		Namespace: "ns-nonexistent",
		CtrlCli:   cli,
	})

	ctx := context.Background()
	require.NoError(t, store.Open(ctx))
	defer store.Close()

	rules := store.Rules()
	require.Empty(t, rules)
}
