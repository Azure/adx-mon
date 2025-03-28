//go:build !disableDocker

package ingestor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimacherrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kwait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultImage = "ingestor"
	DefaultTag   = "latest"
)

type IngestorContainer struct {
	testcontainers.Container
}

func Run(ctx context.Context, img string, opts ...testcontainers.ContainerCustomizer) (*IngestorContainer, error) {
	var relative string
	for iter := range 4 {
		relative = strings.Repeat("../", iter)
		if _, err := os.Stat(filepath.Join(relative, "build/images/Dockerfile.ingestor")); err == nil {
			break
		}
	}

	var req testcontainers.ContainerRequest
	if img == "" {
		req = testcontainers.ContainerRequest{
			FromDockerfile: testcontainers.FromDockerfile{
				Repo:       DefaultImage,
				Tag:        DefaultTag,
				Context:    relative, // repo base
				Dockerfile: "build/images/Dockerfile.ingestor",
				KeepImage:  true,
			},
		}
	} else {
		req = testcontainers.ContainerRequest{
			Image: img,
		}
	}

	genericContainerReq := testcontainers.GenericContainerRequest{
		ContainerRequest: req,
	}

	for _, opt := range opts {
		if err := opt.Customize(&genericContainerReq); err != nil {
			return nil, err
		}
	}

	container, err := testcontainers.GenericContainer(ctx, genericContainerReq)
	var c *IngestorContainer
	if container != nil {
		c = &IngestorContainer{Container: container}
	}

	if err != nil {
		return c, fmt.Errorf("generic container: %w", err)
	}

	return c, nil
}

// WithStarted will start the container when it is created.
// You don't want to do this if you want to load the container into a k8s cluster.
func WithStarted() testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		req.Started = true
		return nil
	}
}

// WithTmpfsMount configures a tmpfs volume with specific size for the ingestor
func WithTmpfsMount(sizeBytes int64) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		// Store the size in a custom field in the Env map
		// We'll use a special key that won't conflict with actual environment variables
		if req.Env == nil {
			req.Env = make(map[string]string)
		}
		req.Env["__TMPFS_SIZE_BYTES__"] = fmt.Sprintf("%d", sizeBytes)

		return nil
	}
}

