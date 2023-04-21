package tls

import (
	"crypto/tls"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNewCACert(t *testing.T) {
	cert, key, err := NewFakeTLSCredentials()
	require.NoError(t, err)

	// Validate that the cert and key are valid
	_, err = tls.X509KeyPair(cert, key)
	require.NoError(t, err)
}
