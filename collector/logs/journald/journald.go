package journald

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/adx-mon/collector/logs"
	"github.com/coreos/go-systemd/sdjournal"
)

// container constants - move out of here
var (
	CONTAINER_METADATA = []string{"CONTAINER_PARTIAL_ID", "CONTAINER_PARTIAL_LAST", "CONTAINER_NAME"}
)

func CollectLogs(ctx context.Context) error {
	j, err := sdjournal.NewJournal()
	if err != nil {
		return err
	}

	defer j.Close()
	err = j.AddMatch("_SYSTEMD_UNIT=docker.service")
	if err != nil {
		return err
	}

	err = j.SeekTail()
	if err != nil {
		return err
	}

	for {
		if ctx.Err() != nil {
			// done
			return nil
		}

		c, err := j.Next()
		if err != nil {
			return err
		}

		if c == 0 {
			err = waitForLogs(j, ctx)
			if err != nil {
				return err
			}
			continue
		}

		entry, err := j.GetEntry()
		if err != nil {
			return err
		}

		//TODO batch
		metadata := map[string]string{}
		for _, k := range CONTAINER_METADATA {
			value, ok := entry.Fields[k]
			if ok {
				metadata[k] = value
			}
		}
		log := &logs.Log{
			Message:  entry.Fields["MESSAGE"],
			Metadata: metadata,
		}

		fmt.Println(log)
	}
}

func waitForLogs(j *sdjournal.Journal, ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			// done
			return nil
		}

		status := j.Wait(250 * time.Millisecond)
		switch status {
		case sdjournal.SD_JOURNAL_NOP:
			continue
		case sdjournal.SD_JOURNAL_APPEND, sdjournal.SD_JOURNAL_INVALIDATE:
			return nil
		default:
			if status < 0 {
				return fmt.Errorf("error status waiting for logs: %d", status)
			}
			// TODO unexpected event
		}
	}
}
