package azauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/Azure/go-autorest/autorest/azure/cli"
)

const (
	adalAccessTokenVar = "AZURE_ADAL_ACCESS_TOKEN"
)

// DefaultAuthorizersChain returns a provider chain that will use the following chain:
//
// 1. Client Secret - env var AZURE_CLIENT_SECRET is not empty.
// 2. Managed Identity - Azure Instance Metadata Service is available.
// 3. CLI - CLI credentials.
func DefaultAuthorizersChain(opts ...ManagedIdentityLoaderOption) AuthorizersLoader {
	return AuthorizersChain(
		AuthorizersFromClientSecret(),
		AuthorizersFromManagedIdentity(opts...),
		AuthorizersFromCLI())
}

type AuthorizersLoader interface {
	IsValid() bool
	GetAuthorizers(env azure.Environment) (Authorizers, error)
}

func AuthorizersChain(al ...AuthorizersLoader) AuthorizersLoader {
	return &authorizersChain{loaders: al}
}

type authorizersChain struct {
	loaders []AuthorizersLoader
}

func (a authorizersChain) IsValid() bool {
	return true
}

func (a authorizersChain) GetAuthorizers(env azure.Environment) (Authorizers, error) {
	for _, loader := range a.loaders {
		loader := loader
		if !loader.IsValid() {
			continue
		}

		authorizers, err := loader.GetAuthorizers(env)
		if err != nil {
			return nil, fmt.Errorf("failed to create azure authorizers: %w", err)
		}

		return authorizers, nil
	}

	return nil, errors.New("failed to find an appropriate authorizer loader from the chain")
}

type AuthorizersProvider interface {
	IsValid() bool
	BuildAuthorizers(env azure.Environment) (Authorizers, error)
}

type ManagedIdentityLoaderOption func(m *ManagedIdentityLoader)

// ManagedIdentityID configures the ManagedIdentityLoader to use a specific managed identity if there are multiple. If
// this is not set then one of three things occur:
//
// 1. If there is only one managed identity then it is used by default.
//
// 2. If there are multiple identities and the AZURE_CLIENT_ID environment variable is set then the identity indicated
// by the AZURE_CLIENT_ID value is used.
//
// 3. If there are multiple identifies and the AZURE_CLIENT_ID environment variable is not set then the first identity
// is used.
//
// When this option is used the underlying ManagedIdentityLoader will set the AZURE_CLIENT_ID environment variable.
func ManagedIdentityID(v string) ManagedIdentityLoaderOption {
	return func(m *ManagedIdentityLoader) {
		m.IdentityID = v
	}
}

// ManagedIdentityResourceID configures the ManagedIdentityLoader to resolve a managed identity ID using the IMDS token
// retrieval API. The "resource" in this case can be one of the following things:
//
// 	1. empty in which case the resource is assumed to be the Azure Resource Manager.
// 	2. the name of a specific resource field in autorest azure.Environment (see the JSON field names)
// 	3. a specific resource URL
func ManagedIdentityResourceID(resource, resourceID string) ManagedIdentityLoaderOption {
	return func(m *ManagedIdentityLoader) {
		m.Resource = resource
		m.IdentityResourceID = resourceID
	}
}

// ManagedIdentityIDFromAzureJSON configures the ManagedIdentityLoader to use a specific managed identity as specified
// in an Azure JSON file. If this is not set then one of three things occur:
//
// 1. If there is only one managed identity then it is used by default.
//
// 2. If there are multiple identities and the AZURE_CLIENT_ID environment variable is set then the identity indicated
// by the AZURE_CLIENT_ID value is used.
//
// 3. If there are multiple identifies and the AZURE_CLIENT_ID environment variable is not set then the first identity
// is used.
//
// When this option is used the underlying ManagedIdentityLoader will set the AZURE_CLIENT_ID environment variable.
func ManagedIdentityIDFromAzureJSON(path string) ManagedIdentityLoaderOption {
	return func(m *ManagedIdentityLoader) {
		m.AzureJSONFile = path
	}
}

// ManagedIdentityIMDSAvailabilityTimeout configures how long the ManagedIdentityLoader should wait to determine if the
// metadata service is available. This only controls the connect timeout, if the loader can connect to the IMDS then it
// is assumed IMDS is available.
func ManagedIdentityIMDSAvailabilityTimeout(v time.Duration) ManagedIdentityLoaderOption {
	return func(m *ManagedIdentityLoader) {
		m.Timeout = v
	}
}

