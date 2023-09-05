package journald

import (
	"context"

	"github.com/Azure/adx-mon/collector/logs"
)

// combines multiple lines from docker into a single log entry
type DockerMultiline struct {
	// TODO expire
	partialMessages map[string]string
}

func (d *DockerMultiline) Open(ctx context.Context) error {
	d.partialMessages = map[string]string{}
	return nil
}

func (d *DockerMultiline) Transform(ctx context.Context, batch *logs.LogBatch) ([]*logs.LogBatch, error) {
	// TODO use ctx
	for idx, log := range batch.Logs {
		partialID, ok := log.Attributes["CONTAINER_PARTIAL_ID"]
		if !ok { // not a partial log
			continue
		}

		// TODO handle partials that are out of order?
		// Seems unlikely on the same host
		priorMessage := d.partialMessages[partialID]
		message := priorMessage + log.Body["MESSAGE"].(string)

		isLast, ok := log.Attributes["CONTAINER_PARTIAL_LAST"]
		if ok && isLast == "true" { // last partial log
			delete(d.partialMessages, partialID)
			log.Body["MESSAGE"] = message
		} else {
			d.partialMessages[partialID] = message
			// Remove partial from the batch
			batch.Logs = append(batch.Logs[:idx], batch.Logs[idx:]...)
		}
	}
	batches := [1]*logs.LogBatch{batch}
	return batches[:], nil
}

func (d *DockerMultiline) Close() error {
	return nil
}
