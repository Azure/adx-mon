package rulestest

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/alerter/engine"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/go-test/deep"
)

type testResult struct {
	name    string
	failed  bool
	message string
}

func (r *testResult) String() string {
	if r.failed {
		return fmt.Sprintf("Test %s failed: %s\n", r.name, r.message)
	}
	return fmt.Sprintf("%s: ok\n", r.name)
}

type Client interface {
	Query(ctx context.Context, qc *engine.QueryContext) ([]*alert.Alert, int, error)
}

type Evaluator struct {
	kustoClient Client
	rules       map[string]*rules.Rule
	tests       []*Test
	schema      storage.SchemaMapping
}

func (e *Evaluator) RunAndPrintTests(w io.Writer) {
	results := e.RunTests()
	for _, result := range results {
		fmt.Fprint(w, result.String())
	}
}

func (e *Evaluator) RunTests() []*testResult {
	var results []*testResult
	for _, test := range e.tests {
		results = append(results, e.runTest(test))
	}
	return results
}

func (e *Evaluator) runTest(test *Test) *testResult {
	curTime := time.Time{}
	for _, expected := range test.expectedAlerts {
		qc, err := engine.NewQueryContext(e.rules[expected.Name], curTime.Add(expected.EvalAt), "local")
		if err != nil {
			return &testResult{
				name:    test.name,
				failed:  true,
				message: fmt.Sprintf("failed to create query context: %s", err),
			}
		}
		alerts, _, err := e.kustoClient.Query(context.Background(), qc)
		if err != nil {
			return &testResult{
				name:    test.name,
				failed:  true,
				message: fmt.Sprintf("failed to execute query: %s", err),
			}
		}
		if len(alerts) != 1 {
			return &testResult{
				name:    test.name,
				failed:  true,
				message: fmt.Sprintf("expected 1 alert, got %d", len(alerts)),
			}
		}

		if diff := deep.Equal(); len(diff) > 0 {
			return &testResult{
				name:    test.name,
				failed:  true,
				message: fmt.Sprintf("expected alert %s, got %s\n\ndiff: %s", expected.Alert, alerts[0], diff),
			}
		}
		return &testResult{
			name:   test.name,
			failed: true,
			// message: fmt.Sprintf("expected alert name %s, got %s", expected.Alert.Name, alerts[0].Name),
		}
	}
	return nil
}