// AuthorizersFromManagedIdentity creates a new Authorizers instance that delegates to the Azure Instance Metadata
// Service ("IMDS") in order to use a Managed identity.
type ManagedIdentityLoader struct {
	// AzureJSONFile is the path to an azure.json file with a userAssignedIdentityID field. If this value is set then
	// the file is read, and the IdentityID value is set. If the IdentityID value is set as well as this field then
	// the IdentityID field is overwritten with the value read from the azure.json file.
	AzureJSONFile string

	// IdentityID is the UUID of the managed identity to authenticate with. If this value is set then the
	// AZURE_CLIENT_ID environment variable will be set if it is not already set. Leave this value empty if you want
	// to default to primary managed identity on the system.
	IdentityID string

	// Resource is the name / ID of the resource that a managed identity resource ID is being resolved for. This is
	// only used when IdentityResourceID is not empty.
	Resource string

	// IdentityResourceID is the Azure Resource ID of the managed identity to authenticate with. If this value is set
	// then the identity will be resolved through a special IMDS API request.
	IdentityResourceID string

	// IMDSHost allows a custom IMDS Host to be used. If not value is provided then the Azure default is used.
	IMDSHost string

	// Timeout controls how long to wait before deciding the IMDS is not available.
	Timeout time.Duration
}

func (l *ManagedIdentityLoader) IsValid() bool {
	if l.Timeout == 0 {
		l.Timeout = 2 * time.Second
	}

	c := newIMDSClient(l.IMDSHost)

	res, err := c.IsAvailable(l.Timeout)
	if err != nil {
		return false
	}

	if l.AzureJSONFile != "" {
		// only valid if azure.json can be accessed.
		_, err = ioutil.ReadFile(l.AzureJSONFile)
		if err != nil {
			return false
		}
	}

	return res
}

func (l *ManagedIdentityLoader) GetAuthorizers(env azure.Environment) (Authorizers, error) {
	if l.AzureJSONFile != "" {
		data, err := ioutil.ReadFile(l.AzureJSONFile)
		if err != nil {
			return nil, err
		}

		azureJSON := struct {
			UserAssignedIdentityID string `json:"userAssignedIdentityID"`
		}{}

		if err := json.Unmarshal(data, &azureJSON); err != nil {
			return nil, err
		}

		l.IdentityID = azureJSON.UserAssignedIdentityID
	}

	if l.IdentityResourceID != "" {
		resource := l.Resource

		// fall through this chain of lookups...
		// 1. resource is empty then assume they want ARM
		// 2. resource is contained in azure.Environment data then use that value
		// 3. resource is
		if resource == "" {
			resource = env.ResourceManagerEndpoint
		}

		azm, err := azureEnvironmentToMap(&env)
		if _, ok := azm[resource]; ok {
			resource, err = lookup(resource, azm)
			if err != nil {
				return nil, err
			}
		}

		c := newIMDSClient(l.IMDSHost)
		id, err := c.ResolveManagedIdentityID(resource, l.IdentityResourceID)
		if err != nil {
			return nil, err
		}

		l.IdentityID = id
	}

	if l.IdentityID != "" {
		if err := os.Setenv(auth.ClientID, l.IdentityID); err != nil {
			return nil, fmt.Errorf("failed to set %s environment variable: %w", auth.ClientID, err)
		}
	}

	return newAuthorizersFromEnvironment(env)
}

type cliLoader struct{}

func (a cliLoader) IsValid() bool {
	return true
}

func (a cliLoader) GetAuthorizers(env azure.Environment) (Authorizers, error) {
	return newAuthorizersFromCLI(env)
}

type adalAccessTokenLoader struct{}

func (a adalAccessTokenLoader) IsValid() bool {
	return os.Getenv(adalAccessTokenVar) != ""
}

func (a adalAccessTokenLoader) GetAuthorizers(env azure.Environment) (Authorizers, error) {
	return newAuthorizersFromADALAccessToken(env)
}

type clientCertificateLoader struct{}

func (p clientCertificateLoader) IsValid() bool {
	return os.Getenv(auth.CertificatePath) != "" && os.Getenv(auth.CertificatePassword) != ""
}

func (p clientCertificateLoader) GetAuthorizers(env azure.Environment) (Authorizers, error) {
	return newAuthorizersFromFile(env)
}

type clientSecretLoader struct{}

func (p clientSecretLoader) IsValid() bool {
	return os.Getenv(auth.ClientSecret) != ""
}

func (p clientSecretLoader) GetAuthorizers(env azure.Environment) (Authorizers, error) {
	return newAuthorizersFromEnvironment(env)
}

type legacyAzureJSON struct {
	AzureClientID     string `json:"aadClientId"`
	AzureClientSecret string `json:"aadClientSecret"`
	AzureTenantID     string `json:"tenantId"`
	Cloud             string `json:"cloud"`
}

// azureJSONClientSecretLoader loads Azure API client credentials from an azure.json file. The keys it searches for
// are AZURE_CLIENT_ID and AZURE_CLIENT_SECRET.
type azureJSONClientSecretLoader struct {
	azureJSONFile string
}

