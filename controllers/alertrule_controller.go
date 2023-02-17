/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/Azure/adx-mon/alert"
	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/queue"
	"github.com/Azure/adx-mon/rules"
)

type KustoClient interface {
	Query(ctx context.Context, db string, query kusto.Stmt, options ...kusto.QueryOption) (*kusto.RowIterator, error)
	Endpoint() string
}

// AlertRuleReconciler reconciles a AlertRule object
type AlertRuleReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	Region    string
	AlertCli  *alert.Client
	AlertAddr string

	KustoClient Client

	KustoClients map[string]KustoClient
}

// Results contains a map of the queries being run in the background.
// The key is the namespace/name of the AlertRule object. The value is a bool if the query
// is currently running.
// @todo switch to queue package
var Results = map[string]queue.Result{}

// mutex is used to synchronize access to the Results map.
var mutex = &sync.RWMutex{}

//+kubebuilder:rbac:groups=adx-mon.azure.com,resources=alertrules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=adx-mon.azure.com,resources=alertrules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=adx-mon.azure.com,resources=alertrules/finalizers,verbs=update
//+kubebuilder:rbac:groups=adx-mon.azure.com,resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the AlertRule object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// Reconcile performs a full reconciliation for the object referred to by the Request.
// The Controller will requeue the Request to be processed again if an error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *AlertRuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling AlertRule")

	alertRule := &adxmonv1.AlertRule{}
	if err := r.Get(ctx, req.NamespacedName, alertRule); err != nil {
		if client.IgnoreNotFound(err) == nil {
			logger.Info("AlertRule deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// if the alertRule's observed generation is different than the status' generation,
	// then the alertRule has been updated and we need reset the conditions
	if alertRule.Status.ObservedGeneration != alertRule.Generation {
		logger.Info(fmt.Sprintf("AlertRule updated. ObservedGeneration: %d, Generation: %d", alertRule.Status.ObservedGeneration, alertRule.Generation))
		alertRule.Status.Conditions = nil
		alertRule.Status.ObservedGeneration = alertRule.Generation

		// update the status
		if err := r.Status().Update(ctx, alertRule); err != nil {
			return ctrl.Result{}, err
		}

		// requeue the alertRule
		return ctrl.Result{Requeue: true}, nil
	}

	// if the conditions are not initialized (or malformed), initialize them
	if alertRule.Status.Conditions == nil || len(alertRule.Status.Conditions) != 2 {
		logger.Info("Initializing conditions for AlertRule")
		alertRule.Status.Conditions = []metav1.Condition{
			{
				Type:               adxmonv1.AlertRuleConditionQueryParsed,
				Status:             metav1.ConditionUnknown,
				Reason:             "QueryNotParsed",
				Message:            "Query has not been parsed yet",
				LastTransitionTime: metav1.Now(),
			},
			{
				Type:               adxmonv1.AlertRuleConditionQueryRunning,
				Status:             metav1.ConditionUnknown,
				Reason:             "QueryNotRunning",
				Message:            "Query has not been started yet",
				LastTransitionTime: metav1.Now(),
			},
		}
	}

	// if the query has not been parsed yet, parse it
	// and update the status of the rule, then requeue
	if alertRule.Status.Conditions[0].Status != metav1.ConditionTrue {
		logger.Info("Parsing query for AlertRule")
		_, err := rules.VerifyRule(alertRule, r.Region)
		if err != nil {
			alertRule.Status.Conditions[0] = metav1.Condition{
				Type:               adxmonv1.AlertRuleConditionQueryParsed,
				Status:             metav1.ConditionFalse,
				Reason:             "QueryParseError",
				Message:            err.Error(),
				LastTransitionTime: metav1.Now(),
			}

			if err := r.Status().Update(ctx, alertRule); err != nil {
				return ctrl.Result{}, err
			}

			// if the query is not valid, we don't need to requeue
			logger.Info("Not requeuing after parsing query for AlertRule")
			return ctrl.Result{}, nil
		}

		r.Recorder.Event(alertRule, corev1.EventTypeNormal, "QueryParsed", "Query has been parsed successfully")

		alertRule.Status.Conditions[0] = metav1.Condition{
			Type:               adxmonv1.AlertRuleConditionQueryParsed,
			Status:             metav1.ConditionTrue,
			Reason:             "QueryParsed",
			Message:            "Query has been parsed successfully",
			LastTransitionTime: metav1.Now(),
		}

		if err := r.Status().Update(ctx, alertRule); err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("Requeuing after parsing query for AlertRule")
		return ctrl.Result{Requeue: true}, nil
	}

	// if the query is supposed to be running, but it is not in the results map,
	// it means this process is not actually running the query.
	// set the status of the alert rule to not running, and requeue
	if alertRule.Status.Conditions[1].Status == metav1.ConditionTrue {
		mutex.RLock()
		_, ok := Results[alertRule.Namespace+"/"+alertRule.Name]
		mutex.RUnlock()
		if !ok {
			logger.Info("Query result object not found in results map, setting query to not running for AlertRule")
			alertRule.Status.Conditions[1] = metav1.Condition{
				Type:               adxmonv1.AlertRuleConditionQueryRunning,
				Status:             metav1.ConditionFalse,
				Reason:             "QueryNotRunning",
				Message:            "Query is not running",
				LastTransitionTime: metav1.Now(),
			}

			if err := r.Status().Update(ctx, alertRule); err != nil {
				return ctrl.Result{}, err
			}
			logger.Info("Requeuing after setting query to not running for AlertRule")
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// if the query is supposed to be running, but the last transition time
	// of the query is larger than the interval, we need to re-run the query.
	// set the status of the alert rule to not running, and requeue.
	// @TODO use some sort of timeout instead of the interval
	if alertRule.Status.Conditions[1].Status == metav1.ConditionTrue {
		if alertRule.Status.Conditions[1].LastTransitionTime.Time.Add(alertRule.Spec.Interval.Duration).Before(time.Now()) {
			logger.Info("Query supposed to be running, but last transition time is larger than interval, setting query to not running for AlertRule")
			alertRule.Status.Conditions[1] = metav1.Condition{
				Type:               adxmonv1.AlertRuleConditionQueryRunning,
				Status:             metav1.ConditionFalse,
				Reason:             "QueryNotRunning",
				Message:            "Query is not running",
				LastTransitionTime: metav1.Now(),
			}

			if err := r.Status().Update(ctx, alertRule); err != nil {
				return ctrl.Result{}, err
			}
			logger.Info("Requeuing after setting query to not running for AlertRule")
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// if it's been less than the interval since the last transition time,
	// requeue until the correct time.
	if alertRule.Status.Conditions[1].Status == metav1.ConditionFalse && alertRule.Status.Conditions[1].LastTransitionTime.Time.Add(alertRule.Spec.Interval.Duration).After(time.Now()) {
		requeueAfter := time.Until(alertRule.Status.Conditions[1].LastTransitionTime.Time.Add(alertRule.Spec.Interval.Duration))

		logger.Info(fmt.Sprintf("Query has run recently, requeuing after %v", requeueAfter))
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	// if the query is not running yet,
	// try to acquire a worker slot.
	// if a slot is available, in a go-routine, run the query,
	// then update the status of the rule, and requeue.
	// if a slot is not available, requeue
	if alertRule.Status.Conditions[1].Status != metav1.ConditionTrue {
		logger.Info("Query is not running, checking for worker slot")

		// Determine if there is a worker slot available
		mutex.Lock()
		count := len(Results)
		if count >= 4 {
			mutex.Unlock()
			logger.Info("No worker slot available, requeuing")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		Results[alertRule.Namespace+"/"+alertRule.Name] = queue.Result{}
		mutex.Unlock()
		logger.Info("Occupied worker slot to run query")

		// Run the query in a go-routine to not block the reconcile loop.
		// As soon as the query is queued, update the status of the rule and requeue.
		go func() {
			start := time.Now()
			logger.Info("Executing query")
			alertcount := 0
			//time to interface this function?
			counticms := func(endpoint string, rule adxmonv1.AlertRule, row *table.Row) error {
				alertcount += 1
				return r.ICMHandler(endpoint, rule, row)
			}

			if err := r.KustoClient.Query(ctx, *alertRule, counticms); err != nil {
				logger.Error(err, "Failed to execute query")
				mutex.Lock()
				Results[alertRule.Namespace+"/"+alertRule.Name] = queue.Result{
					Error:     err,
					Timestamp: start,
				}
				mutex.Unlock()

				r.Recorder.Eventf(alertRule, corev1.EventTypeWarning, "QueryFailed", "Query failed to execute with error: %v", err)

				return
			}
			logger.Info(fmt.Sprintf("Completed query in %s", time.Since(start)))

			mutex.Lock()
			Results[alertRule.Namespace+"/"+alertRule.Name] = queue.Result{
				Error:     nil,
				Timestamp: start,
			}
			mutex.Unlock()

			r.Recorder.Eventf(alertRule, corev1.EventTypeNormal, "QueryCompleted", "Query completed successfully, alerts: %d", alertcount)

		}()

		alertRule.Status.Conditions[1] = metav1.Condition{
			Type:               adxmonv1.AlertRuleConditionQueryRunning,
			Status:             metav1.ConditionTrue,
			Reason:             "QueryRunning",
			Message:            "Query is running",
			LastTransitionTime: metav1.Now(),
		}

		if err := r.Status().Update(ctx, alertRule); err != nil {
			return ctrl.Result{}, err
		}
		logger.Info("Query running, will check back for results")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// If the query is running, see if the query has returned results.
	if alertRule.Status.Conditions[1].Status == metav1.ConditionTrue {
		mutex.RLock()
		res, ok := Results[alertRule.Namespace+"/"+alertRule.Name]
		mutex.RUnlock()

		// if the result is not found in the map, reset the status of the rule and requeue
		if !ok {
			logger.Info("Query was running, but result not found in map, resetting query status for AlertRule")
			alertRule.Status.Conditions[1] = metav1.Condition{
				Type:               adxmonv1.AlertRuleConditionQueryRunning,
				Status:             metav1.ConditionFalse,
				Reason:             "QueryNotRunning",
				Message:            "Query is not running",
				LastTransitionTime: metav1.Now(),
			}

			if err := r.Status().Update(ctx, alertRule); err != nil {
				return ctrl.Result{}, err
			}
			logger.Info("Requeuing after resetting query status for AlertRule")
			return ctrl.Result{Requeue: true}, nil
		}

		// if the query has not returned results, requeue
		if res.Timestamp.IsZero() {
			logger.Info("Query has not returned results, requeuing")
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		// figure out when the next query should run.
		// alertRule.Interval - (time.Now() - alertRule.LastQueryTime)
		// if the next query should run in the past, requeue immediately
		// if the next query should run in the future, requeue after the appropriate amount of time
		alertRule.Status.LastQueryTime.Time = res.Timestamp
		nextQueryTime := alertRule.Status.LastQueryTime.Add(alertRule.Spec.Interval.Duration)

		// if the query has returned results, update the status of the rule and requeue
		alertRule.Status.Conditions[1] = metav1.Condition{
			Type:               adxmonv1.AlertRuleConditionQueryRunning,
			Status:             metav1.ConditionFalse,
			Reason:             "QueryFinished",
			Message:            fmt.Sprintf("Query finished. Next query should run at %s", nextQueryTime),
			LastTransitionTime: metav1.Now(),
		}

		if res.Error != nil {
			alertRule.Status.Conditions[1].Reason = "QueryFailed"
			alertRule.Status.Conditions[1].Message = fmt.Sprintf("Query failed: %s", res.Error)
		}

		if err := r.Status().Update(ctx, alertRule); err != nil {
			return ctrl.Result{}, err
		}

		// Delete the query result from the map
		mutex.Lock()
		delete(Results, alertRule.Namespace+"/"+alertRule.Name)
		mutex.Unlock()

		if time.Now().After(nextQueryTime) {
			logger.Info("Query finished. Next query should run in the past, requeuing immediately")
			return ctrl.Result{Requeue: true}, nil
		}
		logger.Info(fmt.Sprintf("Query finished. Next query should run in the future, requeuing after %s", time.Until(nextQueryTime)))
		return ctrl.Result{RequeueAfter: time.Until(nextQueryTime)}, nil
	}

	logger.Info("Finished reconciling AlertRule")

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AlertRuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// The GenerationChangedPredicate will only trigger a reconcile if the
	// generation of the AlertRule changes. This prevents the controller from
	// reconciling when the status of the AlertRule changes.
	pred := predicate.GenerationChangedPredicate{}
	return ctrl.NewControllerManagedBy(mgr).
		For(&adxmonv1.AlertRule{}).
		WithEventFilter(pred).
		Complete(r)
}

func (r *AlertRuleReconciler) Query(ctx context.Context, rule adxmonv1.AlertRule, fn func(string, adxmonv1.AlertRule, *table.Row) error) error {
	client := r.KustoClients[rule.Spec.Database]
	if client == nil {
		return fmt.Errorf("no client found for database=%s", rule.Spec.Database)
	}

	// verify the query and convert it to a Kusto query
	// @todo ideally we wouldnt need to redo this here, but
	// but we need the kusto stmt.
	converted, err := rules.VerifyRule(&rule, r.Region)
	if err != nil {
		return fmt.Errorf("failed to verify rule=%s/%s: %s", rule.Namespace, rule.Name, err)
	}

	iter, err := client.Query(ctx, rule.Spec.Database, converted.Stmt, kusto.ResultsProgressiveDisable())
	if err != nil {
		return fmt.Errorf("failed to execute kusto query=%s/%s: %s", rule.Namespace, rule.Name, err)
	}
	defer iter.Stop()
	return iter.Do(func(row *table.Row) error {
		return fn(client.Endpoint(), rule, row)
	})
}
