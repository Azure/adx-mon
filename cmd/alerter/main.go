package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/Azure/adx-mon/alerter/service"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func main() {
	cmd := &cobra.Command{
		Use:   "l2m",
		Short: "Emits metrics based on Kusto query results",
		RunE: func(_ *cobra.Command, _ []string) error {
			l2m, err := service.New()
			if err != nil {
				return err
			}
			return l2m.Run()
		},
	}

	viper.SetEnvPrefix("L2M")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	flags := cmd.Flags()
	flags.Bool("dev", false, "Run on a local dev machine without executing real queries")
	flags.Int("port", 4023, "Metrics port number")
	// Either the msi-id or msi-resource must be specified
	flags.String("msi-id", "", "MSI client ID")
	flags.String("msi-resource", "", "MSI resource ID")
	flags.String("cloud", "", "Azure cloud")
	flags.String("kusto-service-endpoint", "", "Kusto service endpoint")
	flags.String("kusto-infra-endpoint", "", "Kusto infra endpoint")
	flags.String("region", "", "Current region")
	flags.String("underlay", "", "Current underlay")
	flags.String("alert-address", "", "Address of the alert notification service")
	viper.BindPFlags(flags)
	viper.AutomaticEnv()

	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
