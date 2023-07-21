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

	dockerCollector := journald.NewJournaldCollector([]logs.Transformer{&journald.DockerMultiline{}, &transform.ExampleTransform{}})
	err := dockerCollector.CollectLogs(ctx)
	fmt.Println(err)
}
