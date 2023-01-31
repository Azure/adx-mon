package rules

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/types"
	"github.com/Azure/azure-kusto-go/kusto/unsafe"
	"github.com/prometheus/client_golang/prometheus"
	"gopkg.in/yaml.v2"
	// //nolint:godot // comment does not end with a sentence // temporarily disabling code
	// Load go-based rules: only temporarily in place, will
	// be replaced by a metrics loader
	// _ "goms.io/aks/aksiknife/pkg/logstometrics/rules/underlay"
)

// ///go:embed underlay/*.yaml
var content embed.FS

// List returns the set of Rule rules.
func List() []*Rule {
	return rules
}

// VerifyRules ensures all configurations are valid and adds dynamic
// metadata as required, such as the current region.
func VerifyRules(region string) error {
	// Load the yaml-based configurations / rules
	err := fs.WalkDir(content, ".", func(path string, d fs.DirEntry, err error) error {

		if filepath.Ext(d.Name()) == ".yaml" {
			data, err := content.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read embedded %s: %w", path, err)
			}

			var rule map[string]*Rule
			if err := yaml.Unmarshal(data, &rule); err != nil {
				return fmt.Errorf("failed to load rule %s: %w", path, err)
			}
			for k, v := range rule {
				// Set the Rule's name
				v.DisplayName = k
				// Create a Kusto statement
				v.Stmt = kusto.NewStmt(``, kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).
					UnsafeAdd(v.Query).
					MustDefinitions(
						kusto.NewDefinitions().Must(
							kusto.ParamTypes{
								"ParamRegion": kusto.ParamType{Type: types.String},
							},
						),
					)
				// Optionally create a Prometheus Metric
				if v.Value != "" && len(v.Dimensions) != 0 {
					v.Metric = prometheus.NewCounterVec(
						prometheus.CounterOpts{
							Namespace: "l2m",
							Subsystem: v.DisplayName,
							Name:      v.Value,
							Help:      v.DisplayName,
						},
						v.Dimensions,
					)
					if err := prometheus.Register(v.Metric); err != nil {
						return fmt.Errorf("failed to register metric=%s: %w", v.DisplayName, err)
					}
				}
				Register(v)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to load rules: %w", err)
	}

	for _, rule := range rules {
		qv := kusto.QueryValues{}
		qv["ParamRegion"] = region
		params, err := kusto.NewParameters().With(qv)
		if err != nil {
			return fmt.Errorf("configuration %s does not have the required region parameter: %w", rule.DisplayName, err)
		}
		stm, err := rule.Stmt.WithParameters(params)
		if err != nil {
			return fmt.Errorf("configuration %s does not contain a region configuration: %w", rule.DisplayName, err)
		}
		rule.Stmt = stm
		rule.Parameters = qv
	}
	return nil
}

// Rule is analogous to a kusto-to-metric configuration, containing
// definitions and using the parlance found in the k2m UI.
type Rule struct {
	// DisplayName is the name of your rule
	DisplayName string
	// Database is the database you want to execute your underlayEtcdPeersQuery against
	Database string `yaml:"Database"`
	// Interval defines how frequently your underlayEtcdPeersQuery will be executed in minutes.
	Interval time.Duration `yaml:"Interval"`

	// Query is the underlayEtcdPeersQuery text to execute.
	Query string `yaml:"Query"`

	// Stmt specifies the underlayEtcdPeersQuery to execute.
	Stmt kusto.Stmt

	// Parameters are the parameters passed with the underlayEtcdPeersQuery
	Parameters kusto.QueryValues

	// RoutingID is the ICM routing ICM to use when creating ICMs.
	RoutingID string `yaml:"RoutingID"`

	// TSG is the URL to the TSG for the ICM.
	TSG string `yaml:"TSG"`

	// Dimensions is an array of Prometheus dimensions as projected
	// from the Kusto query result
	Dimensions []string `yaml:"Dimensions"`

	// Value is the Kusto column to be used as a metric value
	Value string `yaml:"Value"`

	// Metric is a Prometheus metric
	Metric *prometheus.CounterVec

	// AutoMitigateAfter is a duration when to automitigate an ICM if it has no longer correlated in that time.
	AutoMitigateAfter time.Duration `yaml:"AutoMitigateAfter"`
}

func Register(rule *Rule) {
	rules = append(rules, rule)
}

var rules []*Rule
