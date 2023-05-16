package multikustoclient

import (
	"fmt"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-kusto-go/kusto"
)

// AuthConfiguror is a function that can be used to configure a kusto connection's authentication method
type authConfiguror func(*kusto.ConnectionStringBuilder) *kusto.ConnectionStringBuilder

type authMethod func() (authConfiguror, error)

// DefaultAuth uses standard AZURE_ environment variables and falls back to az cli if they are not set
func DefaultAuth() authMethod {
	return func() (authConfiguror, error) {
		logger.Info("Using default authentication")
		return func(kcsb *kusto.ConnectionStringBuilder) *kusto.ConnectionStringBuilder {
			return kcsb.WithDefaultAzureCredential()
		}, nil
	}
}

// MsiAuth uses the provided Managed Service Identity to authenticate to kusto
func MsiAuth(msi string) authMethod {
	return func() (authConfiguror, error) {
		if msi == "" {
			return nil, fmt.Errorf("msi cannot be empty")
		}
		logger.Info("Using MSI authentication")
		return func(kcsb *kusto.ConnectionStringBuilder) *kusto.ConnectionStringBuilder {
			return kcsb.WithUserManagedIdentity(msi)
		}, nil
	}
}

// TokenAuth uses the provided application id and token to authenticate to kusto
func TokenAuth(kustoAppId string, kustoToken string) authMethod {
	return func() (authConfiguror, error) {
		if kustoAppId == "" {
			return nil, fmt.Errorf("appId cannot be empty")
		}
		if kustoToken == "" {
			return nil, fmt.Errorf("token cannot be empty")
		}
		logger.Info("Using token authentication")
		return func(kcsb *kusto.ConnectionStringBuilder) *kusto.ConnectionStringBuilder {
			return kcsb.WithApplicationToken(kustoAppId, kustoToken)
		}, nil
	}
}

// GetAuth returns the first valid authConfiguror from the provided list of authMethods
func GetAuth(methods ...authMethod) (authConfiguror, error) {
	for _, method := range methods {
		auth, err := method()
		if err != nil {
			continue
		}
		return auth, nil
	}
	return nil, fmt.Errorf("no valid auth method found")
}