func WithCluster(ctx context.Context, k *k3s.K3sContainer) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		isolateKinds := []string{"StatefulSet", "CustomResourceDefinition"}
		writeTo, err := os.MkdirTemp("", "ingestor")
		if err != nil {
			return fmt.Errorf("failed to create temp dir: %w", err)
		}

		var img string
		if req.FromDockerfile.Context != "" {
			// We build to "ingestor:latest", load that image into the k3s cluster
			img = req.FromDockerfile.Repo + ":" + req.FromDockerfile.Tag
		} else {
			img = req.Image
		}

		req.LifecycleHooks = append(req.LifecycleHooks, testcontainers.ContainerLifecycleHooks{
			PreCreates: []testcontainers.ContainerRequestHook{
				func(ctx context.Context, req testcontainers.ContainerRequest) error {

					if err := k.LoadImages(ctx, img); err != nil {
						return fmt.Errorf("failed to load image: %w", err)
					}
					if err != nil {
						return fmt.Errorf("failed to create temp dir: %w", err)
					}

					// Our quick-start builds the necessary manifests to install ingestor. We need
					// to extract the actual StatefulSet that runs the ingestor instances so that we
					// can customize the launch arguments. In addition, these manifests are installed
					// as static pod manifests, which means they're immutable, so we install the statefulset
					// separately so that we can mutate the state at runtime.
					if err := testutils.ExtractManifests(writeTo, "build/k8s/ingestor.yaml", isolateKinds); err != nil {
						return fmt.Errorf("failed to extract manifests: %w", err)
					}

					manifestsPath := filepath.Join(writeTo, "manifests.yaml")
					containerPath := filepath.Join(testutils.K3sManifests, "ingestor-manifests.yaml")
					if err := k.CopyFileToContainer(ctx, manifestsPath, containerPath, 0644); err != nil {
						return fmt.Errorf("failed to copy file to container: %w", err)
					}

					return testutils.InstallCrds(ctx, k)
				},
			},
			PostCreates: []testcontainers.ContainerHook{
				func(ctx context.Context, c testcontainers.Container) error {
					// Deserialize our statefulset manifest and customize it to our needs
					ssPath := filepath.Join(writeTo, isolateKinds[0]+".yaml")
					ssData, err := os.ReadFile(ssPath)
					if err != nil {
						return fmt.Errorf("failed to read file: %w", err)
					}

					var statefulSet appsv1.StatefulSet
					if err := yaml.Unmarshal(ssData, &statefulSet); err != nil {
						return fmt.Errorf("failed to unmarshal statefulset: %w", err)
					}

					// (jesthom) our container doesn't have curl, so we can't provide the kubelet
					// an alternate way to ignore the self-signed cert. We'll have to disable the readiness probe
					statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe = nil
					// Readiness probe is https, but the cert is self-signed in our test env
					// statefulSet.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
					// 	InitialDelaySeconds: 5,
					// 	PeriodSeconds:       5,
					// 	FailureThreshold:    3,
					// 	SuccessThreshold:    1,
					// 	TimeoutSeconds:      1,
					// 	ProbeHandler: corev1.ProbeHandler{
					// 		Exec: &corev1.ExecAction{
					// 			Command: []string{
					// 				"/bin/sh",
					// 				"-c",
					// 				"curl -k https://127.0.0.1:443/readyz",
					// 			},
					// 		},
					// 	},
					// }

					// Extract the size if this was configured
					var (
						mountSizeBytes int64 = 21474836480 // Default 20GB
						storageDir           = "/mnt/data" // Default path
						hasVolumeMount       = false
					)
					// Check if a tmpfs size was specified
					if sizeStr, ok := req.Env["__TMPFS_SIZE_BYTES__"]; ok {
						if size, err := strconv.ParseInt(sizeStr, 10, 64); err == nil {
							mountSizeBytes = size
							hasVolumeMount = true
						}
					}

					statefulSet.Spec.Template.Spec.Containers[0].Image = img
					statefulSet.Spec.Template.Spec.Containers[0].ImagePullPolicy = corev1.PullNever
					statefulSet.Spec.Template.Spec.Containers[0].Args = []string{
						fmt.Sprintf("--storage-dir=%s", storageDir),
						"--max-segment-age=5s",
						fmt.Sprintf("--max-disk-usage=%d", mountSizeBytes),
						"--max-transfer-size=10485760",
						"--max-connections=1000",
						"--insecure-skip-verify",
						"--metrics-kusto-endpoints=Metrics=http://kustainer.default.svc.cluster.local:8080",
						"--logs-kusto-endpoints=Logs=http://kustainer.default.svc.cluster.local:8080",
					}
					statefulSet.Spec.Template.Spec.Affinity = nil
					statefulSet.Spec.Template.Spec.Tolerations = nil

					// Update StatefulSet to use hostPath volume
					if hasVolumeMount {
						// First, find and remove the existing "metrics" volume if it exists
						for i, vol := range statefulSet.Spec.Template.Spec.Volumes {
							if vol.Name == "metrics" {
								// Remove the existing volume by reslicing
								statefulSet.Spec.Template.Spec.Volumes = append(
									statefulSet.Spec.Template.Spec.Volumes[:i],
									statefulSet.Spec.Template.Spec.Volumes[i+1:]...,
								)
								break
							}
						}

						// Now add our tmpfs volume
						statefulSet.Spec.Template.Spec.Volumes = append(statefulSet.Spec.Template.Spec.Volumes, corev1.Volume{
							Name: "metrics", // Use "metrics" to match the existing volumeMount
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{
									Medium: corev1.StorageMediumMemory,
									SizeLimit: &[]resource.Quantity{
										resource.MustParse(fmt.Sprintf("%dMi", mountSizeBytes/1024/1024)),
									}[0],
								},
							},
						})

						// Add the filler container to the pod spec
						fillerContainer := createFillerContainer(storageDir)
						statefulSet.Spec.Template.Spec.Containers = append(
							statefulSet.Spec.Template.Spec.Containers,
							fillerContainer,
						)

						// We're keeping the existing volumeMount since it's already pointing to /mnt/data
					}

					restConfig, _, err := testutils.GetKubeConfig(ctx, k)
					if err != nil {
						return fmt.Errorf("failed to get kube config: %w", err)
					}

					clientset, err := kubernetes.NewForConfig(restConfig)
					if err != nil {
						return fmt.Errorf("failed to create clientset: %w", err)
					}

					ctrlCli, err := ctrlclient.New(restConfig, ctrlclient.Options{})
					if err != nil {
						return fmt.Errorf("failed to create controller client: %w", err)
					}

					err = kwait.PollUntilContextTimeout(ctx, 1*time.Second, 10*time.Minute, true, func(ctx context.Context) (bool, error) {
						ns, err := clientset.CoreV1().Namespaces().Get(ctx, statefulSet.GetNamespace(), metav1.GetOptions{})
						return ns != nil && err == nil, nil
					})
					if err != nil {
						return fmt.Errorf("failed to wait for namespace: %w", err)
					}

					// Check if StatefulSet already exists
					existingStatefulSet := &appsv1.StatefulSet{}
					err = ctrlCli.Get(ctx, types.NamespacedName{
						Namespace: statefulSet.Namespace,
						Name:      statefulSet.Name,
					}, existingStatefulSet)

					// If it exists, delete it first
					if err == nil {
						// StatefulSet exists, delete it
						if err := ctrlCli.Delete(ctx, existingStatefulSet); err != nil {
							return fmt.Errorf("failed to delete existing statefulset: %w", err)
						}

						// Wait for deletion to complete
						err = kwait.PollUntilContextTimeout(ctx, 1*time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
							err := ctrlCli.Get(ctx, types.NamespacedName{
								Namespace: statefulSet.Namespace,
								Name:      statefulSet.Name,
							}, &appsv1.StatefulSet{})
							return apimacherrors.IsNotFound(err), nil
						})
						if err != nil {
							return fmt.Errorf("failed to wait for statefulset deletion: %w", err)
						}
					}

					// Create the new StatefulSet
					if err := ctrlCli.Create(ctx, &statefulSet); err != nil {
						return fmt.Errorf("failed to create statefulset: %w", err)
					}

					// Continue with Function creation...
					if err := ctrlCli.Get(ctx, types.NamespacedName{Namespace: ingestorFunction.Namespace, Name: ingestorFunction.Name}, ingestorFunction); err != nil {
						if err := ctrlCli.Create(ctx, ingestorFunction); err != nil {
							return fmt.Errorf("failed to create function: %w", err)
						}
					}

					return nil
				},
			},
		})

		return nil
	}
}

