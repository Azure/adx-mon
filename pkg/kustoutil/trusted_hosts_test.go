package kustoutil

import (
	"testing"

	trustedEndpoints "github.com/Azure/azure-kusto-go/azkustodata/trusted_endpoints"
	"github.com/stretchr/testify/require"
)

func TestAddTrustedHosts(t *testing.T) {
	const authority = "https://login.microsoftonline.com"
	const emulator = "http://kustainer.default.svc.cluster.local:8080"
	validate := trustedEndpoints.Instance.ValidateTrustedEndpoint
	t.Cleanup(func() { require.NoError(t, trustedEndpoints.Instance.AddTrustedHosts(nil, true)) })

	// An unset flag does not change validation. Kustainer reports no login authority.
	require.NoError(t, AddTrustedHosts(nil))
	require.Error(t, validate(emulator, ""))
	require.NoError(t, validate("http://localhost:8080", ""))

	for _, host := range []string{"", "*.svc.cluster.local", ".svc.cluster.local", "http://example.test", "example.test:8080", "example.test/path", "fe80::1%eth0"} {
		// A bad value rejects the entire list instead of adding a partial rule set.
		require.Error(t, AddTrustedHosts([]string{"kustainer.default.svc.cluster.local", host}), "host %q", host)
		require.Error(t, validate(emulator, ""))
	}

	require.NoError(t, AddTrustedHosts([]string{"Kustainer.default.svc.cluster.local", "192.0.2.1", "2001:db8::1"}))
	require.NoError(t, AddTrustedHosts([]string{"another.example.test"}))
	require.NoError(t, validate(emulator, ""))
	require.NoError(t, validate("http://192.0.2.1:8080", ""))
	require.NoError(t, validate("http://[2001:db8::1]:8080", ""))
	require.NoError(t, validate("https://another.example.test", ""))
	require.NoError(t, validate("https://mycluster.kusto.windows.net", authority))
	require.Error(t, validate("http://other.default.svc.cluster.local:8080", ""))
	require.Error(t, validate("http://not-kustainer.default.svc.cluster.local:8080", ""))
	require.Error(t, validate("http://sub.kustainer.default.svc.cluster.local:8080", ""))
}
