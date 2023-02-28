package alerter

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Azure/adx-mon/alert"
	"github.com/Azure/adx-mon/alerter/engine"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Azure/adx-mon/alerter/rules"
)

type AlerterOpts struct {
	Dev            bool
	KustoEndpoints map[string]string
	Region         string
	AlertAddr      string
	Cloud          string
	Port           int
	Concurrency    int
	MSIID          string
	MSIResource    string
	CtrlCli        client.Client
}

type Alerter struct {
	log      logger.Logger
	clients  map[string]KustoClient
	queue    chan struct{}
	alertCli *alert.Client
	opts     *AlerterOpts

	wg        sync.WaitGroup
	executor  *engine.Executor
	ctx       context.Context
	closeFn   context.CancelFunc
	CtrlCli   client.Client
	ruleStore *rules.Store
}

type KustoClient interface {
	Mgmt(ctx context.Context, db string, query kusto.Stmt, options ...kusto.MgmtOption) (*kusto.RowIterator, error)
	Query(ctx context.Context, db string, query kusto.Stmt, options ...kusto.QueryOption) (*kusto.RowIterator, error)
	Endpoint() string
}

var ruleErrorCounter = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "l2m",
		Subsystem: "engine",
		Name:      "errors",
		Help:      "Number of errors encountered in the primary execution engine",
	},
	[]string{"region"},
)

func NewService(opts *AlerterOpts) (*Alerter, error) {
	log, err := newLogger()
	if err != nil {
		return nil, fmt.Errorf("failed to construct logger: %w", err)
	}

	ruleStore := rules.NewStore(rules.StoreOpts{
		Region:  opts.Region,
		CtrlCli: opts.CtrlCli,
	})

	l2m := &Alerter{
		opts:      opts,
		log:       log,
		clients:   make(map[string]KustoClient),
		queue:     make(chan struct{}, opts.Concurrency),
		CtrlCli:   opts.CtrlCli,
		ruleStore: ruleStore,
	}

	if opts.MSIID != "" {
		logger.Info("Using MSI ID=%s", opts.MSIID)
	}

	for name, endpoint := range opts.KustoEndpoints {
		kcsb := kusto.NewConnectionStringBuilder(endpoint)
		if opts.MSIID == "" {
			kcsb.WithAzCli()
		} else {
			kcsb.WithUserManagedIdentity(opts.MSIID)
		}
		l2m.clients[name], err = kusto.New(kcsb)
		if err != nil {
			return nil, fmt.Errorf("kusto client=%s: %w", endpoint, err)
		}
	}

	if opts.CtrlCli == nil {
		logger.Warn("No kusto endpoints provided, using fake kusto clients")
		fakeRule := &rules.Rule{
			Namespace: "fake-namespace",
			Name:      "FakeRule",
			Database:  "FakeDB",
			Interval:  time.Minute,
			Query:     `UnderlayNodeInfo | where Region == ParamRegion | limit 1 | project Title="test"`,
		}
		l2m.clients[fakeRule.Database] = fakeKustoClient{endpoint: "http://fake.endpoint"}
		ruleStore.Register(fakeRule)
	}

	if opts.AlertAddr == "" {
		logger.Warn("No alert address provided, using fake alert handler")
		http.Handle("/alerts", fakeAlertHandler())
		opts.AlertAddr = fmt.Sprintf("http://localhost:%d", opts.Port)
	}

	alertCli, err := alert.NewClient(time.Minute)
	if err != nil {
		return nil, fmt.Errorf("failed to create alert client: %w", err)
	}
	l2m.alertCli = alertCli

	executor := &engine.Executor{
		AlertAddr:   opts.AlertAddr,
		AlertCli:    alertCli,
		KustoClient: l2m,
		RuleStore:   ruleStore,
	}
	l2m.executor = executor
	return l2m, nil
}

func (l *Alerter) Open(ctx context.Context) error {
	l.ctx, l.closeFn = context.WithCancel(ctx)

	logger.Info("Starting adx-mon alerter")

	if err := l.ruleStore.Open(ctx); err != nil {
		return fmt.Errorf("failed to open rule store: %w", err)
	}

	if err := l.executor.Open(ctx); err != nil {
		return fmt.Errorf("failed to open executor: %w", err)
	}

	go func() {
		logger.Info("Listening at :%d", l.opts.Port)
		http.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(fmt.Sprintf(":%d", l.opts.Port), nil); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}()

	return nil
}

func (l *Alerter) Endpoint(db string) string {
	if l.clients[db] != nil {
		return l.clients[db].Endpoint()
	} else {
		return "unknown endpoint (is nil)"
	}
}

func (l *Alerter) Query(ctx context.Context, r rules.Rule, fn func(string, rules.Rule, *table.Row) error) error {
	client := l.clients[r.Database]
	if client == nil {
		return fmt.Errorf("no client found for database=%s", r.Database)
	}

	var iter *kusto.RowIterator
	var err error
	if r.IsMgmtQuery {
		iter, err = client.Mgmt(ctx, r.Database, r.Stmt)
		if err != nil {
			return fmt.Errorf("failed to execute management kusto query=%s/%s: %s", r.Namespace, r.Name, err)
		}
	} else {
		iter, err = client.Query(ctx, r.Database, r.Stmt, kusto.ResultsProgressiveDisable())
		if err != nil {
			return fmt.Errorf("failed to execute kusto query=%s/%s: %s", r.Namespace, r.Name, err)
		}
	}
	defer iter.Stop()
	return iter.Do(func(row *table.Row) error {
		return fn(client.Endpoint(), r, row)
	})
}

func newLogger() (logger.Logger, error) {
	return logger.NewLogger(), nil
}

func (l *Alerter) Close() error {
	l.closeFn()
	if err := l.executor.Close(); err != nil {
		return fmt.Errorf("failed to close executor: %w", err)
	}

	if err := l.ruleStore.Close(); err != nil {
		return fmt.Errorf("failed to close rule store: %w", err)
	}
	return nil
}
