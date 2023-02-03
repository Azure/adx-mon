package main

import (
	"github.com/Azure/adx-mon/alerter/service"
	"github.com/Azure/adx-mon/logger"
	"github.com/urfave/cli/v2" // imports as package "cli"
	"os"
	"strings"
)

func main() {
	app := &cli.App{
		Name:  "alerter",
		Usage: "adx-mon alerting engine for ADX",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "dev", Usage: "Run on a local dev machine without executing real queries"},
			&cli.IntFlag{Name: "port", Value: 4023, Usage: "Metrics port number"},
			// Either the msi-id or msi-resource must be specified
			&cli.StringFlag{Name: "msi-id", Usage: "MSI client ID"},
			&cli.StringFlag{Name: "msi-resource", Usage: "MSI resource ID"},
			&cli.StringFlag{Name: "cloud", Usage: "Azure cloud"},
			&cli.StringFlag{Name: "region", Usage: "Current region"},
			&cli.StringSliceFlag{Name: "kusto-endpoint", Usage: "Kusto endpoint in the format of <name>=<endpoint>"},
			&cli.StringFlag{Name: "alerter-address", Usage: "Address of the alert notification service"},
			&cli.IntFlag{Name: "concurrency", Value: 10, Usage: "Number of concurrent queries to run"},
		},

		Action: func(ctx *cli.Context) error {
			return realMain(ctx)
		},
	}

	if err := app.Run(os.Args); err != nil {
		logger.Fatal(err.Error())
	}
}

func realMain(ctx *cli.Context) error {

	endpoints := make(map[string]string)
	endpointsArg := ctx.StringSlice("kusto-endpoint")
	for _, v := range endpointsArg {
		parts := strings.Split(v, "=")
		if len(parts) != 2 {
			return cli.Exit("Invalid kusto-endpoint format, expected <name>=<endpoint>", 1)
		}
		endpoints[parts[0]] = parts[1]
	}

	opts := &service.AlerterOpts{
		Dev:            ctx.Bool("dev"),
		Port:           ctx.Int("port"),
		KustoEndpoints: endpoints,
		Region:         ctx.String("region"),
		Cloud:          ctx.String("cloud"),
		AlertAddr:      ctx.String("alerter-address"),
		Concurrency:    ctx.Int("concurrency"),
		MSIID:          ctx.String("msi-id"),
		MSIResource:    ctx.String("msi-resource"),
	}

	svc, err := service.New(opts)
	if err != nil {
		return err
	}
	return svc.Run()
}
