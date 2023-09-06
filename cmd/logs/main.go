package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Azure/adx-mon/collector/logs"
	"github.com/Azure/adx-mon/collector/logs/journald"
	"github.com/Azure/adx-mon/collector/logs/transform"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func main() {
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-sc
		cancel()
		fmt.Println("Received signal, exiting...")
	}()

	k8sConfig := rest.Config{
		Host:            "https://172.30.0.1:443",
		BearerTokenFile: "/etc/td-agent-bit/token",
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true,
		},
	}
	client, err := kubernetes.NewForConfig(&k8sConfig)
	if err != nil {
		panic(err) // TODO
	}

	kubernetesTransform := transform.NewKubernetesTransform(client)
	testPlugin, err := transform.NewGoPlugin("goms.io/aks/logs/pkg/ccp", "ccp")
	if err != nil {
		panic(err) // TODO
	}

	dockerCollector := journald.NewJournaldCollector([]logs.Transformer{&journald.DockerMultiline{}, kubernetesTransform, testPlugin})
	err = dockerCollector.CollectLogs(ctx)
	fmt.Println(err)
}
