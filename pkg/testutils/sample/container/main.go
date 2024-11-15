package main

import (
	"context"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
)

func main() {
	jhandler := slog.NewJSONHandler(os.Stdout, nil)
	jlog := slog.New(jhandler)
	jlog = jlog.WithGroup("handler").With(slog.String("encoding", "json"))

	exporter, err := prometheus.New(
		prometheus.WithNamespace("sample"),
		prometheus.WithoutScopeInfo(),
	)
	if err != nil {
		jlog.Error("failed to create prometheus exporter", "error", err)
		os.Exit(1)
	}

	provider := metric.NewMeterProvider(metric.WithReader(exporter))
	meter := provider.Meter("github.com/Azure/adx-mon/pkg/testutils/sample/container")
	c, err := meter.Int64Counter("iteration")
	if err != nil {
		jlog.Error("failed to create counter", "error", err)
		os.Exit(1)
	}

	metricsServer := &http.Server{Addr: ":8080"}
	http.Handle("/metrics", promhttp.Handler())

	go func() {
		if err := metricsServer.ListenAndServe(); err != nil {
			jlog.Error("failed to start metrics server", "error", err)
			os.Exit(1)
		}
	}()

	jlog.Info("Begin")
	for {
		<-time.After(time.Second)

		randomNumber := rand.Intn(3) + 1 // Generate a random number between 1 and 3
		switch randomNumber {
		case 1:
			jlog.Info("All is well", "number", randomNumber)
		case 2:
			jlog.Warn("Something is wrong", "number", randomNumber)
		case 3:
			jlog.Error("Something is very wrong", "number", randomNumber)
		}

		// Increment the counter
		c.Add(context.Background(), 1)
	}
}
