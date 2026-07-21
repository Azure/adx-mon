package testutils

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"testing"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// DumpIntegrationDiagnostics captures failure evidence while the test cluster is still running.
// Every section is best effort so an unhealthy API or Kusto endpoint cannot hide other evidence.
func DumpIntegrationDiagnostics(ctx context.Context, t testing.TB, cluster *k3s.K3sContainer, kustoURL string) {
	t.Helper()
	t.Log("===== integration failure diagnostics =====")

	if cluster == nil {
		t.Log("Kubernetes diagnostics: unavailable because K3s setup did not produce a container")
	} else {
		restConfig, controllerClient, err := GetKubeConfig(ctx, cluster)
		if err != nil {
			t.Logf("Kubernetes client: ERROR: %v", err)
		} else {
			clientset, err := kubernetes.NewForConfig(restConfig)
			if err != nil {
				t.Logf("Kubernetes clientset: ERROR: %v", err)
			} else {
				dumpPodsAndLogs(ctx, t, clientset)
				dumpEvents(ctx, t, clientset)
				dumpWorkloads(ctx, t, clientset)
				dumpLeases(ctx, t, clientset)
			}
			dumpFunctions(ctx, t, controllerClient)
		}
	}

	if kustoURL == "" {
		t.Log("Kusto functions: unavailable because setup did not produce an endpoint")
	} else {
		dumpKustoFunctions(ctx, t, kustoURL)
	}

	for _, command := range [][]string{
		{"df", "-h"},
		{"docker", "system", "df"},
		{"docker", "image", "ls", "--no-trunc"},
		{"docker", "container", "ls", "-a", "--no-trunc"},
	} {
		dumpCommand(ctx, t, command)
	}
	t.Log("===== end integration failure diagnostics =====")
}

func dumpPodsAndLogs(ctx context.Context, t testing.TB, clientset kubernetes.Interface) {
	const logRequestTimeout = 10 * time.Second
	logLimitBytes := int64(1024 * 1024)

	pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Logf("Pods: ERROR: %v", err)
		return
	}

	sort.Slice(pods.Items, func(i, j int) bool {
		return pods.Items[i].Namespace+"/"+pods.Items[i].Name < pods.Items[j].Namespace+"/"+pods.Items[j].Name
	})
	t.Log("----- pods -----")
	for _, pod := range pods.Items {
		t.Logf("%s", formatPod(&pod))
	}

	for _, pod := range pods.Items {
		for _, container := range diagnosticContainers(&pod) {
			for _, previous := range []bool{false, true} {
				label := "current"
				if previous {
					label = "previous"
				}
				requestCtx, cancel := context.WithTimeout(ctx, logRequestTimeout)
				logs, err := clientset.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
					Container:  container,
					Previous:   previous,
					Timestamps: true,
					LimitBytes: &logLimitBytes,
				}).DoRaw(requestCtx)
				cancel()
				t.Logf("----- logs %s/%s container=%s instance=%s -----", pod.Namespace, pod.Name, container, label)
				if err != nil {
					t.Logf("ERROR: %v", err)
					continue
				}
				t.Log(strings.TrimSpace(string(logs)))
			}
		}
	}
}

func formatPod(pod *corev1.Pod) string {
	ready := false
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			ready = condition.Status == corev1.ConditionTrue
			break
		}
	}

	images := make(map[string]string, len(pod.Spec.InitContainers)+len(pod.Spec.Containers))
	for _, container := range append(pod.Spec.InitContainers, pod.Spec.Containers...) {
		images[container.Name] = container.Image
	}
	statuses := append(append([]corev1.ContainerStatus{}, pod.Status.InitContainerStatuses...), pod.Status.ContainerStatuses...)
	details := make([]string, 0, len(statuses))
	for _, status := range statuses {
		startedAt, waitingReason, terminationReason, lastTerminationReason := "", "", "", ""
		if status.State.Running != nil {
			startedAt = status.State.Running.StartedAt.Time.Format(time.RFC3339Nano)
		}
		if status.State.Waiting != nil {
			waitingReason = status.State.Waiting.Reason
		}
		if status.State.Terminated != nil {
			terminationReason = status.State.Terminated.Reason
		}
		if status.LastTerminationState.Terminated != nil {
			lastTerminationReason = status.LastTerminationState.Terminated.Reason
		}
		details = append(details, fmt.Sprintf("%s{ready=%t restarts=%d image=%q imageID=%q started=%q waiting=%q termination=%q lastTermination=%q}",
			status.Name, status.Ready, status.RestartCount, images[status.Name], status.ImageID, startedAt, waitingReason, terminationReason, lastTerminationReason))
	}

	return fmt.Sprintf("%s/%s phase=%s ready=%t created=%s containers=[%s]", pod.Namespace, pod.Name, pod.Status.Phase,
		ready, pod.CreationTimestamp.Time.Format(time.RFC3339Nano), strings.Join(details, ", "))
}

func diagnosticContainers(pod *corev1.Pod) []string {
	const components = "ingestor collector alerter kustainer"
	var names []string
	for _, container := range append(pod.Spec.InitContainers, pod.Spec.Containers...) {
		candidate := strings.ToLower(pod.Name + " " + container.Name + " " + container.Image)
		for _, component := range strings.Fields(components) {
			if strings.Contains(candidate, component) {
				names = append(names, container.Name)
				break
			}
		}
	}
	return names
}

