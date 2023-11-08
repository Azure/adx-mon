package main

import (
	"bytes"
	"flag"
	"io"
	"os"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/wal"
)

func main() {
	var (
		dataFile      string
		silent, stats bool
	)

	flag.StringVar(&dataFile, "data-file", "", "Data file input created from data-gen utility")
	flag.BoolVar(&silent, "silent", false, "Silent mode")
	flag.BoolVar(&stats, "stats", false, "Print stats")
	flag.Parse()

	if dataFile == "" {
		logger.Fatalf("data-file is required")
	}

	f, err := os.Open(dataFile)
	if err != nil {
		logger.Fatalf("open file: %s", err)
	}
	defer f.Close()

	var (
		blocks int
		lines  int
	)

	iter, err := wal.NewSegmentIterator(f)
	for {
		next, err := iter.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			println(err.Error())
			return
		} else if !next {
			break
		}
		blocks++
		lines += bytes.Count(iter.Value(), []byte("\n"))

		if !silent {
			print(string(iter.Value()))
		}
	}

	if stats {
		println("Blocks:", blocks)
		println("Lines:", lines)
	}
}
