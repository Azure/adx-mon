package operator

import (
	"testing"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAdvancePhase(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		input    metav1.Condition
		expected metav1.Condition
	}{
		{
			name: "EnsureCrds → EnsureKusto",
			input: metav1.Condition{
				Reason: PhaseEnsureCrds.String(),
			},
			expected: metav1.Condition{
				Type:    adxmonv1.ADXClusterConditionOwner,
				Status:  metav1.ConditionUnknown,
				Reason:  PhaseEnsureKusto.String(),
				Message: "Ensuring Kusto Cluster",
			},
		},
		{
			name: "EnsureKusto → EnsureIngestor",
			input: metav1.Condition{
				Reason: PhaseEnsureKusto.String(),
			},
			expected: metav1.Condition{
				Type:    adxmonv1.IngestorClusterConditionOwner,
				Status:  metav1.ConditionUnknown,
				Reason:  PhaseEnsureIngestor.String(),
				Message: "Ensuring Ingestor StatefulSet",
			},
		},
		{
			name: "EnsureIngestor → EnsureCollector",
			input: metav1.Condition{
				Reason: PhaseEnsureIngestor.String(),
			},
			expected: metav1.Condition{
				Type:    adxmonv1.CollectorClusterConditionOwner,
				Status:  metav1.ConditionUnknown,
				Reason:  PhaseEnsureCollector.String(),
				Message: "Ensuring Collector DaemonSet",
			},
		},
		{
			name: "EnsureCollector → EnsureAlerter",
			input: metav1.Condition{
				Reason: PhaseEnsureCollector.String(),
			},
			expected: metav1.Condition{
				Type:    adxmonv1.AlerterClusterConditionOwner,
				Status:  metav1.ConditionUnknown,
				Reason:  PhaseEnsureAlerter.String(),
				Message: "Ensuring Alerter Deployment",
			},
		},
		{
			name: "EnsureAlerter → Done",
			input: metav1.Condition{
				Reason: PhaseEnsureAlerter.String(),
			},
			expected: metav1.Condition{
				Type:    adxmonv1.OperatorCommandConditionOwner,
				Status:  metav1.ConditionTrue,
				Reason:  PhaseDone.String(),
				Message: "All components are ready",
			},
		},
		{
			name: "Default/Unknown",
			input: metav1.Condition{
				Reason: "something-else",
			},
			expected: metav1.Condition{
				Type:    adxmonv1.OperatorCommandConditionOwner,
				Status:  metav1.ConditionUnknown,
				Reason:  PhaseEnsureCrds.String(),
				Message: "Starting reconciliation",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := advancePhase(tc.input)
			if got.Type != tc.expected.Type || got.Status != tc.expected.Status || got.Reason != tc.expected.Reason || got.Message != tc.expected.Message {
				t.Errorf("advance() = %+v, want %+v", got, tc.expected)
			}
		})
	}
}

