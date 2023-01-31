package service

import (
	"context"
	"fmt"
	"github.com/Azure/adx-mon/alert"
	"github.com/Azure/adx-mon/alerter/engine"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/data/table"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/viper"
	"go.goms.io/aks/azauth"
	"go.goms.io/aks/imds"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Azure/adx-mon/alerter/rules"
)

type Alerter struct {
	log      logger.Logger
	clients  map[string]*kusto.Client
	queue    chan struct{}
	alertCli *alert.Client
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

func New() (*Alerter, error) {
	log, err := newLogger()
	if err != nil {
		return nil, fmt.Errorf("failed to construct logger: %w", err)
	}

	if err := rules.VerifyRules(viper.GetString("region")); err != nil {
		return nil, err
	}

	var auth kusto.Authorization
	if viper.GetBool("dev") {
		auth, err = devAuth()
	} else {
		auth, err = prodAuth()
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create authorizer: %w", err)
	}

	l2m := &Alerter{
		log:     log,
		clients: make(map[string]*kusto.Client),
		queue:   make(chan struct{}, viper.GetInt("concurrency")),
	}
	infraEndpoint := viper.GetString("kusto-infra-endpoint")
	l2m.clients["AKSinfra"], err = kusto.New(infraEndpoint, auth)
	if err != nil {
		return nil, fmt.Errorf("kusto infra client=%s: %w", infraEndpoint, err)
	}
	serviceEndpoint := viper.GetString("kusto-service-endpoint")
	l2m.clients["AKSprod"], err = kusto.New(serviceEndpoint, auth)
	if err != nil {
		return nil, fmt.Errorf("kusto service client=%s: %w", serviceEndpoint, err)
	}
	customerEndpoint := viper.GetString("kusto-customer-endpoint")
	l2m.clients["AKSccplogs"], err = kusto.New(customerEndpoint, auth)
	if err != nil {
		return nil, fmt.Errorf("kusto customer client=%s: %w", customerEndpoint, err)
	}
	if viper.GetString("region") == "" {
		return nil, errors.New("missing or invalid region")
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
		AlertAddr: viper.GetString("alert-address"),
		AlertCli:  l.alertCli,
	}

	log, err := newLogger()
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}
	logger.Info("Starting log-to-metrics")

	go func() {
		logger.Info("Listening at :%d", viper.GetInt("port"))
		http.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(fmt.Sprintf(":%d", viper.GetInt("port")), nil); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}()

	return executor.Execute(context.Background(), l, log)
}

func prodAuth() (kusto.Authorization, error) {
	msiID := viper.GetString("msi-id")
	msiResource := viper.GetString("msi-resource")
	if msiID == "" && msiResource == "" {
		return kusto.Authorization{}, errors.New("missing required parameter for MSI")
	}

	if msiResource != "" {
		cloud := viper.GetString("cloud")
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
	client := l.clients["AKSprod"]
	if strings.Contains(r.Database, "infra") {
		client = l.clients["AKSinfra"]
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

func devAuth() (kusto.Authorization, error) {
	// To run queries locally in dev on your laptop, you'll need to create an access token for the Kusto
	// cluster you want to query via the AZ cli.  This uses your corp account to get an
	// access token to Kusto and then uses the token to auth to Kusto for you.
	//
	// Use microsoft.com creds to login
	// $ az login
	// Create an access token for the cluster you want to access
	// $ az account get-access-token --resource https://aksinfra.centralus.kusto.windows.net
	// Run it
	// $ alerter \
	// 		--dev \
	// 		--kusto-infra-endpoint https://aksinfra.centralus.kusto.windows.net \
	//		--kusto-service-endpoint  https://aks.centralus.kusto.windows.net \
	// 		--kusto-customer-endpoint https://aksccplogs.centralus.kusto.windows.net \
	// 		--region westeurope \
	//		--rule NameOfRuleToTest
	auth, err := auth.NewAuthorizerFromCLIWithResource(viper.GetString("kusto-infra-endpoint"))
	if err != nil {
		return kusto.Authorization{}, fmt.Errorf("failed to created authorized: %w", err)
	}
	authorizer := kusto.Authorization{
		Authorizer: auth,
	}
	return authorizer, nil
}

func newLogger() (logger.Logger, error) {
	return logger.NewLogger(), nil
}
