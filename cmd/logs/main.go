package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Azure/adx-mon/collector/logs/journald"
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
	err := journald.CollectLogs(ctx)
	fmt.Println(err)
}
