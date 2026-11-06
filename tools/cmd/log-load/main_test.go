package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestFiniteFileRun(t *testing.T) {
	for _, total := range []int64{1, 2, 3, 4, 5, 11, 2501} {
		t.Run(strconv.FormatInt(total, 10), func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "logs")
			opts := validOptions(directory)
			opts.totalSamples = total
			var output bytes.Buffer
			if err := run(context.Background(), opts, &output, io.Discard); err != nil {
				t.Fatal(err)
			}
			files, err := os.ReadDir(directory)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != opts.concurrency {
				t.Fatalf("got %d files, want %d", len(files), opts.concurrency)
			}
			var records, writtenBytes int64
			for i, file := range files {
				if file.Name() != "writer-"+strconv.Itoa(i)+".log" {
					t.Fatalf("unexpected filename: %s", file.Name())
				}
				content, err := os.ReadFile(writerPath(directory, i))
				if err != nil {
					t.Fatal(err)
				}
				writtenBytes += int64(len(content))
				if len(content) > 0 && content[len(content)-1] != '\n' {
					t.Fatal("missing final newline")
				}
				for line := range bytes.SplitSeq(content, []byte{'\n'}) {
					if len(line) == 0 {
						continue
					}
					var body map[string]any
					if err := json.Unmarshal(line, &body); err != nil {
						t.Fatalf("invalid JSON: %v\n%s", err, line)
					}
					if len(body) != 3 || body["message"] != "something worth logging" || body["kusto.database"] != opts.database || body["kusto.table"] != opts.table {
						t.Fatalf("incorrect body: %v", body)
					}
					records++
				}
			}
			if records != total {
				t.Fatalf("got %d records, want %d", records, total)
			}
			for _, line := range []string{
				"Generated records: " + strconv.FormatInt(total, 10) + "\n",
				"Written records: " + strconv.FormatInt(total, 10) + "\n",
				"Written bytes: " + strconv.FormatInt(writtenBytes, 10) + "\n",
				"Dry-run records: 0\n",
				"Written records/sec: ", "Written bytes/sec: ",
			} {
				if !strings.Contains(output.String(), line) {
					t.Fatalf("missing %q in stats:\n%s", line, &output)
				}
			}
		})
	}
}

func TestFileRunAppends(t *testing.T) {
	directory := t.TempDir()
	path := writerPath(directory, 0)
	original := []byte("{\"existing\":true}\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	opts := validOptions(directory)
	opts.concurrency = 1
	opts.totalSamples = 2
	for range 2 {
		if err := run(context.Background(), opts, io.Discard, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(content, original) || bytes.Count(content, []byte{'\n'}) != 5 {
		t.Fatalf("file was not appended: %s", content)
	}
}

func TestReadOnlyModesDoNotTouchFiles(t *testing.T) {
	for _, configMode := range []bool{false, true} {
		for _, existing := range []bool{false, true} {
			t.Run(strconv.FormatBool(configMode)+"/existing="+strconv.FormatBool(existing), func(t *testing.T) {
				directory := filepath.Join(t.TempDir(), "logs")
				var before os.FileInfo
				original := []byte("untouched\n")
				path := writerPath(directory, 0)
				if existing {
					if err := os.Mkdir(directory, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, original, 0600); err != nil {
						t.Fatal(err)
					}
					var err error
					before, err = os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
				}
				opts := validOptions(directory)
				opts.dryRun = !configMode
				opts.printCollectorConfig = configMode
				var output bytes.Buffer
				if err := run(context.Background(), opts, &output, io.Discard); err != nil {
					t.Fatal(err)
				}
				if !existing {
					if _, err := os.Stat(directory); !os.IsNotExist(err) {
						t.Fatalf("read-only mode created directory: %v", err)
					}
				} else {
					content, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					after, err := os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
					entries, err := os.ReadDir(directory)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(content, original) || !after.ModTime().Equal(before.ModTime()) || len(entries) != 1 {
						t.Fatal("read-only mode modified output files")
					}
				}
				if !configMode && (!strings.Contains(output.String(), "Generated records: 11\n") || !strings.Contains(output.String(), "Dry-run records: 11\n") ||
					!strings.Contains(output.String(), "Written records: 0\nWritten bytes: 0\n")) {
					t.Fatalf("incorrect dry-run stats:\n%s", &output)
				}
			})
		}
	}
}

func TestCLI(t *testing.T) {
	var output bytes.Buffer
	if err := runCLI(context.Background(), []string{
		"-target", fileURL(filepath.Join(t.TempDir(), "logs")), "-concurrency", "2",
		"-database", "DB", "-table", "Logs", "-collector-log-dir", "/load-logs", "-print-collector-config",
	}, &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "/load-logs/writer-1.log") || strings.Contains(output.String(), "Generated records:") {
		t.Fatalf("incorrect config-only CLI output: %s", &output)
	}
	for _, args := range [][]string{{"-unknown"}, {"unexpected"}, {"-total-samples", "-1"}} {
		if err := runCLI(context.Background(), args, io.Discard, io.Discard); err == nil {
			t.Fatalf("expected error for %v", args)
		}
	}
	if err := runCLI(context.Background(), []string{"-help"}, io.Discard, io.Discard); err != flag.ErrHelp {
		t.Fatalf("help error: %v", err)
	}
}
