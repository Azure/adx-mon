/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/Azure/azure-kusto-go/kusto"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/Azure/adx-mon/alert"
	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/controllers"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

type options struct {
	metricsAddr          string
	probeAddr            string
	enableLeaderElection bool
	dev                  bool

	alertAddr string

	// Either the msi-id or msi-resource must be specified
	msiID       string
	msiResource string

	cloud          string
	region         string
	kustoEndpoint  string
	alerterAddress string
	concurrency    int
	port           int
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(adxmonv1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

// formatKustoEndpoints parses the kusto-endpoint flag and returns a map of name to endpoint.
// It expects the flag to be in the format of <name>=<endpoint>,<name>=<endpoint>
func formatKustoEndpoints(endpoints string) (map[string]string, error) {
	result := make(map[string]string)
	parts := strings.Split(endpoints, ",")
	for _, v := range parts {
		parts := strings.Split(v, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("Invalid kusto-endpoint format, expected <name>=<endpoint>")
		}
		result[parts[0]] = parts[1]
	}
	return result, nil
}

func main() {
	var opts options
	flag.StringVar(&opts.metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&opts.probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")

	// @todo rename to alert-address
	flag.IntVar(&opts.port, "port", 8082, "The port to host the fake-alerts server on.")

	flag.BoolVar(&opts.enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&opts.dev, "dev", false, "Enable development mode")
	flag.StringVar(&opts.msiID, "msi-id", "", "The MSI ID to use for authentication")
	flag.StringVar(&opts.msiResource, "msi-resource", "", "The MSI resource to use for authentication")
	flag.StringVar(&opts.cloud, "cloud", "AzurePublicCloud", "The cloud environment to use")
	flag.StringVar(&opts.region, "region", "eastus", "The region to use")
	flag.StringVar(&opts.kustoEndpoint, "kusto-endpoint", "", "The Kusto endpoint to use, in the format of <name>=<endpoint>,<name>=<endpoint>")
	flag.StringVar(&opts.alerterAddress, "alerter-address", "", "The address of the alerter service")
	flag.IntVar(&opts.concurrency, "concurrency", 1, "The number of concurrent workers to use")

	o := zap.Options{
		Development: true,
	}
	o.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&o)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     opts.metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: opts.probeAddr,
		LeaderElection:         opts.enableLeaderElection,
		LeaderElectionID:       "a1b4108e.azure.com",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if opts.alertAddr == "" {
		setupLog.Info("No alert address provided, using fake alert handler")
		opts.alertAddr = fmt.Sprintf("http://localhost:%d", opts.port)
		mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
			http.Handle("/alerts", fakeAlertHandler())
			setupLog.Info("Starting server", "path", "/alerts", "kind", "fake alert handler", "addr", fmt.Sprintf("[::]:%d", opts.port))

			// listen and serve, but if the context is canceled, stop the server, and return
			srv := &http.Server{Addr: fmt.Sprintf(":%d", opts.port)}
			go func() {
				if err := srv.ListenAndServe(); err != nil {
					setupLog.Error(err, "error from fake alert handler")
					return
				}
			}()

			<-ctx.Done()
			if err := srv.Shutdown(ctx); err != nil {
				setupLog.Error(err, "unable to shutdown fake alert handler")
				return err
			}

			return nil
		}))
	}

	if opts.msiID != "" {
		setupLog.Info(fmt.Sprintf("Using MSI ID=%s", opts.msiID))
	}

	endpoints, err := formatKustoEndpoints(opts.kustoEndpoint)
	if err != nil {
		setupLog.Error(err, "unable to parse kusto endpoints")
		os.Exit(1)
	}

	kustoClients := make(map[string]controllers.KustoClient)
	for name, endpoint := range endpoints {
		kcsb := kusto.NewConnectionStringBuilder(endpoint)
		if opts.msiID == "" {
			kcsb.WithAzCli()
		} else {
			kcsb.WithUserManagedIdentity(opts.msiID)
		}
		kustoClients[name], err = kusto.New(kcsb)
		if err != nil {
			setupLog.Error(err, fmt.Sprintf("unable to create kusto client for %s", name))
			os.Exit(1)
		}
	}

	alertCli, err := alert.NewClient(time.Minute)
	if err != nil {
		setupLog.Error(err, "unable to create alert client")
		os.Exit(1)
	}

	arr := &controllers.AlertRuleReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		Region:       opts.region,
		AlertCli:     alertCli,
		AlertAddr:    opts.alertAddr,
		KustoClients: kustoClients,
		Recorder:     mgr.GetEventRecorderFor("adx-mon-controller"),
	}
	arr.KustoClient = arr

	if err = arr.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AlertRule")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func fakeAlertHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			setupLog.Error(err, "failed to read request body")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		a := alert.Alert{}
		if err := json.Unmarshal(b, &a); err != nil {
			setupLog.Error(err, "failed to unmarshal alert")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		setupLog.Info("received alert", "alert", a)
		w.WriteHeader(http.StatusCreated)
	})
}
