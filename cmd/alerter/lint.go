package main

import (
	"context"
	"strings"

	"github.com/Azure/adx-mon/alerter"
	alertrulev1 "github.com/Azure/adx-mon/api/v1"
	"github.com/urfave/cli/v2"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

func NewLintCommand() *cli.Command {
	return &cli.Command{
		Name:    "lint",
		Aliases: []string{"l"},
		Usage:   "lint a directory by running each rule once",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{Name: "kusto-endpoint", Usage: "Kusto endpoint in the format of <name>=<endpoint>"},
			&cli.StringFlag{Name: "lint-dir", Usage: "Read alert rules from local filesystem", Required: true},
			&cli.StringFlag{Name: "auth-msi-id", Usage: "MSI client ID for authentication to Kusto"},
			&cli.StringFlag{Name: "auth-token", Usage: "Application token for authentication to Kusto"},
			&cli.IntFlag{Name: "max-notifications", Value: 25, Usage: "Maximum number of notifications to send per rule"},
			&cli.StringFlag{Name: "region", Usage: "Current region"},
		},
		Action: lintMain,
	}
}

func lintMain(ctx *cli.Context) error {
	endpoints := make(map[string]string)
	endpointsArg := ctx.StringSlice("kusto-endpoint")
	for _, v := range endpointsArg {
		parts := strings.Split(v, "=")
		if len(parts) != 2 {
			return cli.Exit("Invalid kusto-endpoint format, expected <name>=<endpoint>", 1)
		}
		endpoints[parts[0]] = parts[1]
	}

	scheme := clientgoscheme.Scheme
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return err
	}
	if err := alertrulev1.AddToScheme(scheme); err != nil {
		return err
	}

	opts := &alerter.AlerterOpts{
		KustoEndpoints:   endpoints,
		Port:             4023, // needs to be adjustable?Failed to create Notification
		Region:           ctx.String("region"),
		MaxNotifications: ctx.Int("max-notifications"),
		KustoToken:       ctx.String("auth-token"),
	}

	lintCtx, cancel := context.WithCancel(ctx.Context)
	defer cancel()
	// TODO fail early if azlogin is not up to date
	return alerter.Lint(lintCtx, opts, ctx.String("lint-dir"))
}
