package journald

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/adx-mon/collector/logs"
	"github.com/coreos/go-systemd/sdjournal"
)

// container constants - move out of here
var (
	CONTAINER_METADATA = []string{"CONTAINER_PARTIAL_ID", "CONTAINER_PARTIAL_LAST", "CONTAINER_NAME"}
)

const (
	ContainerAttribute = "k8s.container.name"
	PodAttribute       = "k8s.pod.name"
	NamespaceAttribute = "k8s.namespace.name"
)

type JournaldCollector struct {
	transforms []logs.Transformer
}

func NewJournaldCollector(transforms []logs.Transformer) *JournaldCollector {
	return &JournaldCollector{
		transforms: transforms,
	}
}

func (c *JournaldCollector) CollectLogs(ctx context.Context) error {
	for _, transformer := range c.transforms {
		transformer.Open(ctx)
		// TODO shutdown transformers, going from first to last
	}

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

		ret, err := j.Next()
		if err != nil {
			return err
		}

		if ret == 0 {
			err = c.waitForLogs(ctx, j)
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
		attributes := map[string]string{}
		for _, k := range CONTAINER_METADATA {
			value, ok := entry.Fields[k]
			if ok {
				attributes[k] = value
			}
		}
		c.addKubernetesAttributes(entry, attributes)
		log := &logs.Log{
			Timestamp:         int64(entry.RealtimeTimestamp),
			ObservedTimestamp: time.Now().UnixNano(),
			Body:              map[string]interface{}{"MESSAGE": entry.Fields["MESSAGE"]},
			Attributes:        attributes,
		}

		c.process(ctx, log)
	}
}

func (c *JournaldCollector) waitForLogs(ctx context.Context, j *sdjournal.Journal) error {
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

func (c *JournaldCollector) process(ctx context.Context, log *logs.Log) error {
	batch := &logs.LogBatch{
		Logs: []*logs.Log{log},
	}

	var err error
	for _, transformer := range c.transforms {
		batch, err = transformer.Transform(ctx, batch)
		if err != nil {
			return err
		}
	}
	// TODO
	fmt.Println(batch)
	return nil
}

func (c *JournaldCollector) addKubernetesAttributes(entry *sdjournal.JournalEntry, attributes map[string]string) {
	containerNameAttribute, ok := attributes["CONTAINER_NAME"]
	if !ok {
		return
	}

	// example container_name: k8s_calico-node_canal-9dq4b_kube-system_e3e72bef-7f90-497e-8870-65509a3f95ad_0
	split := strings.Split(containerNameAttribute, "_")
	if len(split) != 6 {
		return
	}

	attributes[ContainerAttribute] = split[1]
	attributes[PodAttribute] = split[2]
	attributes[NamespaceAttribute] = split[3]
}
