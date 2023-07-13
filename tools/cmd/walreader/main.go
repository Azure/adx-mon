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

	return
	reader, err := wal.NewSegmentReader(dataFile)
	if err != nil {
		logger.Fatal("open file: %s", err)
	}

	defer reader.Close()

	b, err := io.ReadAll(reader)
	if err != nil {
		// logger.Fatal("read: %s", err)
	}

	lines := bytes.Split(b, []byte("\n"))
	for _, line := range lines {
		println(string(line))
	}

}
