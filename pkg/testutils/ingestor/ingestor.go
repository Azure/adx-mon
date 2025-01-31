//go:build !disableDocker

package ingestor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/testutils"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	kwait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
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

					return testutils.InstallFunctionsCrd(ctx, k)
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

					statefulSet.Spec.Template.Spec.Containers[0].Image = img
					statefulSet.Spec.Template.Spec.Containers[0].ImagePullPolicy = corev1.PullNever
					statefulSet.Spec.Template.Spec.Containers[0].Args = []string{
						"--storage-dir=/mnt/data",
						"--max-segment-age=5s",
						"--max-disk-usage=21474836480",
						"--max-transfer-size=10485760",
						"--max-connections=1000",
						"--insecure-skip-verify",
						"--metrics-kusto-endpoints=Metrics=http://kustainer.default.svc.cluster.local:8080",
						"--logs-kusto-endpoints=Logs=http://kustainer.default.svc.cluster.local:8080",
					}
					statefulSet.Spec.Template.Spec.Affinity = nil
					statefulSet.Spec.Template.Spec.Tolerations = nil

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

					patchBytes, err := json.Marshal(statefulSet)
					if err != nil {
						return fmt.Errorf("failed to marshal statefulset: %w", err)
					}
					_, err = clientset.AppsV1().StatefulSets(statefulSet.Namespace).Patch(ctx, statefulSet.Name, types.ApplyPatchType, patchBytes, metav1.PatchOptions{
						FieldManager: "testcontainers",
					})
					if err != nil {
						return fmt.Errorf("failed to patch statefulset: %w", err)
					}

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
