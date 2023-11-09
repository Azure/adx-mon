package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"

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

	stat, err := f.Stat()
	if err != nil {
		logger.Fatalf("stat file: %s", err)
	}

	var segments []*os.File
	if stat.IsDir() {
		files, err := f.Readdir(0)
		if err != nil {
			logger.Fatalf("readdir: %s", err)
		}

		for _, file := range files {
			if filepath.Ext(file.Name()) != ".wal" {
				continue
			}

			f, err := os.Open(filepath.Join(dataFile, file.Name()))
			if err != nil {
				logger.Fatalf("open file: %s", err)
			}
			segments = append(segments, f)
		}
	} else {
		segments = append(segments, f)
	}

	var (
		blocks int
		lines  int
	)

	defer func() {
		for _, f := range segments {
			f.Close()
		}
	}()

	for _, f := range segments {
		if len(segments) > 0 {
			println("Processing", f.Name())
		}

		iter, err := wal.NewSegmentIterator(f)
		if err != nil {
			logger.Fatalf("new segment iterator: %s: %s", f.Name(), err)
		}
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
	}

	if stats {
		println("Blocks:", blocks)
		println("Lines:", lines)
	}
}