func TestCurrentPhase(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		conds    []metav1.Condition
		expected OperatorPhase
	}{
		{
			name:     "no conditions returns PhaseEnsureCrds",
			conds:    nil,
			expected: PhaseEnsureCrds,
		},
		{
			name: "EnsureCrds incomplete",
			conds: []metav1.Condition{{
				Type:   "crds.adx-mon.azure.com",
				Status: metav1.ConditionUnknown,
				Reason: PhaseEnsureCrds.String(),
			}},
			expected: PhaseEnsureCrds,
		},
		{
			name: "EnsureCrds complete, EnsureKusto incomplete",
			conds: []metav1.Condition{{
				Type:   "crds.adx-mon.azure.com",
				Status: metav1.ConditionTrue,
				Reason: PhaseEnsureCrds.String(),
			}, {
				Type:   "adxcluster.adx-mon.azure.com",
				Status: metav1.ConditionUnknown,
				Reason: PhaseEnsureKusto.String(),
			}},
			expected: PhaseEnsureKusto,
		},
		{
			name: "EnsureKusto complete, EnsureIngestor incomplete",
			conds: []metav1.Condition{{
				Type:   "adxcluster.adx-mon.azure.com",
				Status: metav1.ConditionTrue,
				Reason: PhaseEnsureKusto.String(),
			}, {
				Type:   "ingestorcluster.adx-mon.azure.com",
				Status: metav1.ConditionUnknown,
				Reason: PhaseEnsureIngestor.String(),
			}},
			expected: PhaseEnsureIngestor,
		},
		{
			name: "all phases complete returns PhaseDone",
			conds: []metav1.Condition{{
				Type:   "crds.adx-mon.azure.com",
				Status: metav1.ConditionTrue,
				Reason: PhaseEnsureCrds.String(),
			}, {
				Type:   "adxcluster.adx-mon.azure.com",
				Status: metav1.ConditionTrue,
				Reason: PhaseEnsureKusto.String(),
			}, {
				Type:   "ingestorcluster.adx-mon.azure.com",
				Status: metav1.ConditionTrue,
				Reason: PhaseEnsureIngestor.String(),
			}, {
				Type:   "collectorcluster.adx-mon.azure.com",
				Status: metav1.ConditionTrue,
				Reason: PhaseEnsureCollector.String(),
			}, {
				Type:   "alertercluster.adx-mon.azure.com",
				Status: metav1.ConditionTrue,
				Reason: PhaseEnsureAlerter.String(),
			}, {
				Type:   "operator.adx-mon.azure.com",
				Status: metav1.ConditionTrue,
				Reason: PhaseDone.String(),
			}},
			expected: PhaseDone,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			op := &adxmonv1.Operator{
				Status: adxmonv1.OperatorStatus{
					Conditions: tc.conds,
				},
			}
			phase := currentPhase(op)
			if phase != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, phase)
			}
		})
	}
}

func TestClustersAreDone(t *testing.T) {
	t.Parallel()
	gen := int64(5)
	testCases := []struct {
		name     string
		operator *adxmonv1.Operator
		expect   bool
	}{
		{
			name:     "no conditions returns false",
			operator: &adxmonv1.Operator{},
			expect:   false,
		},
		{
			name: "condition present but not true",
			operator: &adxmonv1.Operator{
				ObjectMeta: metav1.ObjectMeta{Generation: gen},
				Status: adxmonv1.OperatorStatus{
					Conditions: []metav1.Condition{{
						Type:               adxmonv1.ADXClusterConditionOwner,
						Status:             metav1.ConditionFalse,
						Reason:             "test",
						ObservedGeneration: gen,
					}},
				},
			},
			expect: false,
		},
		{
			name: "condition true and observed generation matches",
			operator: &adxmonv1.Operator{
				ObjectMeta: metav1.ObjectMeta{Generation: gen},
				Status: adxmonv1.OperatorStatus{
					Conditions: []metav1.Condition{{
						Type:               adxmonv1.ADXClusterConditionOwner,
						Status:             metav1.ConditionTrue,
						Reason:             "test",
						ObservedGeneration: gen,
					}},
				},
			},
			expect: true,
		},
		{
			name: "all clusters in spec have endpoints",
			operator: &adxmonv1.Operator{
				Spec: adxmonv1.OperatorSpec{
					ADX: &adxmonv1.ADXConfig{
						Clusters: []adxmonv1.ADXClusterSpec{{
							Name:     "c1",
							Endpoint: "https://foo",
						}},
					},
				},
			},
			expect: true,
		},
		{
			name: "cluster in spec missing endpoint returns false",
			operator: &adxmonv1.Operator{
				Spec: adxmonv1.OperatorSpec{
					ADX: &adxmonv1.ADXConfig{
						Clusters: []adxmonv1.ADXClusterSpec{{
							Name:     "c1",
							Endpoint: "",
						}},
					},
				},
			},
			expect: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := clustersAreDone(tc.operator)
			if got != tc.expect {
				t.Errorf("clustersAreDone() = %v, want %v", got, tc.expect)
			}
		})
	}
}
