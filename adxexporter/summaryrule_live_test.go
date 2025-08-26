//go:build integration
// +build integration

package adxexporter

// NOTE: This diagnostic test is intended for manual triage against a live cluster.
// It will attempt to fetch the SummaryRule named "underlay-apiserver-qos" in the
// "adx-mon" namespace and invoke the SummaryRuleReconciler.Reconcile method while
// logging each gating decision (database managed, criteria match, adoption logic,
// submission readiness, etc). It uses a lightweight mock Kusto executor that
// returns a synthetic operation id and immediately reports the async operation
// as Completed so we can observe status mutation paths without contacting a real
// ADX cluster.
//
// Usage:
//   go test -v -tags=integration ./adxexporter -run TestLiveSummaryRuleReconcile
//
// Prerequisites:
// - KUBECONFIG pointing at the target cluster (or in-cluster execution)
// - The SummaryRule CRD installed and the specific SummaryRule existing
// - Network access not required for ADX (calls are mocked)
//
// This file is not meant to be committed long-term; remove after triage.

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	"github.com/Azure/azure-kusto-go/kusto/data/types"
	"github.com/Azure/azure-kusto-go/kusto/data/value"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	kclock "k8s.io/utils/clock/testing"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// diagnosticKustoExecutor is a minimal KustoExecutor that simulates a successful
// async .set-or-append submission followed by a Completed async status.
type diagnosticKustoExecutor struct {
	db       string
	endpoint string
	calls    int
}

func (d *diagnosticKustoExecutor) Database() string { return d.db }
func (d *diagnosticKustoExecutor) Endpoint() string { return d.endpoint }

func (d *diagnosticKustoExecutor) Query(ctx context.Context, stmt kusto.Statement, _ ...kusto.QueryOption) (*kusto.RowIterator, error) {
	return d.emptyIterator(), nil
}

func (d *diagnosticKustoExecutor) Mgmt(ctx context.Context, stmt kusto.Statement, _ ...kusto.MgmtOption) (*kusto.RowIterator, error) {
	d.calls++
	if d.calls == 1 { // submission path -> return single cell op id
		cols := table.Columns{{Name: "OperationId", Type: types.String}}
		rows, _ := kusto.NewMockRows(cols)
		rows.Row(value.Values{value.String{Value: "diag-op-id", Valid: true}})
		return d.iteratorFromRows(rows), nil
	}
	// subsequent (status polling) -> return Completed state row
	cols := table.Columns{
		{Name: "LastUpdatedOn", Type: types.DateTime},
		{Name: "OperationId", Type: types.String},
		{Name: "State", Type: types.String},
		{Name: "ShouldRetry", Type: types.Real},
		{Name: "Status", Type: types.String},
	}
	rows, _ := kusto.NewMockRows(cols)
	rows.Row(value.Values{
		value.DateTime{Value: time.Now().UTC(), Valid: true},
		value.String{Value: "diag-op-id", Valid: true},
		value.String{Value: "Completed", Valid: true},
		value.Real{Value: 0, Valid: true},
		value.String{Value: "", Valid: true},
	})
	return d.iteratorFromRows(rows), nil
}

func (d *diagnosticKustoExecutor) ensureTestFlag() {
	if flag.Lookup("test.v") == nil {
		flag.String("test.v", "", "")
		_ = flag.CommandLine.Set("test.v", "true")
	}
}

func (d *diagnosticKustoExecutor) iteratorFromRows(rows *kusto.MockRows) *kusto.RowIterator {
	d.ensureTestFlag()
	iter := &kusto.RowIterator{}
	if err := iter.Mock(rows); err != nil {
		panic(err)
	}
	return iter
}

func (d *diagnosticKustoExecutor) emptyIterator() *kusto.RowIterator {
	cols := table.Columns{}
	rows, _ := kusto.NewMockRows(cols)
	return d.iteratorFromRows(rows)
}

// getKubeConfig tries in-cluster first, then falls back to KUBECONFIG/default path.
func getKubeConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	// Fallback to local kubeconfig
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, _ := os.UserHomeDir()
		kubeconfig = fmt.Sprintf("%s/.kube/config", home)
	}
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}

