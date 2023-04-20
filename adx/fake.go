package adx

import (
	"context"
	"github.com/Azure/adx-mon/logger"
	"os"
)

// fakeUploader is an Uploader that does nothing.
type fakeUploader struct {
	queue chan []string
	close context.CancelFunc
}

func NewFakeUploader() Uploader {
	return &fakeUploader{
		queue: make(chan []string),
	}
}

func (f *fakeUploader) Open() error {
	ctx, cancel := context.WithCancel(context.Background())
	f.close = cancel
	go f.upload(ctx)
	return nil
}

func (f *fakeUploader) Close() error {
	f.close()
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