func (l azureJSONClientSecretLoader) IsValid() bool {
	// only valid if azure.json can be accessed.
	_, err := os.Stat(l.azureJSONFile)
	if err != nil {
		return false
	}

	data, err := ioutil.ReadFile(l.azureJSONFile)
	if err != nil {
		return false
	}

	azureJSON := legacyAzureJSON{}

	if err := json.Unmarshal(data, &azureJSON); err != nil {
		return false
	}

	if azureJSON.AzureClientID == "" || azureJSON.AzureClientSecret == "" {
		return false
	}

	return true
}

func (l azureJSONClientSecretLoader) GetAuthorizers(env azure.Environment) (Authorizers, error) {
	data, err := ioutil.ReadFile(l.azureJSONFile)
	if err != nil {
		return nil, err
	}

	azureJSON := legacyAzureJSON{}

	if err := json.Unmarshal(data, &azureJSON); err != nil {
		return nil, err
	}

	if azureJSON.AzureClientID == "" || azureJSON.AzureClientSecret == "" {
		return nil, fmt.Errorf(
			`file %s has empty or missing values for "aadClientId" and "aadClientSecret"`, l.azureJSONFile)
	}

	// set these so newAuthorizersFromEnvironment works
	_ = os.Setenv(auth.ClientID, azureJSON.AzureClientID)
	_ = os.Setenv(auth.ClientSecret, azureJSON.AzureClientSecret)
	_ = os.Setenv(auth.TenantID, azureJSON.AzureTenantID)
	_ = os.Setenv(auth.EnvironmentName, azureJSON.Cloud)

	return newAuthorizersFromEnvironment(env)
}

type usernameAndPasswordLoader struct{}

func (l usernameAndPasswordLoader) IsValid() bool {
	return os.Getenv(auth.Username) != "" && os.Getenv(auth.Password) != ""
}

func (l usernameAndPasswordLoader) GetAuthorizers(env azure.Environment) (Authorizers, error) {
	return newAuthorizersFromEnvironment(env)
}

// AuthorizersFromAzureJSONClientSecret returns Authorizers configured by reading an AZURE_CLIENT_ID and
// AZURE_CLIENT_SECRET value from an azure.json file.
func AuthorizersFromAzureJSON(path string) AuthorizersLoader {
	return &azureJSONClientSecretLoader{azureJSONFile: path}
}

// AuthorizersFromCLI returns Authorizers configured by the Azure CLI. This is often OK for quick and dirty tools but
// requires the Azure CLI is configured and setup on the host machine. It also has some other limitations such as
// quirky authentication and token refresh behavior.
func AuthorizersFromCLI() AuthorizersLoader {
	return &cliLoader{}
}

// AuthorizersFromClientSecret returns an AuthorizersLoader that creates an Authorizer if the AZURE_CLIENT_SECRET
// environment variable is not empty.
func AuthorizersFromClientSecret() AuthorizersLoader {
	return &clientSecretLoader{}
}

// AuthorizersFromClientCertificate returns an AuthorizersLoader that creates an Authorizer if the
// AZURE_CERTIFICATE_PATH and AZURE_CERTIFICATE_PASSWORD environment variables are not empty.
func AuthorizersFromClientCertificate() AuthorizersLoader {
	return &clientCertificateLoader{}
}

// AuthorizersFromManagedIdentity returns an AuthorizersLoader that creates an Authorizers if the Azure Instance
// Metadata Service ("IMDS") is available. Use vararg options to change options on the underlying loader.
func AuthorizersFromManagedIdentity(opts ...ManagedIdentityLoaderOption) AuthorizersLoader {
	loader := &ManagedIdentityLoader{}

	for _, opt := range opts {
		opt(loader)
	}

	return loader
}

// AuthorizersFromUsernameAndPassword returns an AuthorizersLoader that creates an Authorizer if the AZURE_USERNAME
// and AZURE_PASSWORD environment variables are set.
func AuthorizersFromUsernameAndPassword() AuthorizersLoader {
	return &usernameAndPasswordLoader{}
}

// AuthorizersFromADALAccessToken returns Authorizers based on an access token for a single resource type.
// You can use az account get-access-token --resource <resource> to get the token and then set the
// AZURE_ADAL_ACCESS_TOKEN environment variable before using this authorizer
func AuthorizersFromADALAccessToken() AuthorizersLoader {
	return &adalAccessTokenLoader{}
}

type Authorizers interface {
	// ARM returns an Authorizer that is configured to work with the Azure Resource Manager (ARM).
	ARM() autorest.Authorizer

	// Graph returns an Authorizer that is configured to work the Azure Graph services.
	Graph() autorest.Authorizer

	// KeyVault returns an Authorizer that is configured to work with the Azure KeyVault service.
	KeyVault() autorest.Authorizer
}

