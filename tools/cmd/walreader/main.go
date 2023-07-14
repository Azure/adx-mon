package main

import (
	"flag"
	"os"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/wal"
)

func main() {
	var (
		dataFile string
	)

	flag.StringVar(&dataFile, "data-file", "", "Data file input created from data-gen utility")
	flag.Parse()

	if dataFile == "" {
		logger.Fatal("data-file is required")
	}

	f, err := os.Open(dataFile)
	if err != nil {
		logger.Fatal("open file: %s", err)
	}
	defer f.Close()

	iter, err := wal.NewSegmentIterator(f)
	for {
		next, err := iter.Next()
		if err != nil {
			println(err.Error())
			return
		} else if !next {
			return
		}
		print(string(iter.Value()))
	}
}
