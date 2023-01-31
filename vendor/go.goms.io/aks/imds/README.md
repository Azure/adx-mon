# AKS Instance Metadata Service ("IMDS") Client

# Getting Started

1. `go get go.goms.io/aks/imds`
2. Initialize a client

```go
imdsClient := imds.NewClient()
```

3. Get instance metadata

```go
package main

import (
    "fmt"
    
    "go.goms.io/aks/imds"
)

func main() {
    c := imds.NewClient()
    
    im, err := c.GetInstanceMetadata()
    if err != nil {
        panic(err)
    }

    // do stuff with instance metadata ("im")
    fmt.Println(im.Compute.VMName)
}
```

# Customize User-Agent, Timeouts etc.

```go
package main

import (
    "fmt"
    "time"
    
    "go.goms.io/aks/imds"
)

func main() {
	ua := imds.UserAgentConfig{
		Application: "aks-thingamajig",
	    Version:     "1.0",
	}

    // user agent will be set to "aks-imds/? (aks-thingamajig/1.0)
	c := imds.NewClient(imds.WithUserAgent(ua))

    // many other mechanisms to customize client
    c = imds.NewClient(
        imds.WithTimeout(10 * time.Second), // increase the time to allow for requests to succeed.
        imds.WithEndpoint("http://foobar"), // set a custom imds endpoint.
        imds.WithInstanceMetadataAPIVersion("2000-01-01"), // use a different API version.
    )
}
```

# Supported Features

| Feature           | API Call                                           |
| ----------------- | -------------------------------------------------- |
| Instance Metadata | `GetInstanceMetadata()`                            |
| Scheduled Events  | `GetScheduledEvents()`                             |
| Acknowledge Event | `AcknowledgeEvents(eventIDs ...string)`            |
| OAuth2 Token      | `GetToken(resource string, opts *GetTokenRequest)` |
| Attested Data     | Not Implemented (Unlikely)                         |

# IMDS Proxy

The `imds-proxy` is a small program that can be installed on an Azure VM. Requests to the IMDS can be routed through
the proxy. The IMDS proxy supports some modification of the responses, for example, replacing the `azEnvironment` value
on the `/metadata/instance` API.

# IMDS Faker

The `imds-faker` is a small program that can be installed anywhere and used as a fake IMDS. It works by serving files
from a content root matching the IMDS request path.

## Usage

1. Create a data directory where the IMDS faker will serve IMDS content from:
2. The structure of the data directory should mimic the IMDS URL path hierarchy, for example, to serve instance metadata
   your directory structure should look like this:
   
   ```text
   ${DataDir}/
    |-- metadata/
    |    |-- instance.json
   ```
   
3. Start the IMDS faker: `imds-faker -data-dir=${DataDir}`

## `iptables` configuration

The below rule will redirect 127.0.0.1 -> 169.254.169.254

`iptables -t nat -I OUTPUT -p tcp -d 169.254.169.254 --dport 80 -j DNAT --to-destination 127.0.0.1:80`
