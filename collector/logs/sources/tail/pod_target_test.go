package tail

import (
	"fmt"
	"log/slog"
	"testing"

	"github.com/Azure/adx-mon/collector/logs/sources/tail/sourceparse"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	uid = "1234"
)

func TestGetFileTargets(t *testing.T) {
	logger.SetLevel(slog.LevelDebug)
	type testcase struct {
		name     string
		pod      *v1.Pod
		expected []FileTailTarget
	}

	ourNode := "node1"
	otherNode := "node2"

	tests := []testcase{
		{
			name:     "Pod not on the same node",
			pod:      genPod("pod1", "namespace1", otherNode, []string{"container1"}, nil),
			expected: nil,
		},
		{
			name:     "Pod not opted in to scraping",
			pod:      genPod("pod1", "namespace1", ourNode, []string{"container1"}, nil),
			expected: nil,
		},
		{
			name: "Pod has scraping turned off",
			pod: genPod("pod1", "namespace1", ourNode, []string{"container1"}, map[string]string{
				"adx-mon/scrape": "false",
			}),
			expected: nil,
		},
		{
			name: "Pod has no log destinations",
			pod: genPod("pod1", "namespace1", ourNode, []string{"container1"}, map[string]string{
				"adx-mon/scrape": "true",
			}),
			expected: nil,
		},
		{
			name: "Pod has invalid log destinations",
			pod: genPod("pod1", "namespace1", ourNode, []string{"container1"}, map[string]string{
				"adx-mon/scrape":          "true",
				"adx-mon/log-destination": "db1:",
			}),
			expected: nil,
		},
		{
			name: "Pod has log destination",
			pod: genPod("pod1", "namespace1", ourNode, []string{"container1"}, map[string]string{
				"adx-mon/scrape":          "true",
				"adx-mon/log-destination": "db1:table1",
			}),
			expected: []FileTailTarget{
				{
					FilePath: fmt.Sprint("/var/log/pods/namespace1_pod1_", uid, "/container1/0.log"),
					Database: "db1",
					Table:    "table1",
					LogType:  sourceparse.LogTypeDocker,
					Parsers:  []string{},
					Resources: map[string]interface{}{
						"pod":         "pod1",
						"namespace":   "namespace1",
						"container":   "container1",
						"containerID": "docker://container1",
					},
				},
			},
		},
		{
			name: "Pod has log destination and parser",
			pod: genPod("pod1", "namespace1", ourNode, []string{"container1"}, map[string]string{
				"adx-mon/scrape":          "true",
				"adx-mon/log-destination": "db1:table1",
				"adx-mon/log-parsers":     "json,json",
			}),
			expected: []FileTailTarget{
				{
					FilePath: fmt.Sprint("/var/log/pods/namespace1_pod1_", uid, "/container1/0.log"),
					Database: "db1",
					Table:    "table1",
					LogType:  sourceparse.LogTypeDocker,
					Parsers:  []string{"json", "json"},
					Resources: map[string]interface{}{
						"pod":         "pod1",
						"namespace":   "namespace1",
						"container":   "container1",
						"containerID": "docker://container1",
					},
				},
			},
		},
		{
			name: "No targets with invalid parsers",
			pod: genPod("pod1", "namespace1", ourNode, []string{"container1"}, map[string]string{
				"adx-mon/scrape":          "true",
				"adx-mon/log-destination": "db1:table1",
				"adx-mon/log-parsers":     "json,unknown",
			}),
			expected: []FileTailTarget{},
		},
		{
			name: "Pod has log destination and parser and multiple containers",
			pod: genPodWithLabels("pod1", "namespace1", ourNode, []string{"container1", "container2"}, map[string]string{
				"adx-mon/scrape":              "true",
				"adx-mon/log-destination":     "db1:table1",
				"adx-mon/log-parsers":         "json",
				"cni.projectcalico.org/podIP": "10.10.10.10",
			}, map[string]string{
				"app": "myapp",
			}),
			expected: []FileTailTarget{
				{
					FilePath: fmt.Sprint("/var/log/pods/namespace1_pod1_", uid, "/container1/0.log"),
					Database: "db1",
					Table:    "table1",
					LogType:  sourceparse.LogTypeDocker,
					Parsers:  []string{"json"},
					Resources: map[string]interface{}{
						"pod":                                    "pod1",
						"namespace":                              "namespace1",
						"container":                              "container1",
						"containerID":                            "docker://container1",
						"annotation.cni.projectcalico.org/podIP": "10.10.10.10",
						"label.app":                              "myapp",
					},
				},
				{
					FilePath: fmt.Sprint("/var/log/pods/namespace1_pod1_", uid, "/container2/0.log"),
					Database: "db1",
					Table:    "table1",
					LogType:  sourceparse.LogTypeDocker,
					Parsers:  []string{"json"},
					Resources: map[string]interface{}{
						"pod":                                    "pod1",
						"namespace":                              "namespace1",
						"container":                              "container2",
						"containerID":                            "docker://container2",
						"annotation.cni.projectcalico.org/podIP": "10.10.10.10",
						"label.app":                              "myapp",
					},
				},
			},
		},
		{
			name: "Pod has log destination and parser and multiple types of containers",
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: "namespace1",
					Annotations: map[string]string{
						"adx-mon/scrape":              "true",
						"adx-mon/log-destination":     "db1:table1",
						"adx-mon/log-parsers":         "json",
						"cni.projectcalico.org/podIP": "10.10.10.10",
					},
					UID: uid,
				},
				Spec: v1.PodSpec{
					NodeName:            ourNode,
					Containers:          []v1.Container{{Name: "container1", Image: "image-container1"}},
					InitContainers:      []v1.Container{{Name: "init-container1", Image: "image-container2"}},
					EphemeralContainers: []v1.EphemeralContainer{{EphemeralContainerCommon: v1.EphemeralContainerCommon{Name: "ephemeral-container1", Image: "image-container3"}}},
				},
				Status: v1.PodStatus{
					ContainerStatuses: []v1.ContainerStatus{
						{
							Name: "container1",
							// Not a real id format
							ContainerID: "docker://container1",
							State: v1.ContainerState{
								Running: &v1.ContainerStateRunning{},
							},
						},
						{
							Name: "init-container1",
							// Not a real id format
							ContainerID: "docker://init-container1",
							State: v1.ContainerState{
								Running: &v1.ContainerStateRunning{},
							},
						},
						{
							Name: "ephemeral-container1",
							// Not a real id format
							ContainerID: "docker://ephemeral-container1",
							State: v1.ContainerState{
								Running: &v1.ContainerStateRunning{},
							},
						},
					},
				},
			},
			expected: []FileTailTarget{
				{
					FilePath: fmt.Sprint("/var/log/pods/namespace1_pod1_", uid, "/container1/0.log"),
					Database: "db1",
					Table:    "table1",
					LogType:  sourceparse.LogTypeDocker,
					Parsers:  []string{"json"},
					Resources: map[string]interface{}{
						"pod":                                    "pod1",
						"namespace":                              "namespace1",
						"container":                              "container1",
						"containerID":                            "docker://container1",
						"annotation.cni.projectcalico.org/podIP": "10.10.10.10",
					},
				},
				{
					FilePath: fmt.Sprint("/var/log/pods/namespace1_pod1_", uid, "/init-container1/0.log"),
					Database: "db1",
					Table:    "table1",
					LogType:  sourceparse.LogTypeDocker,
					Parsers:  []string{"json"},
					Resources: map[string]interface{}{
						"pod":                                    "pod1",
						"namespace":                              "namespace1",
						"container":                              "init-container1",
						"containerID":                            "docker://init-container1",
						"annotation.cni.projectcalico.org/podIP": "10.10.10.10",
					},
				},
				{
					FilePath: fmt.Sprint("/var/log/pods/namespace1_pod1_", uid, "/ephemeral-container1/0.log"),
					Database: "db1",
					Table:    "table1",
					LogType:  sourceparse.LogTypeDocker,
					Parsers:  []string{"json"},
					Resources: map[string]interface{}{
						"pod":                                    "pod1",
						"namespace":                              "namespace1",
						"container":                              "ephemeral-container1",
						"containerID":                            "docker://ephemeral-container1",
						"annotation.cni.projectcalico.org/podIP": "10.10.10.10",
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			targets := getFileTargets(test.pod, ourNode)
			require.ElementsMatch(t, test.expected, targets)
		})
	}
}

