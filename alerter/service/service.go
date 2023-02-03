package service

import (
	"context"
	"fmt"
	"github.com/Azure/adx-mon/alert"
	"github.com/Azure/adx-mon/alerter/engine"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.goms.io/aks/azauth"
	"go.goms.io/aks/imds"
	"net/http"
	"os"
	"strings"
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

func New(opts *AlerterOpts) (*Alerter, error) {
	log, err := newLogger()
	if err != nil {
		return nil, fmt.Errorf("failed to construct logger: %w", err)
	}

	if err := rules.VerifyRules(opts.Region); err != nil {
		return nil, err
	}

	l2m := &Alerter{
		opts:    opts,
		log:     log,
		clients: make(map[string]KustoClient),
		queue:   make(chan struct{}, opts.Concurrency),
	}

	if opts.MSIID == "" && opts.MSIResource == "" {
		logger.Warn("No MSI ID or resource provided, using fake kusto clients")

		for name, endpoint := range opts.KustoEndpoints {
			l2m.clients[name] = fakeKustoClient{endpoint: endpoint}
			if err != nil {
				return nil, fmt.Errorf("kusto client=%s: %w", endpoint, err)
			}
		}
		fakeRule := &rules.Rule{
			DisplayName: "FakeRule",
			Database:    "FakeDB",
			Interval:    time.Minute,
			Query:       "Table | where foo == 'bar'",
			RoutingID:   "FakeRoutingID",
			TSG:         "FakeTSG",
		}
		l2m.clients[fakeRule.Database] = fakeKustoClient{endpoint: "http://fake.endpoint"}
		rules.Register(fakeRule)
	} else {
		var auth kusto.Authorization
		if opts.Dev {
			auth, err = devAuth(opts)
		} else {
			auth, err = prodAuth(opts)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to create authorizer: %w", err)
		}

		for name, endpoint := range opts.KustoEndpoints {
			l2m.clients[name], err = kusto.New(endpoint, auth)
			if err != nil {
				return nil, fmt.Errorf("kusto client=%s: %w", endpoint, err)
			}
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

	return l2m, nil
}

func (l *Alerter) Run() error {
	executor := &engine.Executor{
		AlertAddr: l.opts.AlertAddr,
		AlertCli:  l.alertCli,
	}

	log, err := newLogger()
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}
	logger.Info("Starting adx-mon alerter")

	go func() {
		logger.Info("Listening at :%d", l.opts.Port)
		http.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(fmt.Sprintf(":%d", l.opts.Port), nil); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}()

	return executor.Execute(context.Background(), l, log)
}

func prodAuth(opts *AlerterOpts) (kusto.Authorization, error) {
	msiID := opts.MSIID
	msiResource := opts.MSIResource
	if msiID == "" && msiResource == "" {
		return kusto.Authorization{}, errors.New("missing required parameter for MSI")
	}

	if msiResource != "" {
		cloud := opts.Cloud
		if strings.ToLower(cloud) == "azurecloud" {
			cloud = "AzurePublicCloud"
		}
		e := azauth.EnvironmentFromName(cloud)
		env, err := e.GetEnvironment()
		if err != nil {
			return kusto.Authorization{}, fmt.Errorf("failed to retrieve environment from cloud=%s: %w", cloud, err)
		}
		msiClient := imds.NewClient()
		id, err := msiClient.ResolveManagedIdentityID(env.ResourceManagerEndpoint, msiResource)
		if err != nil {
			return kusto.Authorization{}, fmt.Errorf("failed to resolve managed identity=%s: %w", env.ResourceManagerEndpoint, err)
		}
		msiID = id
	}

	logger.Info("Using MSI ID=%s", msiID)
	logger.Info("Using MSI Resource=%s", msiResource)

	cfg := auth.NewMSIConfig()
	cfg.ClientID = msiID
	cfg.Resource = msiResource
	authorizer := kusto.Authorization{
		Config: cfg,
	}
	return authorizer, nil
}

func (l *Alerter) Query(ctx context.Context, r rules.Rule, fn func(string, rules.Rule, *table.Row) error) error {
	client := l.clients[r.Database]
	if client == nil {
		return fmt.Errorf("no client found for database=%s", r.Database)
	}

	iter, err := client.Query(ctx, r.Database, r.Stmt, kusto.ResultsProgressiveDisable())
	if err != nil {
		return fmt.Errorf("failed to execute kusto query=%s: %w", r.DisplayName, err)
	}
	defer iter.Stop()
	return iter.Do(func(row *table.Row) error {
		return fn(client.Endpoint(), r, row)
	})
}

func devAuth(opts *AlerterOpts) (kusto.Authorization, error) {
	// To run queries locally in dev on your laptop, you'll need to create an access token for the Kusto
	// cluster you want to query via the AZ cli.  This uses your corp account to get an
	// access token to Kusto and then uses the token to auth to Kusto for you.
	//
	// Use microsoft.com creds to login
	// $ az login
	// Create an access token for the cluster you want to access
	// $ az account get-access-token --resource https://cluster.centralus.kusto.windows.net
	// Run it
	// $ alerter \
	// 		--dev \
	// 		--kusto-endpoint MyDB=https://cluster.centralus.kusto.windows.net
	// 		--region westeurope \

	var authz autorest.Authorizer
	for _, endpoint := range opts.KustoEndpoints {
		var err error
		authz, err = auth.NewAuthorizerFromCLIWithResource(endpoint)
		if err != nil {
			return kusto.Authorization{}, fmt.Errorf("failed to created authorized: %w", err)
		}
		break

	}

	authorizer := kusto.Authorization{
		Authorizer: authz,
	}
	return authorizer, nil
}

func newLogger() (logger.Logger, error) {
	return logger.NewLogger(), nil
}
