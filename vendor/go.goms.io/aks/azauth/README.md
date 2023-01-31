# AKS Azure Auth Helper Library 

# Usage

The library offers a minimal amount of functionality to bootstrap authentication in the Azure Go SDK in a pleasant way. 
It is **NOT** intended to be a common catch-all location for utility code that uses the Azure Go SDK.

This library makes it trivial to get an `autorest.Authorizer` and `azure.Environment` that can be used to create Azure
Go SDK clients.

# Goals

1. Correct - Do the "right thing" when possible. 
2. Simple - Keep the API simple and do not include unnecessary indirection unless the indirection is opt-in.
3. Extensible - Keep the API open so the library does not make it hard to address unforeseen requirements. 

# Concepts

1. Loaders - Loaders get environment config or authentication credentials. There are two types of Loaders:
    1. EnvironmentLoader loads an azure.Environment from some source.
    2. AuthorizersLoader loads an Authorizers from some source.

2. Chains - A chain is an API construct that short-circuits execution while executing through a
            list of possibilities when one of the possibilities returns an acceptable result.

# Examples

See the [examples](examples/) directory.

# Customization

## Implement Custom EnvironmentLoader or AuthorizersLoader

Sometimes special logic is needed. It is easy to implement a custom `EnvironmentLoader` or `AuthorizersLoader` if
needed.

## Specifying Managed Identity when using MSI

A host machine can sometimes have multiple managed identities assigned to it. In this situation it is important to 
specify the managed identity ID to use otherwise the default (first) identity is used. Failure to assign the proper
identity ID can cause software to fail if it expects certain authorization to perform actions.

See the [Multiple User Assigned Identity example](examples/multiple_uai/main.go) for how to do this.

## Overriding Properties when Loading Environment from URL

The Azure SDK allows consumer's to override discrete values in an `azure.Environment` at load time when loading from a
URL. This behavior is preserved in the helper library as well:

```go
package main

import (
    "github.com/Azure/go-autorest/autorest/azure"

    "goms.io/aks/aks-azauth/azauth"
)
func main() {
    overrideName := azure.OverrideProperty{Key: azure.EnvironmentName, Value: "ExampleEnvironment"}
    overridePortal := azure.OverrideProperty{Key: azure.EnvironmentManagementPortalURL, Value: "http://example.org"}

    loader := azauth.EnvironmentFromURL(url, overrideName, overridePortal)
    [...]
}
```


















