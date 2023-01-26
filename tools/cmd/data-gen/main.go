package main

import (
	"flag"
	"fmt"
	"github.com/Azure/adx-mon/prompb"
	"github.com/Azure/adx-mon/tools/data"
	"os"
	"time"
)

func main() {
	var (
		dataFile                string
		verbose                 bool
		batchSize, totalSamples int
		hosts, metrics          int
	)

	flag.BoolVar(&verbose, "verbose", false, "Verbose logging")
	flag.StringVar(&dataFile, "data-file", "", "Data file input created from TSBS suite")
	flag.IntVar(&batchSize, "batch-size", 2500, "Batch size of requests")
	flag.IntVar(&totalSamples, "total-samples", 100*1000*1000, "Total samples to generate")
	flag.IntVar(&hosts, "hosts", 1, "Total hosts (increases cardinality of metrics)")
	flag.IntVar(&metrics, "metrics", 1, "Total metrics")

	flag.Parse()

	fmt.Printf("Generating data set to: %s\n", dataFile)
	fmt.Printf("Total Samples: %d\n", totalSamples)
	fmt.Printf("Batch Size: %d\n", batchSize)

	f, err := data.NewFileWriter(dataFile)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	defer f.Close()

	ds := data.NewDataSet(data.SetOptions{
		Cardinality: hosts,
		NumMetrics:  metrics,
	})

	ts := time.Now().UTC()
	ts = ts.Add(time.Duration(-totalSamples/batchSize/60) * time.Minute)

	wr := &prompb.WriteRequest{}
	for i := 0; i < totalSamples; i++ {

		series := ds.Next(ts)
		wr.Timeseries = append(wr.Timeseries, series)

		if len(wr.Timeseries) == batchSize {
			if _, err := f.Write(wr); err != nil {
				fmt.Println(err.Error())
				return
			}
			wr.Timeseries = wr.Timeseries[:0]
			ts = ts.Add(time.Second)
		}
	}

	if len(wr.Timeseries) > 0 {
		if _, err := f.Write(wr); err != nil {
			fmt.Println(err.Error())
			return
		}
	}
}
