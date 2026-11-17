package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestAlerterAddressFlag_OmittedRepeatAndCommas(t *testing.T) {
	t.Run("omitted flag leaves AlertAddrs empty", func(t *testing.T) {
		var got []string
		app := newApp(func(ctx *cli.Context) error {
			got = alertAddrsFromContext(ctx)
			require.NoError(t, validateAlertAddrs(got))
			return nil
		})

		err := app.Run([]string{"alerter"})
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("repeated flags preserve literal commas and query strings without splitting", func(t *testing.T) {
		var got []string
		app := newApp(func(ctx *cli.Context) error {
			got = alertAddrsFromContext(ctx)
			require.NoError(t, validateAlertAddrs(got))
			return nil
		})

		err := app.Run([]string{
			"alerter",
			"--alerter-address=https://receiver-a.example/team,prod?env=a,b&sig=1",
			"--alerter-address=http://receiver-b.example:8080/v1/alerts",
		})
		require.NoError(t, err)
		require.Equal(t, []string{
			"https://receiver-a.example/team,prod?env=a,b&sig=1",
			"http://receiver-b.example:8080/v1/alerts",
		}, got)
	})

	t.Run("explicitly empty flag fails startup", func(t *testing.T) {
		for _, emptyVal := range []string{"", "   "} {
			app := newApp(func(ctx *cli.Context) error {
				return validateAlertAddrs(alertAddrsFromContext(ctx))
			})
			err := app.Run([]string{"alerter", "--alerter-address=" + emptyVal})
			require.Error(t, err)
			require.Contains(t, err.Error(), "--alerter-address must not be empty")
		}
	})

	t.Run("invalid URL fails startup without leaking raw webhook URL or secrets", func(t *testing.T) {
		secretURL := "ftp://myuser:s3cr3tPass@hooks.example.com/services/T000/B111/SECRET_PATH?token=SECRET_QUERY"
		var stderr bytes.Buffer
		app := newApp(func(ctx *cli.Context) error {
			return validateAlertAddrs(alertAddrsFromContext(ctx))
		})
		app.ErrWriter = &stderr

		err := app.Run([]string{"alerter", "--alerter-address=" + secretURL})
		require.Error(t, err)
		for _, text := range []string{err.Error(), stderr.String()} {
			require.NotContains(t, text, secretURL)
			require.NotContains(t, text, "myuser")
			require.NotContains(t, text, "s3cr3tPass")
			require.NotContains(t, text, "SECRET_PATH")
			require.NotContains(t, text, "SECRET_QUERY")
		}
	})
}
