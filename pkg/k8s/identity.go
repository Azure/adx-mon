package k8s

import (
	"os"
	"path/filepath"

	"github.com/golang-jwt/jwt/v5"
)

var (
	// Instance is the singleton instance of the identity.  This is goroutine safe.
	Instance Metadata
)

func init() {
	var err error
	Instance, err = LoadIdentity("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		panic(err)
	}
}

type Metadata struct {
	Namespace, Pod, Container string
}

// LoadIdentity loads the identity from the specified path.
func LoadIdentity(path string) (Metadata, error) {
	m := Metadata{
		Container: filepath.Base(os.Args[0]),
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return m, nil
	}

	tokenString, err := os.ReadFile(path)
	if err != nil {
		return Instance, err
	}

	p := &jwt.Parser{}
	tok, _, err := p.ParseUnverified(string(tokenString), jwt.MapClaims{})
	if err != nil {
		return Instance, err
	}

	claims := tok.Claims.(jwt.MapClaims)
	if v, ok := claims["kubernetes.io"]; ok {
		if vv, ok := v.(map[string]interface{}); ok {
			m.Namespace = vv["namespace"].(string)
			if vvv, ok := vv["pod"].(map[string]interface{}); ok {
				m.Pod = vvv["name"].(string)
			}
		}
	}

	return m, nil
}
