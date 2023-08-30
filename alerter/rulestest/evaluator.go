package rulestest

import (
	"context"
	"fmt"
	"io"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/alerter/engine"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/adx-mon/ingestor/storage"
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
	rules       []*rules.Rule
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
	return nil
}
