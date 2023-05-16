package adx

import (
	"context"
	"os"

	"github.com/Azure/adx-mon/pkg/logger"
)

// fakeUploader is an Uploader that does nothing.
type fakeUploader struct {
	queue   chan []string
	closeFn context.CancelFunc
}

func NewFakeUploader() Uploader {
	return &fakeUploader{
		queue: make(chan []string),
	}
}

func (f *fakeUploader) Open(ctx context.Context) error {
	ctx, f.closeFn = context.WithCancel(ctx)
	go f.upload(ctx)
	return nil
}

func (f *fakeUploader) Close() error {
	f.closeFn()
	return nil
}

func (f *fakeUploader) UploadQueue() chan []string {
	return f.queue
}

func (f *fakeUploader) upload(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case files := <-f.queue:
			for _, file := range files {
				logger.Warn("Uploading file %s", file)
				if err := os.RemoveAll(file); err != nil {
					logger.Error("Failed to remove file: %s", err.Error())
				}
			}
		}
	}
}
