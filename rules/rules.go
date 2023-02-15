package rules

import (
	"fmt"
	"time"

	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/azure-kusto-go/kusto"
	kustotypes "github.com/Azure/azure-kusto-go/kusto/data/types"
	"github.com/Azure/azure-kusto-go/kusto/unsafe"
	"github.com/prometheus/client_golang/prometheus"
	// //nolint:godot // comment does not end with a sentence // temporarily disabling code
)

func VerifyRule(alertRule *alertrulev1.AlertRule, region string) (*Rule, error) {
	rule := &Rule{
		Database:          alertRule.Spec.Database,
		Namespace:         alertRule.Namespace,
		Name:              alertRule.Name,
		Interval:          alertRule.Spec.Interval.Duration,
		Query:             alertRule.Spec.Query,
		RoutingID:         alertRule.Spec.RoutingID,
		TSG:               alertRule.Spec.TSG,
		AutoMitigateAfter: alertRule.Spec.AutoMitigateAfter.Duration,
	}

	rule.Stmt = kusto.NewStmt(``, kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).
		UnsafeAdd(alertRule.Spec.Query).
		MustDefinitions(
			kusto.NewDefinitions().Must(
				kusto.ParamTypes{
					"ParamRegion": kusto.ParamType{Type: kustotypes.String},
				},
			),
		)

	qv := kusto.QueryValues{}
	qv["ParamRegion"] = region
	params, err := kusto.NewParameters().With(qv)
	if err != nil {
		return nil, fmt.Errorf("configuration %s/%s does not have the required region parameter: %w", alertRule.Namespace, alertRule.Name, err)
	}

	stm, err := rule.Stmt.WithParameters(params)
	if err != nil {
		return nil, fmt.Errorf("configuration %s/%s does not contain a region configuration: %w", alertRule.Namespace, alertRule.Name, err)
	}

	rule.Stmt = stm
	rule.Parameters = qv

	return rule, nil
}

// Rule is analogous to a kusto-to-metric configuration, containing
// definitions and using the parlance found in the k2m UI.
type Rule struct {
	Namespace         string
	Name              string
	Database          string
	Interval          time.Duration
	Query             string
	RoutingID         string
	TSG               string
	AutoMitigateAfter time.Duration

	// @todo remove?
	Dimensions []string
	Value      string

	// Stmt specifies the underlayEtcdPeersQuery to execute.
	Stmt kusto.Stmt

	// Parameters are the parameters passed with the underlayEtcdPeersQuery
	Parameters kusto.QueryValues

	// Metric is a Prometheus metric
	Metric *prometheus.CounterVec
}
