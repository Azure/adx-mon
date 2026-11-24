package kustoutil

import (
	"fmt"
	"net/netip"
	"strings"

	trustedEndpoints "github.com/Azure/azure-kusto-go/azkustodata/trusted_endpoints"
	"k8s.io/apimachinery/pkg/util/validation"
)

// AddTrustedHosts adds exact host matches to the Kusto SDK's process-wide
// allowlist. Call it at startup, before creating or using Kusto clients.
func AddTrustedHosts(hosts []string) error {
	if len(hosts) == 0 {
		return nil
	}

	rules := make([]trustedEndpoints.MatchRule, 0, len(hosts))
	for _, value := range hosts {
		host := strings.ToLower(strings.TrimSpace(value))
		if addr, err := netip.ParseAddr(host); err != nil || addr.Zone() != "" {
			if len(validation.IsDNS1123Subdomain(host)) != 0 {
				return fmt.Errorf("invalid Kusto trusted host %q: expected a bare hostname or IP address without a scheme, port, or wildcard", value)
			}
		}
		rules = append(rules, trustedEndpoints.MatchRule{Suffix: host, Exact: true})
	}
	return trustedEndpoints.Instance.AddTrustedHosts(rules, false)
}