func TestIsTargetChanged(t *testing.T) {
	type testcase struct {
		name     string
		old      *FileTailTarget
		new      *FileTailTarget
		expected bool
	}

	tests := []testcase{
		{
			name: "No change",
			old: &FileTailTarget{
				FilePath: "/path/to/file",
				Database: "db",
				Table:    "table",
				LogType:  sourceparse.LogTypeDocker,
				Parsers:  []string{"json"},
				Resources: map[string]interface{}{
					"pod":       "pod1",
					"namespace": "namespace1",
					"container": "container1",
				},
			},
			new: &FileTailTarget{
				FilePath: "/path/to/file",
				Database: "db",
				Table:    "table",
				LogType:  sourceparse.LogTypeDocker,
				Parsers:  []string{"json"},
				Resources: map[string]interface{}{
					"pod":       "pod1",
					"namespace": "namespace1",
					"container": "container1",
				},
			},
			expected: false,
		},
		{
			name: "Change in file path",
			old: &FileTailTarget{
				FilePath: "/path/to/file",
				Database: "db",
				Table:    "table",
				LogType:  sourceparse.LogTypeDocker,
				Parsers:  []string{"json"},
			},
			new: &FileTailTarget{
				FilePath: "/path/to/other/file",
				Database: "db",
				Table:    "table",
				LogType:  sourceparse.LogTypeDocker,
				Parsers:  []string{"json"},
			},
			expected: false, // This is explicitly not allowed to be updated. Must remove the target.
		},
		{
			name: "Change in log type",
			old: &FileTailTarget{
				FilePath: "/path/to/file",
				Database: "db",
				Table:    "table",
				LogType:  sourceparse.LogTypeDocker,
				Parsers:  []string{"json"},
			},
			new: &FileTailTarget{
				FilePath: "/path/to/file",
				Database: "db",
				Table:    "table",
				LogType:  sourceparse.LogTypePlain,
				Parsers:  []string{"json"},
			},
			expected: false, // This is explicitly not allowed to be updated. Must remove the target.
		},
		{
			name: "Change in database",
			old: &FileTailTarget{
				FilePath: "/path/to/file",
				Database: "db",
				Table:    "table",
				LogType:  sourceparse.LogTypeDocker,
				Parsers:  []string{"json"},
			},
			new: &FileTailTarget{
				FilePath: "/path/to/file",
				Database: "otherdb",
				Table:    "table",
				LogType:  sourceparse.LogTypeDocker,
				Parsers:  []string{"json"},
			},
			expected: true,
		},
		{
			name: "Change in table",
			old: &FileTailTarget{
				FilePath: "/path/to/file",
				Database: "db",
				Table:    "table",
				LogType:  sourceparse.LogTypeDocker,
				Parsers:  []string{"json"},
			},
			new: &FileTailTarget{
				FilePath: "/path/to/file",
				Database: "db",
				Table:    "othertable",
				LogType:  sourceparse.LogTypeDocker,
				Parsers:  []string{"json"},
			},
			expected: true,
		},
		{
			name: "Change in number of parsers",
			old: &FileTailTarget{
				FilePath: "/path/to/file",
				Database: "db",
				Table:    "table",
				LogType:  sourceparse.LogTypeDocker,
				Parsers:  []string{},
			},
			new: &FileTailTarget{
				FilePath: "/path/to/file",
				Database: "db",
				Table:    "table",
				LogType:  sourceparse.LogTypeDocker,
				Parsers:  []string{"json"},
			},
			expected: true,
		},
		{
			name: "Change in parsers",
			old: &FileTailTarget{
				FilePath: "/path/to/file",
				Database: "db",
				Table:    "table",
				LogType:  sourceparse.LogTypeDocker,
				Parsers:  []string{"otherparser", "json"},
			},
			new: &FileTailTarget{
				FilePath: "/path/to/file",
				Database: "db",
				Table:    "table",
				LogType:  sourceparse.LogTypeDocker,
				Parsers:  []string{"json", "otherparser"},
			},
			expected: true,
		},
		{
			name: "Change in attributes",
			old: &FileTailTarget{
				FilePath: "/path/to/file",
				Database: "db",
				Table:    "table",
				LogType:  sourceparse.LogTypeDocker,
				Parsers:  []string{"json"},
				Resources: map[string]interface{}{
					"pod":       "pod1",
					"namespace": "namespace1",
					"container": "container1",
				},
			},
			new: &FileTailTarget{
				FilePath: "/path/to/file",
				Database: "db",
				Table:    "table",
				LogType:  sourceparse.LogTypeDocker,
				Parsers:  []string{"json"},
				Resources: map[string]interface{}{
					"pod":       "pod2",
					"namespace": "namespace1",
					"container": "container1",
				},
			},
			expected: true,
		},
		{
			name: "More attributes",
			old: &FileTailTarget{
				FilePath: "/path/to/file",
				Database: "db",
				Table:    "table",
				LogType:  sourceparse.LogTypeDocker,
				Parsers:  []string{"json"},
				Resources: map[string]interface{}{
					"pod":       "pod1",
					"namespace": "namespace1",
					"container": "container1",
				},
			},
			new: &FileTailTarget{
				FilePath: "/path/to/file",
				Database: "db",
				Table:    "table",
				LogType:  sourceparse.LogTypeDocker,
				Parsers:  []string{"json"},
				Resources: map[string]interface{}{
					"pod":       "pod1",
					"namespace": "namespace1",
					"container": "container1",
					"extra":     "value",
				},
			},
			expected: true,
		},
		{
			name: "fewer attributes",
			old: &FileTailTarget{
				FilePath: "/path/to/file",
				Database: "db",
				Table:    "table",
				LogType:  sourceparse.LogTypeDocker,
				Parsers:  []string{"json"},
				Resources: map[string]interface{}{
					"pod":       "pod1",
					"namespace": "namespace1",
					"container": "container1",
				},
			},
			new: &FileTailTarget{
				FilePath: "/path/to/file",
				Database: "db",
				Table:    "table",
				LogType:  sourceparse.LogTypeDocker,
				Parsers:  []string{"json"},
				Resources: map[string]interface{}{
					"pod":       "pod1",
					"namespace": "namespace1",
				},
			},
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.expected, isTargetChanged(*test.old, *test.new))
		})
	}
}

