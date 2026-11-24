package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/adx-mon/alerter/alert"
	"github.com/Azure/adx-mon/alerter/rules"
	"github.com/Azure/adx-mon/metrics"
	azquery "github.com/Azure/azure-kusto-go/azkustodata/query"
	aztypes "github.com/Azure/azure-kusto-go/azkustodata/types"
	azvalue "github.com/Azure/azure-kusto-go/azkustodata/value"
	"github.com/stretchr/testify/require"
)

type sliceRuleStore struct {
	rules []*rules.Rule
}

func (s *sliceRuleStore) Rules() []*rules.Rule {
	return s.rules
}

func TestExecutor_FanoutConcurrentBurst10Rules25RowsAnd429Isolation(t *testing.T) {
	const (
		numRules    = 10
		rowsPerRule = 25
		totalAlerts = numRules * rowsPerRule
	)

	type recorded struct {
		mu        sync.Mutex
		regular   []alert.Alert
		summaries []alert.Alert
	}
	makeHealthy := func(rec *recorded) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			var a alert.Alert
			if err := json.Unmarshal(b, &a); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			rec.mu.Lock()
			if strings.HasPrefix(a.Source, "notification-failure/") {
				rec.summaries = append(rec.summaries, a)
			} else {
				rec.regular = append(rec.regular, a)
			}
			rec.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		}))
	}

	var rec1, rec2, rec429 recorded
	healthy1 := makeHealthy(&rec1)
	defer healthy1.Close()
	healthy2 := makeHealthy(&rec2)
	defer healthy2.Close()

	srv429 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var a alert.Alert
		if err := json.Unmarshal(b, &a); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if strings.HasPrefix(a.Source, "notification-failure/") {
			rec429.mu.Lock()
			rec429.summaries = append(rec429.summaries, a)
			rec429.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv429.Close()

	fc, err := alert.NewFanoutClient(
		[]string{healthy1.URL, healthy2.URL, srv429.URL},
		alert.WithConcurrency(10),
	)
	require.NoError(t, err)
	ids := fc.EndpointIDs()
	beforeOverflow := make([]float64, len(ids))
	for i, id := range ids {
		beforeOverflow[i] = getCounterValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(id, alert.DropReasonOverflow))
	}

	var queriesCompleted atomic.Int32
	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, handler func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			cols := azquery.Columns{
				testColumn(0, "Title", aztypes.String),
				testColumn(1, "Severity", aztypes.Long),
				testColumn(2, "Summary", aztypes.String),
				testColumn(3, "CorrelationId", aztypes.String),
			}
			for i := range rowsPerRule {
				row := testRow(cols, azvalue.Values{
					azvalue.NewString(fmt.Sprintf("%s-row-%02d", qc.Rule.Name, i)),
					azvalue.NewLong(2),
					azvalue.NewString("row summary"),
					azvalue.NewString(fmt.Sprintf("corr-%02d", i)),
				})
				if err := handler(ctx, "https://fakedb.mockcluster.kusto.windows.net", qc, row); err != nil {
					return err, i + 1
				}
			}
			queriesCompleted.Add(1)
			return nil, rowsPerRule
		},
	}

	ruleList := make([]*rules.Rule, numRules)
	for i := range numRules {
		ruleList[i] = &rules.Rule{
			Namespace:   "burst-ns",
			Name:        fmt.Sprintf("rule-%02d", i),
			Database:    "fakedb",
			Destination: "icm://owning-team",
			Interval:    time.Hour,
			Query:       "Metrics | take 25",
		}
	}

	require.NoError(t, fc.Open(context.Background()))
	exec := NewExecutor(ExecutorOpts{
		AlertCli:    fc,
		KustoClient: kcli,
		RuleStore:   &sliceRuleStore{rules: ruleList},
		Region:      "westus2",
		Concurrency: 10,
	})
	execCtx, execCancel := context.WithCancel(context.Background())
	require.NoError(t, exec.Open(execCtx))

	require.Eventually(t, func() bool {
		return queriesCompleted.Load() == numRules
	}, 5*time.Second, 5*time.Millisecond)

	// Graceful shutdown sequence: cancel service ctx, close Executor producers first, then drain FanoutClient.
	execCancel()
	require.NoError(t, exec.Close())
	require.NoError(t, fc.Close())

	require.Len(t, rec1.regular, totalAlerts)
	require.Empty(t, rec1.summaries)
	require.Len(t, rec2.regular, totalAlerts)
	require.Empty(t, rec2.summaries)
	for i, id := range ids {
		require.Equal(t, beforeOverflow[i], getCounterValue(t, metrics.NotificationsDroppedTotal.WithLabelValues(id, alert.DropReasonOverflow)))
	}

	require.Empty(t, rec429.regular)
	require.Len(t, rec429.summaries, numRules)

	bySource := make(map[string]alert.Alert, numRules)
	for _, s := range rec429.summaries {
		_, dup := bySource[s.Source]
		require.False(t, dup, "duplicate summary POST for %s", s.Source)
		bySource[s.Source] = s
		require.Contains(t, s.Summary, "Click here to show query")
		require.Contains(t, s.Summary, "10 additional suppressed alerts are not shown")
	}
}