func TestLiveSummaryRuleReconcile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live diagnostic test in short mode")
	}

	cfg, err := getKubeConfig()
	if err != nil {
		t.Skipf("no kube config available: %v", err)
	}

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed adding client-go scheme: %v", err)
	}
	if err := adxmonv1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed adding adxmonv1 scheme: %v", err)
	}

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	ctx := context.Background()
	ns := os.Getenv("SR_NAMESPACE")
	if ns == "" {
		ns = "adx-mon"
	}
	name := os.Getenv("SR_NAME")
	if name == "" {
		name = "underlay-apiserver-qos"
	}
	key := client.ObjectKey{Namespace: ns, Name: name}
	var sr adxmonv1.SummaryRule
	if err := c.Get(ctx, key, &sr); err != nil {
		t.Fatalf("failed to fetch SummaryRule %s: %v", key, err)
	}

	t.Logf("Fetched SummaryRule %s/%s", sr.Namespace, sr.Name)
	t.Logf("Annotations: %#v", sr.Annotations)
	t.Logf("Spec: database=%s table=%s interval=%s criteria=%v", sr.Spec.Database, sr.Spec.Table, sr.Spec.Interval.Duration, sr.Spec.Criteria)
	if le := sr.GetLastExecutionTime(); le != nil {
		t.Logf("LastExecutionTime (status): %s", le.UTC().Format(time.RFC3339Nano))
	} else {
		t.Logf("LastExecutionTime: <nil>")
	}
	if cond := sr.GetCondition(); cond != nil {
		t.Logf("Primary Condition: status=%s reason=%s message=%s observedGen=%d", cond.Status, cond.Reason, cond.Message, cond.ObservedGeneration)
	} else {
		t.Logf("Primary Condition: <nil>")
	}
	asyncOps := sr.GetAsyncOperations()
	t.Logf("AsyncOperations count=%d", len(asyncOps))
	for i, op := range asyncOps {
		t.Logf("  [%d] opId=%s window=%s..%s", i, op.OperationId, op.StartTime, op.EndTime)
	}

	// ClusterLabels: attempt to infer from exporter deployment args (best-effort) - optional.
	clusterLabels := map[string]string{}
	if env := os.Getenv("SR_CLUSTER_LABELS"); env != "" {
		// Expect comma-separated key=val pairs
		for _, kv := range splitAndTrim(env, ",") {
			parts := splitAndTrim(kv, "=")
			if len(parts) == 2 {
				clusterLabels[parts[0]] = parts[1]
			}
		}
	}

	t.Logf("ClusterLabels used for criteria evaluation: %#v", clusterLabels)

	// Construct reconciler
	mockExec := &diagnosticKustoExecutor{db: sr.Spec.Database, endpoint: "https://diagnostic"}
	reconciler := &SummaryRuleReconciler{
		Client:         c,
		Scheme:         scheme,
		ClusterLabels:  clusterLabels,
		KustoClusters:  map[string]string{sr.Spec.Database: "https://diagnostic"},
		KustoExecutors: map[string]KustoExecutor{sr.Spec.Database: mockExec},
		Clock:          kclock.NewFakeClock(time.Now().UTC()),
	}

	// Reproduce gating decisions explicitly before calling Reconcile.
	managed := reconciler.isDatabaseManaged(&sr)
	criteriaOk := matchesCriteria(sr.Spec.Criteria, reconciler.ClusterLabels)
	shouldSubmit := sr.ShouldSubmitRule(reconciler.Clock)
	t.Logf("Gating: managed=%v criteriaMatch=%v shouldSubmit=%v", managed, criteriaOk, shouldSubmit)

	// Attempt adoption if desired (side effects patch object) - copy object to avoid mutating sr before logging
	srCopy := sr.DeepCopy()
	adoptResult, adoptHandled := reconciler.adoptIfDesired(ctx, srCopy)
	t.Logf("Adoption probe: handled=%v requeueAfter=%s", adoptHandled, adoptResult.RequeueAfter)

	// Now call actual Reconcile (will re-fetch current state)
	res, recErr := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
	if recErr != nil {
		t.Logf("Reconcile returned error: %v", recErr)
	} else {
		t.Logf("Reconcile succeeded: requeueAfter=%s", res.RequeueAfter)
	}

	// Re-fetch updated object to observe mutations
	var updated adxmonv1.SummaryRule
	if err := c.Get(ctx, key, &updated); err != nil {
		t.Fatalf("failed to refetch SummaryRule: %v", err)
	}
	if cond := updated.GetCondition(); cond != nil {
		t.Logf("Post-Reconcile Condition: status=%s reason=%s message=%s", cond.Status, cond.Reason, cond.Message)
	} else {
		t.Logf("Post-Reconcile Condition: <nil>")
	}
	updatedOps := updated.GetAsyncOperations()
	t.Logf("Post-Reconcile AsyncOperations count=%d", len(updatedOps))
	for i, op := range updatedOps {
		t.Logf("  [%d] opId=%s window=%s..%s", i, op.OperationId, op.StartTime, op.EndTime)
	}
	if let := updated.GetLastExecutionTime(); let != nil {
		t.Logf("Post-Reconcile LastExecutionTime=%s", let.UTC().Format(time.RFC3339Nano))
	}
}

// splitAndTrim is a small helper for parsing env style lists without pulling extra deps.
func splitAndTrim(s, sep string) []string {
	var out []string
	for _, part := range strings.Split(s, sep) {
		p := strings.TrimSpace(part)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