// IngestorStatus contains details about the ingestor pods
type IngestorStatus struct {
	TotalPods      int                 // Total number of ingestor pods
	RunningPods    int                 // Number of running pods
	UnhealthyPods  int                 // Number of pods not in running state
	PodStatuses    map[string]string   // Map of pod name to status
	RestartCounts  map[string]int32    // Map of pod name to restart count
	ContainerState map[string][]string // Map of pod name to container states
}

// VerifyIngestorRunning checks if ingestor pods are running properly
func VerifyIngestorRunning(ctx context.Context, restConfig *rest.Config) (bool, *IngestorStatus, error) {
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return false, nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	namespace := "adx-mon" // Namespace where ingestor is deployed
	labelSelector := "app=ingestor"

	// List pods with the ingestor label
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return false, nil, fmt.Errorf("failed to list ingestor pods: %w", err)
	}

	status := &IngestorStatus{
		TotalPods:      len(pods.Items),
		RunningPods:    0,
		UnhealthyPods:  0,
		PodStatuses:    make(map[string]string),
		RestartCounts:  make(map[string]int32),
		ContainerState: make(map[string][]string),
	}

	// If no pods found, return false
	if status.TotalPods == 0 {
		return false, status, fmt.Errorf("no ingestor pods found")
	}

	// Check each pod's status
	for _, pod := range pods.Items {
		podName := pod.Name
		status.PodStatuses[podName] = string(pod.Status.Phase)

		// Count running pods
		if pod.Status.Phase == corev1.PodRunning {
			status.RunningPods++
		} else {
			status.UnhealthyPods++
		}

		// Track container restart counts and states
		totalRestarts := int32(0)
		containerStates := make([]string, 0)

		// Check container statuses
		for _, containerStatus := range pod.Status.ContainerStatuses {
			totalRestarts += containerStatus.RestartCount

			// Determine container state
			state := "unknown"
			if containerStatus.State.Running != nil {
				state = fmt.Sprintf("running (started at %s)", containerStatus.State.Running.StartedAt.Format(time.RFC3339))
			} else if containerStatus.State.Waiting != nil {
				state = fmt.Sprintf("waiting: %s - %s",
					containerStatus.State.Waiting.Reason,
					containerStatus.State.Waiting.Message)
			} else if containerStatus.State.Terminated != nil {
				state = fmt.Sprintf("terminated: %s (exit code %d) - %s",
					containerStatus.State.Terminated.Reason,
					containerStatus.State.Terminated.ExitCode,
					containerStatus.State.Terminated.Message)
			}

			containerStates = append(containerStates,
				fmt.Sprintf("%s: %s", containerStatus.Name, state))
		}

		status.RestartCounts[podName] = totalRestarts
		status.ContainerState[podName] = containerStates
	}

	// All pods should be running and have no excessive restarts
	allPodsRunning := status.RunningPods == status.TotalPods

	return allPodsRunning, status, nil
}

