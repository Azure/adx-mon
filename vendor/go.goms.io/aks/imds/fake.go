package imds

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

const (
	imdsErrNotFound = `{"error": "Not found."}`
	imdsErrInternal = `{"error": "Internal error."}`
)

type FakeOptions struct {
	DataRoot string
	Host     string
	Port     int
}

type FakeServer interface {
	ListenAndServe() error
}

func NewFakeServer(opts FakeOptions) FakeServer {
	if opts.Host == "" {
		opts.Host = "127.0.0.1"
	}

	p := &fakeServer{
		FakeOptions: opts,
	}

	return p
}

type fakeServer struct {
	FakeOptions
}

func (f *fakeServer) handle(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	format := r.URL.Query().Get("format")

	if format == "" {
		format = "json"
	}

	path = fmt.Sprintf("%s.%s", path, format)

	data, err := ioutil.ReadFile(filepath.Join(f.DataRoot, path))
	if err != nil {
		if os.IsNotExist(err) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(imdsErrNotFound))

			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(imdsErrInternal))

		return
	}

	_, _ = w.Write(data)
}

func (f *fakeServer) ListenAndServe() error {
	http.HandleFunc("/", f.handle)

	listenAddr := fmt.Sprintf("%s:%d", f.Host, f.Port)
	log.Printf("Starting imds-faker: http://%s", listenAddr)

	return http.ListenAndServe(listenAddr, nil)
}
