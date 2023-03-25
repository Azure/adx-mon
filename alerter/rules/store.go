package rules

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/logger"

	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/azure-kusto-go/kusto"
	kustotypes "github.com/Azure/azure-kusto-go/kusto/data/types"
	"github.com/Azure/azure-kusto-go/kusto/unsafe"

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
		Version:           r.ResourceVersion,
		Database:          r.Spec.Database,
		Namespace:         r.Namespace,
		Name:              r.Name,
		Interval:          r.Spec.Interval.Duration,
		Query:             r.Spec.Query,
		Destination:       r.Spec.Destination,
		AutoMitigateAfter: r.Spec.AutoMitigateAfter.Duration,
		IsMgmtQuery:       false,
	}

	qv := kusto.QueryValues{}
	stmt := kusto.NewStmt(``, kusto.UnsafeStmt(unsafe.Stmt{Add: true, SuppressWarning: true})).UnsafeAdd(r.Spec.Query)
	// If a query starts with a dot then it is acting against that Kusto cluster and not looking through
	// rows in any particular table. So we don't want to wrap the query with the ParamRegion query_parameter()
	// declaration because then Kusto will say it's an invalid query.
	if !strings.HasPrefix(r.Spec.Query, ".") {
		rule.Stmt = stmt.MustDefinitions(
			kusto.NewDefinitions().Must(
				kusto.ParamTypes{
					"ParamRegion": kusto.ParamType{Type: kustotypes.String},
				},
			),
		)

		qv["ParamRegion"] = region
		params, err := kusto.NewParameters().With(qv)
		if err != nil {
			return nil, fmt.Errorf("configuration %s/%s does not have the required region parameter: %w", r.Namespace, r.Name, err)
		}
		stmt, err = rule.Stmt.WithParameters(params)
		if err != nil {
			return nil, fmt.Errorf("configuration %s/%s does not contain a region configuration: %w", r.Namespace, r.Name, err)
		}
	} else {
		rule.IsMgmtQuery = true
	}
	rule.Stmt = stmt
	rule.Parameters = qv
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
			logger.Info("Reloading rules...")

			rules, err := s.reloadRules()
			if err != nil {
				logger.Error("failed to reload rules: %s", err)
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

	// Management queries (starts with a dot) have to call a different
	// query API in the Kusto Go SDK.
	IsMgmtQuery bool

	// Stmt specifies the underlayEtcdPeersQuery to execute.
	Stmt kusto.Stmt

	// Parameters are the parameters passed with the stmt
	Parameters kusto.QueryValues
}
