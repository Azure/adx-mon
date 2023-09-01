package k8s

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dgrijalva/jwt-go"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	tok := jwt.Token{
		Header: map[string]interface{}{
			"alg": "none",
			"kid": "zhcH9aFnMwehjYUJAqXz5vth_tyR6schfuh5fix3CG8",
		},
		Method: jwt.SigningMethodNone,
		Claims: jwt.MapClaims{
			"kubernetes.io": map[string]interface{}{
				"pod": map[string]interface{}{
					"name": "collector-2jhz9",
				},
				"namespace": "adx-mon",
			},
		},
	}

	tokenString, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(path, []byte(tokenString), 0644))

	id, err := LoadIdentity(path)
	require.NoError(t, err)
	require.Equal(t, id.Pod, "collector-2jhz9")
	require.Equal(t, id.Namespace, "adx-mon")
	hostname, err := os.Hostname()
	require.NoError(t, err)
	require.Equal(t, id.Container, hostname)
}