func genPod(name, namespace, nodeName string, containerNames []string, annotations map[string]string) *v1.Pod {
	containers := make([]v1.Container, 0, len(containerNames))
	for _, containerName := range containerNames {
		containers = append(containers, v1.Container{
			Name:  containerName,
			Image: fmt.Sprintf("image-%s", containerName),
		})
	}
	statuses := make([]v1.ContainerStatus, 0, len(containerNames))
	for _, containerName := range containerNames {
		statuses = append(statuses, v1.ContainerStatus{
			Name: containerName,
			// Not a real id format
			ContainerID: fmt.Sprintf("docker://%s", containerName),
			State: v1.ContainerState{
				Running: &v1.ContainerStateRunning{},
			},
		})
	}
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
			UID:         uid,
		},
		Spec: v1.PodSpec{
			NodeName:   nodeName,
			Containers: containers,
		},
		Status: v1.PodStatus{
			ContainerStatuses: statuses,
		},
	}
}

func genPodWithLabels(name, namespace, nodeName string, containerNames []string, annotations map[string]string, labels map[string]string) *v1.Pod {
	containers := make([]v1.Container, 0, len(containerNames))
	for _, containerName := range containerNames {
		containers = append(containers, v1.Container{
			Name:  containerName,
			Image: fmt.Sprintf("image-%s", containerName),
		})
	}
	statuses := make([]v1.ContainerStatus, 0, len(containerNames))
	for _, containerName := range containerNames {
		statuses = append(statuses, v1.ContainerStatus{
			Name: containerName,
			// Not a real id format
			ContainerID: fmt.Sprintf("docker://%s", containerName),
			State: v1.ContainerState{
				Running: &v1.ContainerStateRunning{},
			},
		})
	}
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
			Labels:      labels,
			UID:         uid,
		},
		Spec: v1.PodSpec{
			NodeName:   nodeName,
			Containers: containers,
		},
		Status: v1.PodStatus{
			ContainerStatuses: statuses,
		},
	}
}
