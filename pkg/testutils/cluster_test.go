package testutils_test

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/pkg/testutils"
)

func TestRunCluster(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(10*time.Minute))
	defer cancel()

	testutils.RunCluster(t, ctx)
}

// ❌
// If Kustainer is ran in a container outside of k3s and I create a
// Service with NodePort in k3s so that Ingestor can connect to Kustainer,
// the random ports, which we can't control, often fall outside the
// valid range for NodePort, which is 30000-32767.

// ❌
// We can create a custom network and attach Kustainer to it, but k3s
// does not expose an option to attach itself to the network.

// ❌
// If we install Kustainer in k3s, we can create a Service for Kustainer
// and Ingestor can connect to it using the service name and expose a
// nodePort so we can connect to it to run queries. However, this
// does not work. Can't connect to it due to all the redirection...

// ?
// - Run Kustainer outside of k3s.
// - Run Ingestor outside of k3s but give it k3s kubeconfig so we can test the k8s surface area.
// - Point Ingestor at Kustainer.
