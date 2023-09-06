package journald

import (
	"context"
	"encoding/json"

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

func (d *DockerMultiline) Transform(ctx context.Context, batch *logs.LogBatch) (*logs.LogBatch, error) {
	// TODO use ctx
	for idx, log := range batch.Logs {
		partialIDVal, ok := log.Attributes["CONTAINER_PARTIAL_ID"]
		if !ok { // not a partial log
			d.parseMessage(log)
			continue
		}
		partialID := partialIDVal.(string)

		// TODO handle partials that are out of order?
		// Seems unlikely on the same host
		priorMessage := d.partialMessages[partialID]
		message := priorMessage + log.Body["MESSAGE"].(string)

		isLast, ok := log.Attributes["CONTAINER_PARTIAL_LAST"]
		if ok && isLast == "true" { // last partial log
			delete(d.partialMessages, partialID)
			log.Body["MESSAGE"] = message
			d.parseMessage(log)
		} else {
			d.partialMessages[partialID] = message
			// Remove partial from the batch
			batch.Logs = append(batch.Logs[:idx], batch.Logs[idx:]...)
		}
	}
	return batch, nil
}

func (d *DockerMultiline) Close() error {
	return nil
}

func (d *DockerMultiline) parseMessage(log *logs.Log) {
	message := log.Body["MESSAGE"].(string)

	var values map[string]any
	if err := json.Unmarshal([]byte(message), &values); err != nil {
		// probably not json
		return
	}
	for key, value := range values {
		log.Body[key] = value
	}
}
