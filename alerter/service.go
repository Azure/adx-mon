package alerter

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Azure/adx-mon/metrics"

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

	// MaxNotifications is the maximum number of notifications to send per rule.
	MaxNotifications int

	MSIID       string
	MSIResource string
	CtrlCli     client.Client
}

// share with executor or fine for both to define privately?
type ruleStore interface {
	Rules() []*rules.Rule
	Open(context.Context) error
	Close() error
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
	ruleStore ruleStore
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

	executor := engine.NewExecutor(
		engine.ExecutorOpts{
			AlertCli:    alertCli,
			AlertAddr:   opts.AlertAddr,
			Region:      opts.Region,
			KustoClient: l2m,
			RuleStore:   ruleStore,
		})

	l2m.executor = executor
	return l2m, nil
}

func Lint(ctx context.Context, opts *AlerterOpts, path string) error {
	log, err := newLogger()
	if err != nil {
		return fmt.Errorf("failed to construct logger: %w", err)
	}

	ruleStore, err := rules.FromPath(path, opts.Region)
	if err != nil {
		return err
	}
	log.Info("Linting rules from path=%s", path)

	var clients map[string]KustoClient
	for name, endpoint := range opts.KustoEndpoints {
		kcsb := kusto.NewConnectionStringBuilder(endpoint).WithAzCli()
		client, err := kusto.New(kcsb)
		if err != nil {
			return fmt.Errorf("kusto client=%s: %w", endpoint, err)
		}
		clients[name] = client
	}
	if len(clients) == 0 {
		return fmt.Errorf("no kusto endpoints provided")
	}
	lint := NewLinter()
	http.Handle("/alerts", lint.Handler())
	fakeaddr := fmt.Sprintf("http://localhost:%d", opts.Port)
	alertCli, err := alert.NewClient(time.Minute)

	kclient := multiKustoClient{clients: clients, MaxNotifications: opts.MaxNotifications}
	executor := engine.NewExecutor(engine.ExecutorOpts{
		AlertCli:    alertCli,
		AlertAddr:   fakeaddr,
		KustoClient: kclient,
		RuleStore:   ruleStore,
		Region:      opts.Region,
	})

	go func() {
		logger.Info("Listening at :%d", opts.Port)
		http.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(fmt.Sprintf(":%d", opts.Port), nil); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}()

	executor.RunOnce(ctx)
	lint.Log(log)
	return nil

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
	return multiKustoClient{clients: l.clients, MaxNotifications: l.opts.MaxNotifications}.Endpoint(db)
}

// make multikustoclient to share between linter and service
type multiKustoClient struct {
	clients          map[string]KustoClient
	MaxNotifications int
}

func (c multiKustoClient) Query(ctx context.Context, qc *engine.QueryContext, fn func(context.Context, string, *engine.QueryContext, *table.Row) error) error {
	client := c.clients[qc.Rule.Database]
	if client == nil {
		return fmt.Errorf("no client found for database=%s", qc.Rule.Database)
	}

	var iter *kusto.RowIterator
	var err error
	if qc.Rule.IsMgmtQuery {
		iter, err = client.Mgmt(ctx, qc.Rule.Database, qc.Stmt)
		if err != nil {
			return fmt.Errorf("failed to execute management kusto query=%s/%s: %w", qc.Rule.Namespace, qc.Rule.Name, err)
		}
	} else {
		iter, err = client.Query(ctx, qc.Rule.Database, qc.Stmt, kusto.ResultsProgressiveDisable())
		if err != nil {
			return fmt.Errorf("failed to execute kusto query=%s/%s: %w, %s", qc.Rule.Namespace, qc.Rule.Name, err, qc.Stmt)
		}
	}

	var n int
	defer iter.Stop()
	if err := iter.Do(func(row *table.Row) error {
		n++
		if n > c.MaxNotifications {
			metrics.NotificationUnhealthy.WithLabelValues(qc.Rule.Namespace, qc.Rule.Name).Set(1)
			return fmt.Errorf("%s/%s returned more than %d icm, throttling query", qc.Rule.Namespace, qc.Rule.Name, c.MaxNotifications)
		}

		return fn(ctx, client.Endpoint(), qc, row)
	}); err != nil {
		return err
	}

	// reset health metric since we didn't get any errors
	metrics.NotificationUnhealthy.WithLabelValues(qc.Rule.Namespace, qc.Rule.Name).Set(0)
	return nil
}

func (c multiKustoClient) Endpoint(db string) string {
	cl, ok := c.clients[db]
	if !ok {
		return "unknown"
	}
	return cl.Endpoint()
}

func (l *Alerter) Query(ctx context.Context, qc *engine.QueryContext, fn func(context.Context, string, *engine.QueryContext, *table.Row) error) error {
	return multiKustoClient{clients: l.clients, MaxNotifications: l.opts.MaxNotifications}.Query(ctx, qc, fn)
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
