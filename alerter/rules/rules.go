package rules

import (
	"context"
	"fmt"
	"time"

	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/azure-kusto-go/kusto"
	kustotypes "github.com/Azure/azure-kusto-go/kusto/data/types"
	"github.com/Azure/azure-kusto-go/kusto/unsafe"
	// //nolint:godot // comment does not end with a sentence // temporarily disabling code
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var rules []*Rule

// List returns the set of Rule rules.
func List() []*Rule {
	return rules
}

// VerifyRules ensures all configurations are valid and adds dynamic
// metadata as required, such as the current region.
func VerifyRules(kubeclient client.Client, region string) error {
	// Get all the rules
	ruleList := &alertrulev1.AlertRuleList{}
	if err := kubeclient.List(context.Background(), ruleList); err != nil {
		return err
	}

	for _, r := range ruleList.Items {
		rule := &Rule{
			Database:          r.Spec.Database,
			Namespace:         r.Namespace,
			Name:              r.Name,
			Interval:          r.Spec.Interval.Duration,
			Query:             r.Spec.Query,
			AutoMitigateAfter: r.Spec.AutoMitigateAfter.Duration,
		}

		rule.Stmt = kusto.NewStmt(``, kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).
			UnsafeAdd(r.Spec.Query).
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
			return fmt.Errorf("configuration %s/%s does not have the required region parameter: %w", r.Namespace, r.Name, err)
		}
		stm, err := rule.Stmt.WithParameters(params)
		if err != nil {
			return fmt.Errorf("configuration %s/%s does not contain a region configuration: %w", r.Namespace, r.Name, err)
		}
		rule.Stmt = stm
		rule.Parameters = qv

		Register(rule)
	}

	return nil
}

// Rule is analogous to a kusto-to-metric configuration, containing
// definitions and using the parlance found in the k2m UI.
type Rule struct {
	Namespace         string
	Name              string
	Database          string
	Interval          time.Duration
	Query             string
	AutoMitigateAfter time.Duration

	// Stmt specifies the underlayEtcdPeersQuery to execute.
	Stmt kusto.Stmt

	// Parameters are the parameters passed with the stmt
	Parameters kusto.QueryValues
}

func Register(rule *Rule) {
	rules = append(rules, rule)
}
