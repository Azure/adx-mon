package rules

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/celutil"
	"github.com/Azure/adx-mon/pkg/logger"
	"k8s.io/utils/clock"

	// //nolint:godot // comment does not end with a sentence // temporarily disabling code
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type StoreOpts struct {
	Region  string
	CtrlCli client.Client
	Clock   clock.WithTicker
}

type Store struct {
	ctx    context.Context
	cancel context.CancelFunc

	ctrlCli client.Client
	opts    StoreOpts
	clock   clock.WithTicker

	wg sync.WaitGroup

	mu    sync.RWMutex
	rules []*Rule
}

func NewStore(opts StoreOpts) *Store {
	c := opts.Clock
	if c == nil {
		c = clock.RealClock{}
	}
	return &Store{
		ctrlCli: opts.CtrlCli,
		opts:    opts,
		clock:   c,
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

	s.wg.Add(1)
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
	if err := validateInterval(r.Spec.Interval.Duration); err != nil {
		return nil, fmt.Errorf("alert rule %s/%s: %w", r.Namespace, r.Name, err)
	}
	if err := validateDestinations(r.Spec.Destination, r.Spec.Destinations); err != nil {
		return nil, fmt.Errorf("alert rule %s/%s: %w", r.Namespace, r.Name, err)
	}

	var dests []string
	hasExplicit := len(r.Spec.Destinations) > 0
	if hasExplicit {
		dests = slices.Clone(r.Spec.Destinations)
	}

	rule := &Rule{
		UID:                     string(r.UID),
		Generation:              r.Generation,
		Version:                 fmt.Sprintf("%d", r.Generation),
		Database:                r.Spec.Database,
		Namespace:               r.Namespace,
		Name:                    r.Name,
		Interval:                r.Spec.Interval.Duration,
		Query:                   r.Spec.Query,
		Destination:             r.Spec.Destination,
		Destinations:            dests,
		HasExplicitDestinations: hasExplicit,
		AutoMitigateAfter:       r.Spec.AutoMitigateAfter.Duration,
		Criteria:                r.Spec.Criteria,
		CriteriaExpression:      r.Spec.CriteriaExpression,
		IsMgmtQuery:             false,
		LastQueryTime:           r.Status.LastQueryTime.Time,
	}

	// If a query starts with a dot then it is acting against that Kusto cluster and not looking through
	// rows in any particular table. So we don't want to wrap the query with the ParamRegion query_parameter()
	// declaration because then Kusto will say it's an invalid query.
	rule.IsMgmtQuery = strings.HasPrefix(r.Spec.Query, ".")

	return rule, nil
}

func validateInterval(interval time.Duration) error {
	if interval < alertrulev1.MinAlertRuleInterval {
		return fmt.Errorf("interval %s must be at least %s", interval, alertrulev1.MinAlertRuleInterval)
	}
	return nil
}

func validateDestinations(destination string, destinations []string) error {
	if destination != "" && len(destinations) > 0 {
		return errors.New("destination and destinations are mutually exclusive")
	}
	if len(destinations) > alertrulev1.MaxAlertRuleDestinations {
		return fmt.Errorf("destinations cannot contain more than %d entries", alertrulev1.MaxAlertRuleDestinations)
	}
	seen := make(map[string]struct{}, len(destinations))
	for _, d := range destinations {
		if d == "" {
			return errors.New("destinations entries must not be empty")
		}
		if utf8.RuneCountInString(d) > alertrulev1.MaxAlertRuleDestinationLength {
			return fmt.Errorf("destinations entry exceeds maximum length %d", alertrulev1.MaxAlertRuleDestinationLength)
		}
		if _, dup := seen[d]; dup {
			return fmt.Errorf("destinations contains duplicate entry %q", d)
		}
		seen[d] = struct{}{}
	}
	return nil
}

func (s *Store) reloadRules() ([]*Rule, error) {
	ctx, cancel := context.WithTimeout(s.ctx, 1*time.Minute)
	defer cancel()

	ruleList := &alertrulev1.AlertRuleList{}
	if err := s.ctrlCli.List(ctx, ruleList); err != nil {
		return nil, fmt.Errorf("failed to list alert rules: %w", err)
	}

	var rules = make([]*Rule, 0, len(ruleList.Items))
	for _, r := range ruleList.Items {
		rule, err := toRule(r, s.opts.Region)
		if err != nil {
			logger.Errorf("Skipping invalid AlertRule: %v", err)
			continue
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

const reloadInterval = time.Minute

func (s *Store) reloadPeriodically() {
	defer s.wg.Done()

	// Reuse one timer and preserve cadence when a reload takes longer than the
	// interval, rather than starting a fresh delay after each reload finishes.
	ticker := s.clock.NewTicker(reloadInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C():
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
	UID                     string
	Generation              int64
	Version                 string
	Namespace               string
	Name                    string
	Database                string
	Interval                time.Duration
	Query                   string
	AutoMitigateAfter       time.Duration
	Destination             string
	Destinations            []string
	HasExplicitDestinations bool

	// Criteria is a map of key-value pairs that are used to determine where an alert can execute.
	Criteria map[string][]string
	// CriteriaExpression is an optional CEL expression used for richer execution control.
	CriteriaExpression string

	// Management queries (starts with a dot) have to call a different
	// query API in the Kusto Go SDK.
	IsMgmtQuery bool

	// LastQueryTime from the AlertRule status, used for smart scheduling
	LastQueryTime time.Time
}

// ExplicitDestinations reports whether AlertRule.spec.destinations was authored on this rule.
func (r *Rule) ExplicitDestinations() bool {
	return r != nil && (r.HasExplicitDestinations || len(r.Destinations) > 0)
}

// CandidateDestinations returns the rule-level ordered candidate destination slice used for capability matching.
func (r *Rule) CandidateDestinations() []string {
	if r == nil {
		return []string{""}
	}
	if len(r.Destinations) > 0 {
		return slices.Clone(r.Destinations)
	}
	if r.Destination != "" {
		return []string{r.Destination}
	}
	return []string{""}
}

// ResolvedMetaDestinations returns the resolved Destination and optional explicit Destinations slice for meta-alerts.
func (r *Rule) ResolvedMetaDestinations() (string, []string) {
	if r == nil {
		return "", nil
	}
	if len(r.Destinations) > 0 {
		return r.Destinations[0], slices.Clone(r.Destinations)
	}
	return r.Destination, nil
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

	// Map criteria evaluation - only requires one matching key/value
	// We do not currently enforce that unknown keys are an error like with criteriaExpression
	// for backwards compatibility
	var criteriaMatched bool
	if len(r.Criteria) == 0 {
		criteriaMatched = true
	} else {
		for k, values := range r.Criteria {
			keyLower := strings.ToLower(k)
			vv, ok := lowered[keyLower]
			if !ok { // key absent -> cannot match this key; continue checking others
				continue
			}
			for _, candidate := range values {
				if strings.EqualFold(vv, candidate) {
					criteriaMatched = true
					break
				}
			}
		}
	}

	expressionMatched := false
	if r.CriteriaExpression == "" {
		expressionMatched = true
	} else {
		// do not pass lowered - for expressions, match on the actual cases.
		matched, err := celutil.EvaluateCriteriaExpression(tags, r.CriteriaExpression)
		if err != nil {
			return false, fmt.Errorf("failed to evaluate criteriaExpression: %w", err)
		}
		expressionMatched = matched
	}

	return criteriaMatched && expressionMatched, nil
}