func TestExecutor_FanoutCombinedKustoOverflowAndHTTP429StillEmitsExactOneSummary(t *testing.T) {
	var healthyRegular, healthySummaries, throttledSummaries atomic.Int32
	blockerStarted := make(chan struct{})
	releaseBlocker := make(chan struct{})

	srvHealthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var a alert.Alert
		_ = json.Unmarshal(b, &a)
		if a.Title == "blocker" {
			close(blockerStarted)
			<-releaseBlocker
			w.WriteHeader(http.StatusCreated)
			return
		}
		if strings.HasPrefix(a.Source, "notification-failure/") {
			healthySummaries.Add(1)
		} else {
			healthyRegular.Add(1)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srvHealthy.Close()

	srv429 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var a alert.Alert
		_ = json.Unmarshal(b, &a)
		if a.Title == "blocker" {
			w.WriteHeader(http.StatusCreated)
			return
		}
		if strings.HasPrefix(a.Source, "notification-failure/") {
			throttledSummaries.Add(1)
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv429.Close()

	// Single worker occupied on "blocker" while the 26-row evaluation queues its 25 allowed rows and seals overflow:
	// healthy endpoint must still deliver all 25 allowed rows before emitting its exact-1 overflow summary.
	fc, err := alert.NewFanoutClient([]string{srvHealthy.URL, srv429.URL}, alert.WithConcurrency(1))
	require.NoError(t, err)
	require.NoError(t, fc.Open(context.Background()))

	require.NoError(t, fc.Create(context.Background(), "", alert.Alert{Title: "blocker", Severity: 1, Source: "other/rule"}))
	select {
	case <-blockerStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out after 5s waiting for blockerStarted")
	}

	kcli := &fakeKustoClient{
		queryFn: func(ctx context.Context, qc *QueryContext, handler func(context.Context, string, *QueryContext, azquery.Row) error) (error, int) {
			cols := azquery.Columns{
				testColumn(0, "Title", aztypes.String),
				testColumn(1, "Severity", aztypes.Long),
			}
			for i := range 25 {
				row := testRow(cols, azvalue.Values{
					azvalue.NewString(fmt.Sprintf("row-%d", i)),
					azvalue.NewLong(2),
				})
				if err := handler(ctx, "https://fakedb.mockcluster.kusto.windows.net", qc, row); err != nil {
					return err, i + 1
				}
			}
			var overflow ThrottledNotificationsError
			overflow.Add(AlertResult{Title: "kusto-overflow-row-26", Severity: 2})
			return fmt.Errorf("row cap exceeded: %w", &overflow), 26
		},
	}

	rule := &rules.Rule{
		Namespace:   "combo-ns",
		Name:        "combo-rule",
		Database:    "fakedb",
		Destination: "icm://owning-team",
		Query:       "Metrics | take 100",
	}

	exec := NewExecutor(ExecutorOpts{
		AlertCli:    fc,
		KustoClient: kcli,
		RuleStore:   &sliceRuleStore{rules: []*rules.Rule{rule}},
		Region:      "westus2",
		Concurrency: 4,
	})
	exec.RunOnce(context.Background())

	// Evaluation has already sealed with row-cap overflow while all 25 allowed rows are still queued on srvHealthy!
	close(releaseBlocker)
	require.NoError(t, fc.Close())

	require.Equal(t, int32(25), healthyRegular.Load(), "healthy endpoint must deliver all 25 allowed rows before row-cap summary")
	require.Equal(t, int32(1), healthySummaries.Load())
	require.Equal(t, int32(1), throttledSummaries.Load())
}