type authorizers struct {
	armAuthorizer      autorest.Authorizer
	graphAuthorizer    autorest.Authorizer
	keyVaultAuthorizer autorest.Authorizer
}

func (a *authorizers) ARM() autorest.Authorizer {
	return a.armAuthorizer
}

func (a *authorizers) Graph() autorest.Authorizer {
	return a.graphAuthorizer
}

func (a *authorizers) KeyVault() autorest.Authorizer {
	return a.keyVaultAuthorizer
}

func Setup(el EnvironmentLoader, al AuthorizersLoader) (azure.Environment, Authorizers, error) {
	var (
		env         azure.Environment
		authorizers Authorizers
		err         error
	)

	env, err = el.GetEnvironment()
	if err != nil {
		return env, nil, fmt.Errorf("failed to read azure environment config: %w", err)
	}

	authorizers, err = al.GetAuthorizers(env)
	if err != nil {
		return env, nil, fmt.Errorf("failed to create azure authorizers: %w", err)
	}

	return env, authorizers, nil
}

// cleanKeyVaultEndpoint removes the trailing slash from a Key Vault endpoint URL. If the slash is not removed then the
// authorizer will not work when API calls are made.
func cleanKeyVaultEndpoint(s string) string {
	for strings.HasSuffix(s, "/") {
		s = strings.TrimSuffix(s, "/")
	}

	return s
}

func newAuthorizersFromEnvironment(e azure.Environment) (Authorizers, error) {
	var (
		err         error
		authorizers authorizers
	)

	kvEndpoint := cleanKeyVaultEndpoint(e.KeyVaultEndpoint)
	if authorizers.keyVaultAuthorizer, err = auth.NewAuthorizerFromEnvironmentWithResource(kvEndpoint); err != nil {
		return nil, err
	}

	if authorizers.graphAuthorizer, err = auth.NewAuthorizerFromEnvironmentWithResource(e.GraphEndpoint); err != nil {
		return nil, err
	}

	if authorizers.armAuthorizer, err = auth.NewAuthorizerFromEnvironmentWithResource(e.TokenAudience); err != nil {
		return nil, err
	}

	return &authorizers, nil
}

func newAuthorizersFromFile(e azure.Environment) (Authorizers, error) {
	var (
		err         error
		authorizers authorizers
	)

	kvEndpoint := cleanKeyVaultEndpoint(e.KeyVaultEndpoint)
	if authorizers.keyVaultAuthorizer, err = auth.NewAuthorizerFromFileWithResource(kvEndpoint); err != nil {
		return nil, err
	}

	if authorizers.graphAuthorizer, err = auth.NewAuthorizerFromFileWithResource(e.GraphEndpoint); err != nil {
		return nil, err
	}

	if authorizers.armAuthorizer, err = auth.NewAuthorizerFromFile(e.ResourceManagerEndpoint); err != nil {
		return nil, err
	}

	return &authorizers, nil
}

func newAuthorizersFromCLI(e azure.Environment) (Authorizers, error) {
	var (
		err         error
		authorizers authorizers
	)

	kvEndpoint := cleanKeyVaultEndpoint(e.KeyVaultEndpoint)
	if authorizers.keyVaultAuthorizer, err = auth.NewAuthorizerFromCLIWithResource(kvEndpoint); err != nil {
		return nil, err
	}

	if authorizers.graphAuthorizer, err = auth.NewAuthorizerFromCLIWithResource(e.GraphEndpoint); err != nil {
		return nil, err
	}

	if authorizers.armAuthorizer, err = auth.NewAuthorizerFromCLIWithResource(e.ResourceManagerEndpoint); err != nil {
		return nil, err
	}

	return &authorizers, nil
}

func newAuthorizersFromADALAccessToken(e azure.Environment) (Authorizers, error) {
	var (
		authorizers authorizers
	)

	token := os.Getenv(adalAccessTokenVar)

	tokenResponse := cli.Token{}
	err := json.Unmarshal([]byte(token), &tokenResponse)
	if err != nil {
		return nil, fmt.Errorf("can't unmarshal the provided token: %w", err)
	}

	adalToken, err := tokenResponse.ToADALToken()
	if err != nil {
		return nil, fmt.Errorf("can't convert the token to ADAL format: %w", err)
	}

	authz, err := autorest.NewBearerAuthorizer(&adalToken), nil
	if err != nil {
		return nil, fmt.Errorf("can't create the brearer authorizer: %w", err)
	}

	authorizers.armAuthorizer = authz
	authorizers.keyVaultAuthorizer = authz
	authorizers.armAuthorizer = authz

	return &authorizers, nil
}
