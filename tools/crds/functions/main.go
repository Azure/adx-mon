package main

import (
	"context"
	"flag"
	"os"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/ingestor/storage"
	"github.com/Azure/adx-mon/pkg/crd"
	"github.com/Azure/adx-mon/pkg/logger"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func main() {
	var kubeconfig string
	flag.StringVar(&kubeconfig, "kubeconfig", os.Getenv("KUBECONFIG"), "Path to a kubeconfig")
	flag.Parse()

	scheme := clientgoscheme.Scheme
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		logger.Fatalf("Failed to add client-go scheme: %v", err)
	}
	if err := v1.AddToScheme(scheme); err != nil {
		logger.Fatalf("Failed to add v1 scheme: %v", err)
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		logger.Fatalf("Failed to build kubeconfig=%s: %v", kubeconfig, err)
	}

	client, err := ctrlclient.New(config, ctrlclient.Options{})
	if err != nil {
		logger.Fatalf("Failed to create client: %v", err)
	}

	functionStorage := &storage.Functions{}
	opts := crd.Options{
		CtrlCli: client,
		List:    &v1.FunctionList{},
		Store:   functionStorage,
	}
	operator := crd.New(opts)
	if err := operator.Open(context.Background()); err != nil {
		logger.Fatalf("Failed to open CRD: %v", err)
	}
	defer operator.Close()

	for {
		functions := functionStorage.List()
		for _, function := range functions {
			logger.Infof("Found %s\n\tStatus %s", function.Spec.Body, function.Status.String())
		}

		<-time.After(time.Minute)
	}
}
