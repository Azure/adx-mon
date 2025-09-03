package rules

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/logger"

	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/unsafe"

	"github.com/google/cel-go/cel"

	// //nolint:godot // comment does not end with a sentence // temporarily disabling code
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type StoreOpts struct {
	Region  string
	CtrlCli client.Client
}

type Store struct {
	ctx    context.Context
	cancel context.CancelFunc

	ctrlCli client.Client
	opts    StoreOpts

	wg sync.WaitGroup

	mu    sync.RWMutex
	rules []*Rule
}

func NewStore(opts StoreOpts) *Store {
	return &Store{
		ctrlCli: opts.CtrlCli,
		opts:    opts,
	}
}

func (s *Store) Open(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctx, s.cancel = context.WithCancel(ctx)

	// If we have no kube client, don't try to reload rules
	if s.ctrlCli == nil {
		return nil
	}

	rules, err := s.reloadRules()
	if err != nil {
		return err
	}

	s.rules = rules
	go s.reloadPeriodically()
	return nil
}

func (s *Store) Close() error {
	s.cancel()
	s.wg.Wait()
	return nil
}

func (s *Store) Rules() []*Rule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rules
}

func toRule(r alertrulev1.AlertRule, region string) (*Rule, error) {
	rule := &Rule{
		Version:            r.ResourceVersion,
		Database:           r.Spec.Database,
		Namespace:          r.Namespace,
		Name:               r.Name,
		Interval:           r.Spec.Interval.Duration,
		Query:              r.Spec.Query,
		Destination:        r.Spec.Destination,
		AutoMitigateAfter:  r.Spec.AutoMitigateAfter.Duration,
		Criteria:           r.Spec.Criteria,
		CriteriaExpression: r.Spec.CriteriaExpression,
		IsMgmtQuery:        false,
		LastQueryTime:      r.Status.LastQueryTime.Time,
	}

	// If a query starts with a dot then it is acting against that Kusto cluster and not looking through
	// rows in any particular table. So we don't want to wrap the query with the ParamRegion query_parameter()
	// declaration because then Kusto will say it's an invalid query.
	rule.IsMgmtQuery = strings.HasPrefix(r.Spec.Query, ".")

	stmt := kusto.NewStmt(``, kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).UnsafeAdd(r.Spec.Query)

	rule.Stmt = stmt
	return rule, nil
}

func (s *Store) reloadRules() ([]*Rule, error) {
	ruleList := &alertrulev1.AlertRuleList{}
	if err := s.ctrlCli.List(context.Background(), ruleList); err != nil {
		return nil, fmt.Errorf("failed to list alert rules: %w", err)
	}

	var rules = make([]*Rule, 0, len(ruleList.Items))
	for _, r := range ruleList.Items {
		rule, err := toRule(r, s.opts.Region)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func (s *Store) reloadPeriodically() {
	s.wg.Add(1)
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(time.Minute):
			logger.Infof("Reloading rules...")

			rules, err := s.reloadRules()
			if err != nil {
				logger.Errorf("failed to reload rules: %s", err)
				continue
			}

			s.mu.Lock()
			s.rules = rules
			s.mu.Unlock()
		}
	}
}

func (s *Store) Register(rule *Rule) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.rules = append(s.rules, rule)
}

// Rule is analogous to a kusto-to-metric configuration, containing
// definitions and using the parlance found in the k2m UI.
type Rule struct {
	Version           string
	Namespace         string
	Name              string
	Database          string
	Interval          time.Duration
	Query             string
	AutoMitigateAfter time.Duration
	Destination       string

	// Criteria is a map of key-value pairs that are used to determine where an alert can execute.
	Criteria map[string][]string
	// CriteriaExpression is an optional CEL expression used for richer execution control.
	CriteriaExpression string

	// Management queries (starts with a dot) have to call a different
	// query API in the Kusto Go SDK.
	IsMgmtQuery bool

	// Stmt specifies the underlayEtcdPeersQuery to execute.
	Stmt kusto.Stmt

	// LastQueryTime from the AlertRule status, used for smart scheduling
	LastQueryTime time.Time
}

// Matches evaluates whether this rule should execute based on simple Criteria AND the CEL CriteriaExpression.
// It returns true if both the map criteria matches and the expression evaluates to true.
// Errors are returned for invalid criteria keys (referencing tags that don't exist) or CEL compilation issues.
func (r *Rule) Matches(tags map[string]string) (bool, error) {
	// Normalize provided tags (lowercase keys & values) without mutating caller map.
	lowered := make(map[string]string, len(tags))
	for k, v := range tags {
		lowered[strings.ToLower(k)] = strings.ToLower(v)
	}

	// Map criteria evaluation (OR semantics across keys)
	var criteriaMatched bool
	if len(r.Criteria) == 0 {
		criteriaMatched = true
	} else {
		for k, values := range r.Criteria {
			keyLower := strings.ToLower(k)
			vv, ok := lowered[keyLower]
			if !ok { // key absent -> cannot match this key; continue checking others (OR semantics)
				continue
			}
			for _, candidate := range values {
				if strings.EqualFold(vv, candidate) {
					criteriaMatched = true
					break
				}
			}
			if criteriaMatched { // OR semantics across keys/values
				break
			}
		}
	}

	var expressionMatched bool
	if r.CriteriaExpression == "" {
		expressionMatched = true
	} else {
		// Define variables based on our tags
		varDecls := make([]cel.EnvOption, 0)
		activation := map[string]interface{}{}
		for k, v := range lowered {
			varDecls = append(varDecls, cel.Variable(k, cel.StringType))
			activation[k] = v
		}
		env, err := cel.NewEnv(varDecls...)
		if err != nil {
			return false, fmt.Errorf("cel env error: %w", err)
		}

		// Parse the expression and evaluate
		ast, iss := env.Parse(r.CriteriaExpression)
		if iss.Err() != nil {
			return false, fmt.Errorf("cel parse error: %w", iss.Err())
		}
		checked, iss2 := env.Check(ast)
		if iss2.Err() != nil {
			return false, fmt.Errorf("cel typecheck error: %w", iss2.Err())
		}
		prog, err := env.Program(checked)
		if err != nil {
			return false, fmt.Errorf("cel program build error: %w", err)
		}
		out, _, err := prog.Eval(activation)
		if err != nil {
			return false, fmt.Errorf("cel eval error: %w", err)
		}
		if b, ok := out.Value().(bool); ok && b {
			expressionMatched = true
		}
	}

	return criteriaMatched && expressionMatched, nil
}
