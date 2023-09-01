package rulestest

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"text/template"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/alerter/engine"
	"github.com/Azure/adx-mon/alerter/rules"
)

type Client interface {
	Query(ctx context.Context, qc *engine.QueryContext) ([]*alert.Alert, error)
}

type EvaluatorOpts struct {
}

type Evaluator struct {
	kustoClient Client
	rules       map[string]*rules.Rule
	tests       []*Test
}

func NewEvaluator(client Client, testRules []*rules.Rule, tests []*Test) (*Evaluator, error) {
	ruleMap := make(map[string]*rules.Rule, len(testRules))
	for _, rule := range testRules {
		if _, ok := ruleMap[rule.Name]; ok {
			return nil, fmt.Errorf("duplicate rule name: %q", rule.Name)
		}
		ruleMap[rule.Name] = rule
	}
	return &Evaluator{
		kustoClient: client,
		rules:       ruleMap,
		tests:       tests,
	}, nil
}

func (e *Evaluator) RunAndPrintTests(w io.Writer) error {
	results := e.runTests()
	failures := 0

	t := template.Must(template.New("test").Parse(resultsTemplateStr))
	err := t.Execute(w, results)
	if err != nil {
		return err
	}
	for _, result := range results {
		if result.Failed() {
			failures++
		}
	}

	if failures > 0 {
		return fmt.Errorf("%d tests failed out of %d", failures, len(results))
	}
	return nil
}

func (e *Evaluator) ruleForTest(name string, test *Test) (*rules.Rule, error) {
	rule, ok := e.rules[name]
	if !ok {
		return nil, fmt.Errorf("rule \"%q\" not found", name)
	}

	letStatements, err := test.getDatatableStmt(nil)
	if err != nil {
		return nil, err
	}
	// make a copy of rule so the query isn't permanently modified.
	tmp := *rule
	tmp.Query = fmt.Sprintf("%s\n%s", letStatements, rule.Query)
	return &tmp, err
}

func (e *Evaluator) runTests() []result {
	var results []result
	for _, test := range e.tests {
		results = append(results, e.runTest(test)...)
	}
	return results
}

func (e *Evaluator) runTest(test *Test) (results []result) {
	curTime := time.Time{}
	for _, testAt := range test.expectedAlerts {
		rule, err := e.ruleForTest(testAt.Name, test)
		if err != nil {
			results = append(results, test.Error(testAt, err))
			continue
		}
		qc, err := engine.NewQueryContext(rule, curTime.Add(testAt.EvalAt), "local")
		if err != nil {
			results = append(results, test.Error(testAt, fmt.Errorf("failed to create query context: %s", err)))
			continue
		}
		got, err := e.kustoClient.Query(context.Background(), qc)
		if err != nil {
			results = append(results, test.Error(testAt, fmt.Errorf("failed to execute query: %s", err)))
			continue
		}

		extraAlerts, missingAlerts := nonMatchingAlerts(testAt.Alerts, got)
		results = append(results, test.Result(testAt, missingAlerts, extraAlerts))
	}
	return
}

func nonMatchingAlerts(want, got []*alert.Alert) (additional []*alert.Alert, missing []*alert.Alert) {
	for _, a := range got {
		found := false
		for _, b := range want {
			if reflect.DeepEqual(a, b) {
				found = true
				break
			}
		}
		if !found {
			additional = append(additional, a)
		}
	}

	for _, a := range want {
		found := false
		for _, b := range got {
			if reflect.DeepEqual(a, b) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, a)
		}
	}

	return
}
