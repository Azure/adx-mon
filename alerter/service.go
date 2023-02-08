package alerter

import (
	"context"
	"fmt"
	"github.com/Azure/adx-mon/alert"
	"github.com/Azure/adx-mon/alerter/engine"
	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"net/http"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sync"
	"time"

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
}

type Alerter struct {
	log      logger.Logger
	clients  map[string]KustoClient
	queue    chan struct{}
	alertCli *alert.Client
	opts     *AlerterOpts

	wg       sync.WaitGroup
	executor *engine.Executor
	ctx      context.Context
	closeFn  context.CancelFunc
}

type KustoClient interface {
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

	// Setup k8s client
	// @todo figure out how to mock this
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = alertrulev1.AddToScheme(scheme)
	kubeconfig := ctrl.GetConfigOrDie()
	kubeclient, err := client.New(kubeconfig, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}

	if err := rules.VerifyRules(kubeclient, opts.Region); err != nil {
		return nil, err
	}

	l2m := &Alerter{
		opts:    opts,
		log:     log,
		clients: make(map[string]KustoClient),
		queue:   make(chan struct{}, opts.Concurrency),
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

	if kubeclient == nil {
		logger.Warn("No kusto endpoints provided, using fake kusto clients")
		fakeRule := &rules.Rule{
			Namespace: "fake-namespace",
			Name:      "FakeRule",
			Database:  "FakeDB",
			Interval:  time.Minute,
			Query:     `UnderlayNodeInfo | where Region == ParamRegion | limit 1 | project Title="test"`,
		}
		l2m.clients[fakeRule.Database] = fakeKustoClient{endpoint: "http://fake.endpoint"}
		rules.Register(fakeRule)

		if err := rules.VerifyRules(nil, opts.Region); err != nil {
			return nil, err
		}
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
	}
	l2m.executor = executor
	return l2m, nil
}

func (l *Alerter) Open(ctx context.Context) error {
	l.ctx, l.closeFn = context.WithCancel(ctx)

	logger.Info("Starting adx-mon alerter")

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

func (l *Alerter) Query(ctx context.Context, r rules.Rule, fn func(string, rules.Rule, *table.Row) error) error {
	client := l.clients[r.Database]
	if client == nil {
		return fmt.Errorf("no client found for database=%s", r.Database)
	}

	iter, err := client.Query(ctx, r.Database, r.Stmt, kusto.ResultsProgressiveDisable())
	if err != nil {
		return fmt.Errorf("failed to execute kusto query=%s/%s: %s", r.Namespace, r.Name, err)
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
	return l.executor.Close()
}
