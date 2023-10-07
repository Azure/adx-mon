package journald

import (
	"strings"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/coreos/go-systemd/sdjournal"
)

const (
	ContainerAttribute = "k8s.container.name"
	PodAttribute       = "k8s.pod.name"
	NamespaceAttribute = "k8s.namespace.name"

	container_name_field         = "CONTAINER_NAME"
	container_partial_id_field   = "CONTAINER_PARTIAL_ID"
	container_partial_last_field = "CONTAINER_PARTIAL_LAST"
)

func (j *JournaldSource) combinePartialMessages(entry *sdjournal.JournalEntry) (ret *sdjournal.JournalEntry, ok bool) {
	// TODO - this combines split logs from Docker. This does not yet combine split logs from Journald itself with its larger limit.
	partialId, ok := entry.Fields[container_partial_id_field]
	if !ok {
		// Not a partial log
		return entry, true
	}

	priorMessages, ok := j.partialMessages[partialId]
	if !ok {
		// First message we have seen for this partialId
		j.partialMessages[partialId] = entry
	} else {
		// Combine the messages
		priorMessages.Fields[journald_message_field] += entry.Fields[journald_message_field]
	}

	if isLast, ok := entry.Fields[container_partial_last_field]; ok && isLast == "true" {
		// Last message. Delete from the map and return the combined entity
		delete(j.partialMessages, partialId)
		priorMessages.Cursor = entry.Cursor // use latest cursor
		return priorMessages, true
	}
	return nil, false
}

func enrichContainerMetadata(log *types.Log, entry *sdjournal.JournalEntry) {
	containerNameAttribute, ok := entry.Fields[container_name_field]
	if !ok {
		return
	}

	// example container_name: k8s_calico-node_canal-9dq4b_kube-system_e3e72bef-7f90-497e-8870-65509a3f95ad_0
	// field[0] = "k8s"
	// field[1] = container name
	// field[2] = pod name
	// field[3] = namespace
	split := strings.Split(containerNameAttribute, "_")
	if len(split) != 6 {
		return
	}

	log.Attributes[ContainerAttribute] = split[1]
	log.Attributes[PodAttribute] = split[2]
	log.Attributes[NamespaceAttribute] = split[3]
}