func dumpEvents(ctx context.Context, t testing.TB, clientset kubernetes.Interface) {
	events, err := clientset.CoreV1().Events("").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Logf("Events: ERROR: %v", err)
		return
	}
	sort.SliceStable(events.Items, func(i, j int) bool {
		return eventTimestamp(&events.Items[i]).Before(eventTimestamp(&events.Items[j]))
	})
	t.Log("----- events (chronological) -----")
	for _, event := range events.Items {
		t.Logf("%s %s/%s %s %s count=%d: %s", eventTimestamp(&event).Format(time.RFC3339Nano), event.Namespace,
			event.InvolvedObject.Name, event.Type, event.Reason, event.Count, event.Message)
	}
}

func eventTimestamp(event *corev1.Event) time.Time {
	if !event.EventTime.IsZero() {
		return event.EventTime.Time
	}
	if !event.LastTimestamp.IsZero() {
		return event.LastTimestamp.Time
	}
	return event.CreationTimestamp.Time
}

func dumpWorkloads(ctx context.Context, t testing.TB, clientset kubernetes.Interface) {
	t.Log("----- workload status -----")
	statefulSets, err := clientset.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Logf("StatefulSets: ERROR: %v", err)
	} else {
		for _, workload := range statefulSets.Items {
			t.Logf("StatefulSet %s/%s desired=%d current=%d ready=%d unavailable=%d", workload.Namespace, workload.Name,
				replicas(workload.Spec.Replicas), workload.Status.CurrentReplicas, workload.Status.ReadyReplicas,
				replicas(workload.Spec.Replicas)-workload.Status.ReadyReplicas)
		}
	}

	daemonSets, err := clientset.AppsV1().DaemonSets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Logf("DaemonSets: ERROR: %v", err)
	} else {
		for _, workload := range daemonSets.Items {
			t.Logf("DaemonSet %s/%s desired=%d current=%d ready=%d unavailable=%d", workload.Namespace, workload.Name,
				workload.Status.DesiredNumberScheduled, workload.Status.CurrentNumberScheduled, workload.Status.NumberReady,
				workload.Status.NumberUnavailable)
		}
	}

	deployments, err := clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Logf("Deployments: ERROR: %v", err)
	} else {
		for _, workload := range deployments.Items {
			t.Logf("Deployment %s/%s desired=%d current=%d ready=%d unavailable=%d", workload.Namespace, workload.Name,
				replicas(workload.Spec.Replicas), workload.Status.Replicas, workload.Status.ReadyReplicas, workload.Status.UnavailableReplicas)
		}
	}
}

func replicas(value *int32) int32 {
	if value == nil {
		return 1
	}
	return *value
}

func dumpFunctions(ctx context.Context, t testing.TB, client ctrlclient.Client) {
	var functions adxmonv1.FunctionList
	if err := client.List(ctx, &functions); err != nil {
		t.Logf("Functions: ERROR: %v", err)
		return
	}
	t.Log("----- Function resources -----")
	for _, function := range functions.Items {
		t.Logf("%s/%s generation=%d observedGeneration=%d status=%q reason=%q message=%q error=%q appliedEndpoint=%q conditions=%+v",
			function.Namespace, function.Name, function.Generation, function.Status.ObservedGeneration, function.Status.Status,
			function.Status.Reason, function.Status.Message, function.Status.Error, function.Spec.AppliedEndpoint, function.Status.Conditions)
	}
}

func dumpLeases(ctx context.Context, t testing.TB, clientset kubernetes.Interface) {
	leases, err := clientset.CoordinationV1().Leases("").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Logf("Leases: ERROR: %v", err)
		return
	}
	t.Log("----- leases / leadership -----")
	for _, lease := range leases.Items {
		t.Logf("%s/%s holder=%v acquired=%v renewed=%v transitions=%v", lease.Namespace, lease.Name,
			lease.Spec.HolderIdentity, lease.Spec.AcquireTime, lease.Spec.RenewTime, lease.Spec.LeaseTransitions)
	}
}

func dumpKustoFunctions(ctx context.Context, t testing.TB, endpoint string) {
	client, err := kusto.New(kusto.NewConnectionStringBuilder(endpoint))
	if err != nil {
		t.Logf("Kusto client: ERROR: %v", err)
		return
	}
	defer client.Close()

	for _, command := range []string{".show functions", ".show function Collector"} {
		t.Logf("----- Kusto Logs: %s -----", command)
		rows, err := client.Mgmt(ctx, "Logs", kql.New("").AddUnsafe(command))
		if err != nil {
			t.Logf("ERROR: %v", err)
			continue
		}
		for {
			row, inlineErr, finalErr := rows.NextRowOrError()
			if inlineErr != nil {
				t.Logf("PARTIAL ERROR: %v", inlineErr)
			}
			if finalErr == io.EOF {
				break
			}
			if finalErr != nil {
				t.Logf("ERROR: %v", finalErr)
				break
			}
			if row == nil {
				t.Log("PARTIAL ERROR: Kusto returned no row")
				continue
			}
			t.Logf("columns=%v row=%s", row.ColumnNames(), row)
		}
		rows.Stop()
	}
}

func dumpCommand(ctx context.Context, t testing.TB, command []string) {
	t.Logf("----- host: %s -----", strings.Join(command, " "))
	output, err := exec.CommandContext(ctx, command[0], command[1:]...).CombinedOutput()
	if len(output) > 0 {
		t.Log(strings.TrimSpace(string(output)))
	}
	if err != nil {
		t.Logf("ERROR: %v", err)
	}
}
