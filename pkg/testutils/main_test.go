package testutils_test

import (
	"context"
	"os"
	"testing"

	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/Azure/adx-mon/pkg/testutils/collector"
	"github.com/Azure/adx-mon/pkg/testutils/ingestor"
	"github.com/Azure/adx-mon/pkg/testutils/kustainer"
	"github.com/Azure/adx-mon/pkg/testutils/sample"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
)

func TestMain(m *testing.M) {
	TestCluster.Open()

	code := m.Run()

	TestCluster.Close()
	os.Exit(code)
}

type Cluster struct {
	k8s       *k3s.K3sContainer
	kustainer *kustainer.KustainerContainer
	ingestor  *ingestor.IngestorContainer
	collector *collector.CollectorContainer
	sample    *sample.SampleContainer

	kubeconfig         string
	kustoConnectionUrl string
}

func (c *Cluster) Open() error {
	var (
		err error
		ctx = context.Background()
	)
	c.k8s, err = k3s.Run(ctx, "rancher/k3s:v1.31.2-k3s1")
	if err != nil {
		return err
	}

	dir, err := os.MkdirTemp("", ".kube")
	if err != nil {
		return err
	}
	c.kubeconfig, err = testutils.WriteKubeConfig(ctx, c.k8s, dir)
	if err != nil {
		return err
	}

	c.kustainer, err = kustainer.Run(ctx, "mcr.microsoft.com/azuredataexplorer/kustainer-linux:latest", kustainer.WithCluster(ctx, c.k8s))
	if err != nil {
		return err
	}
	restConfig, err := testutils.K8sRestConfig(ctx, c.k8s)
	if err != nil {
		return err
	}
	if err := c.kustainer.PortForward(ctx, restConfig); err != nil {
		return err
	}
	c.kustoConnectionUrl = c.kustainer.ConnectionUrl()

	for _, dbName := range []string{"Metrics", "Logs"} {
		if err := c.kustainer.CreateDatabase(ctx, dbName); err != nil {
			return err
		}
	}

	c.ingestor, err = ingestor.Run(ctx, ingestor.WithCluster(ctx, c.k8s))
	if err != nil {
		return err
	}

	c.collector, err = collector.Run(ctx, collector.WithCluster(ctx, c.k8s))
	if err != nil {
		return err
	}

	c.sample, err = sample.Run(ctx, sample.WithCluster(ctx, c.k8s))
	if err != nil {
		return err
	}

	return nil
}

func (c *Cluster) Close() {
	ctx := context.Background()
	c.k8s.Terminate(ctx)
	c.kustainer.Terminate(ctx)
	c.ingestor.Terminate(ctx)
	c.collector.Terminate(ctx)
	c.sample.Terminate(ctx)
}

func (c *Cluster) KubeConfigPath() string {
	return c.kubeconfig
}

func (c *Cluster) KustoConnectionURL() string {
	return c.kustoConnectionUrl
}

var TestCluster = &Cluster{}
