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
	"github.com/Azure/adx-mon/alerter/multikustoclient"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/azure-kusto-go/kusto"
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

	// Managed Identity options
	MSIID       string
	MSIResource string

	// Application Token options
	KustoToken string

	CtrlCli client.Client
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
		queue:     make(chan struct{}, opts.Concurrency),
		CtrlCli:   opts.CtrlCli,
		ruleStore: ruleStore,
	}

	if opts.MSIID != "" {
		logger.Info("Using MSI ID=%s", opts.MSIID)
	}

	authConfigure, err := multikustoclient.GetAuth(multikustoclient.MsiAuth(opts.MSIID), multikustoclient.TokenAuth("https://kusto.kusto.windows.net", opts.KustoToken), multikustoclient.DefaultAuth())
	if err != nil {
		return nil, fmt.Errorf("failed to get auth: %w", err)
	}
	kclient, err := multikustoclient.New(opts.KustoEndpoints, authConfigure, opts.MaxNotifications)
	if err != nil {
		return nil, err
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
			KustoClient: kclient,
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

	lint := NewLinter()
	http.Handle("/alerts", lint.Handler())
	fakeaddr := fmt.Sprintf("http://localhost:%d", opts.Port)
	alertCli, err := alert.NewClient(time.Minute)
	if err != nil {
		return err
	}

	authConfigure, err := multikustoclient.GetAuth(multikustoclient.MsiAuth(opts.MSIID), multikustoclient.TokenAuth("https://kusto.kusto.windows.net", opts.KustoToken), multikustoclient.DefaultAuth())
	if err != nil {
		return fmt.Errorf("failed to get auth: %w", err)
	}

	kclient, err := multikustoclient.New(opts.KustoEndpoints, authConfigure, opts.MaxNotifications)
	if err != nil {
		return err
	}
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
	if lint.HasFailedQueries() {
		return fmt.Errorf("failed to lint rules")
	}
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
