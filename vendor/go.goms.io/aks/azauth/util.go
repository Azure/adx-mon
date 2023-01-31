package azauth

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Azure/go-autorest/autorest/azure"
	"go.goms.io/aks/imds"
)

func newIMDSClient(endpoint string) imds.Client {
	if endpoint == "" {
		return imds.NewClient()
	}

	if !strings.HasPrefix(endpoint, "http://") {
		endpoint = "http://" + endpoint
	}

	return imds.NewClient(imds.WithEndpoint(endpoint))
}

func azureEnvironmentToMap(env *azure.Environment) (map[string]interface{}, error) {
	b, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}

	m := make(map[string]interface{})

	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}

	return m, nil
}

func lookup(k string, m map[string]interface{}) (string, error) {
	raw, ok := m[k]
	if !ok {
		return "", fmt.Errorf("key %q not found", k)
	}

	v, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("value at %q is not a string", k)
	}

	return v, nil
}