// createFillerContainer returns a container spec that continuously creates 1MB files
// in the specified directory and handles disk-full scenarios gracefully
func createFillerContainer(mountPath string) corev1.Container {
	return corev1.Container{
		Name:            "storage-filler",
		Image:           "mcr.microsoft.com/cbl-mariner/base/core:2.0.20250304",
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command: []string{
			"/bin/bash",
			"-c",
		},
		Args: []string{
			`while true; do
                dd if=/dev/zero of="` + mountPath + `/filler-$(date +%s).dat" bs=10K count=1 2>/dev/null || true
                sleep 1
            done`,
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "metrics",
				MountPath: mountPath,
			},
		},
	}
}

type KustoTableSchema struct{}

func (k *KustoTableSchema) TableName() string {
	return "Collector"
}

func (k *KustoTableSchema) CslColumns() []string {
	return []string{
		"msg:string",
		"lvl:string",
		"ts:datetime",
		"namespace:string",
		"container:string",
		"pod:string",
		"host:string",
	}
}

var ingestorFunction = &adxmonv1.Function{
	TypeMeta: metav1.TypeMeta{
		APIVersion: "adx-mon.azure.com/v1",
		Kind:       "Function",
	},
	ObjectMeta: metav1.ObjectMeta{
		Name:      "ingestor",
		Namespace: "adx-mon",
	},
	Spec: adxmonv1.FunctionSpec{
		Database: "Logs",
		Body: `.create-or-alter function with (view=true, folder='views') Ingestor () {
  table('Ingestor')
  | extend msg = tostring(Body.msg),
		   lvl = tostring(Body.lvl),
		   ts = todatetime(Body.ts),
		   namespace = tostring(Resource.namespace),
		   container = tostring(Resource.container),
		   pod = tostring(Resource.pod),
		   host = tostring(Resource.host)
  | project-away Timestamp, ObservedTimestamp, TraceId, SpanId, SeverityText, SeverityNumber, Body, Resource, Attributes
}`,
	},
}
