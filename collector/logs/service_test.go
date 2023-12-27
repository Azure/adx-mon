package logs_test

import (
	"context"
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/logs"
	"github.com/Azure/adx-mon/collector/logs/sinks"
	"github.com/Azure/adx-mon/collector/logs/sources"
	"github.com/Azure/adx-mon/collector/logs/types"
)

func BenchmarkPipeline(b *testing.B) {
	for i := 0; i < b.N; i++ {
		sink := sinks.NewCountingSink(10000)
		source := sources.NewConstSource("test-val", 1*time.Second, 1000, types.WorkerCreator(nil, sink))

		service := &logs.Service{
			Source: source,
			Sink:   sink,
		}
		context := context.Background()

		service.Open(context)
		<-sink.DoneChan()
		service.Close()
	}
}

func TestPipeline(t *testing.T) {
	// Ensure we can send 10k logs through the pipeline.
	sink := sinks.NewCountingSink(10000)
	source := sources.NewConstSource("test-val", 1*time.Second, 1000, types.WorkerCreator(nil, sink))

	service := &logs.Service{
		Source: source,
		Sink:   sink,
	}
	context := context.Background()

	service.Open(context)
	<-sink.DoneChan()
	service.Close()
}
