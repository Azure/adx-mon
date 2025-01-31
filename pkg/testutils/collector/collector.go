//go:build !disableDocker

package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	kwait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultImage = "collector"
	DefaultTag   = "latest"
)

type CollectorContainer struct {
	testcontainers.Container
}

func Run(ctx context.Context, img string, opts ...testcontainers.ContainerCustomizer) (*CollectorContainer, error) {
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
				Dockerfile: "build/images/Dockerfile.collector",
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
	var c *CollectorContainer
	if container != nil {
		c = &CollectorContainer{Container: container}
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
		isolateKinds := []string{"DaemonSet", "ConfigMap", "Deployment"}
		writeTo, err := os.MkdirTemp("", "collector")
		if err != nil {
			return fmt.Errorf("failed to create temp dir: %w", err)
		}

		var img string
		if req.FromDockerfile.Context != "" {
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

					if err := testutils.ExtractManifests(writeTo, "build/k8s/collector.yaml", isolateKinds); err != nil {
						return fmt.Errorf("failed to extract manifests: %w", err)
					}

					manifestsPath := filepath.Join(writeTo, "manifests.yaml")
					containerPath := filepath.Join(testutils.K3sManifests, "collector-manifests.yaml")
					if err := k.CopyFileToContainer(ctx, manifestsPath, containerPath, 0644); err != nil {
						return fmt.Errorf("failed to copy file to container: %w", err)
					}

					return testutils.InstallFunctionsCrd(ctx, k)
				},
			},
			PostCreates: []testcontainers.ContainerHook{
				func(ctx context.Context, c testcontainers.Container) error {
					// Deserialize our statefulset manifest and customize it to our needs
					dsPath := filepath.Join(writeTo, "DaemonSet.yaml")
					dsData, err := os.ReadFile(dsPath)
					if err != nil {
						return fmt.Errorf("failed to read file: %w", err)
					}

					var daemonset appsv1.DaemonSet
					if err := yaml.Unmarshal(dsData, &daemonset); err != nil {
						return fmt.Errorf("failed to unmarshal daemonset: %w", err)
					}
					daemonset.Spec.Template.Spec.Tolerations = nil
					daemonset.Spec.Template.Spec.Containers[0].Image = img
					daemonset.Spec.Template.Spec.Containers[0].ImagePullPolicy = corev1.PullNever

					daemonset.Spec.Template.Spec.Volumes = slices.DeleteFunc(daemonset.Spec.Template.Spec.Volumes, func(v corev1.Volume) bool {
						return v.Name == "etcmachineid"
					})
					daemonset.Spec.Template.Spec.Containers[0].VolumeMounts = slices.DeleteFunc(daemonset.Spec.Template.Spec.Containers[0].VolumeMounts, func(v corev1.VolumeMount) bool {
						return v.Name == "etcmachineid"
					})

					// Wait for our manifests to be applied
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
						ns, err := clientset.CoreV1().Namespaces().Get(ctx, daemonset.GetNamespace(), metav1.GetOptions{})
						return ns != nil && err == nil, nil
					})
					if err != nil {
						return fmt.Errorf("failed to wait for namespace: %w", err)
					}

					patchBytes, err := json.Marshal(collectorConfigMap)
					if err != nil {
						return fmt.Errorf("failed to marshal configmap: %w", err)
					}
					_, err = clientset.CoreV1().ConfigMaps(collectorConfigMap.Namespace).Patch(ctx, collectorConfigMap.Name, types.ApplyPatchType, patchBytes, metav1.PatchOptions{
						FieldManager: "testcontainers",
					})
					if err != nil {
						return fmt.Errorf("failed to patch configmap: %w", err)
					}

					patchBytes, err = json.Marshal(daemonset)
					if err != nil {
						return fmt.Errorf("failed to marshal daemonset: %w", err)
					}
					_, err = clientset.AppsV1().DaemonSets(daemonset.Namespace).Patch(ctx, daemonset.Name, types.ApplyPatchType, patchBytes, metav1.PatchOptions{
						FieldManager: "testcontainers",
					})
					if err != nil {
						return fmt.Errorf("failed to patch daemonset: %w", err)
					}

					if err := ctrlCli.Get(ctx, types.NamespacedName{Namespace: collectorFunction.Namespace, Name: collectorFunction.Name}, collectorFunction); err != nil {
						if err := ctrlCli.Create(ctx, collectorFunction); err != nil {
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

var collectorFunction = &adxmonv1.Function{
	TypeMeta: metav1.TypeMeta{
		APIVersion: "adx-mon.azure.com/v1",
		Kind:       "Function",
	},
	ObjectMeta: metav1.ObjectMeta{
		Name:      "collector",
		Namespace: "adx-mon",
	},
	Spec: adxmonv1.FunctionSpec{
		Database: "Logs",
		Body: `.create-or-alter function with (view=true, folder='views') Collector () {
  table('Collector')
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

// collectorConfigMap is a minimal config suitable for use by kustainer
var collectorConfigMap = &corev1.ConfigMap{
	TypeMeta: metav1.TypeMeta{
		APIVersion: "v1",
		Kind:       "ConfigMap",
	},
	ObjectMeta: metav1.ObjectMeta{
		Name:      "collector-config",
		Namespace: "adx-mon",
	},
	Data: map[string]string{
		"config.toml": `
# We have to keep our scape targets very light due to the capacity
# constraints of kustainer

# Ingestor URL to send collected telemetry.
endpoint = 'https://ingestor.adx-mon.svc.cluster.local'

# Region is a location identifier
region = '$REGION'

# Skip TLS verification.
insecure-skip-verify = true

# Address to listen on for endpoints.
listen-addr = ':8080'

# Maximum number of connections to accept.
max-connections = 100

# Maximum number of samples to send in a single batch.
max-batch-size = 10000

# Storage directory for the WAL.
storage-dir = '/mnt/data'

# Regexes of metrics to drop from all sources.
drop-metrics = []

keep-metrics = ['^adxmon.*']

# Disable metrics forwarding to endpoints.
disable-metrics-forwarding = false

# Key/value pairs of labels to add to all metrics and logs.
[add-labels]
  host = '$(HOSTNAME)'
  cluster = '$CLUSTER'

# Defines a prometheus scrape endpoint.
[prometheus-scrape]

  # Database to store metrics in.
  database = 'Metrics'

  default-drop-metrics = false

  # Defines a static scrape target.
  static-scrape-target = [
    # Scrape our own metrics
    { host-regex = '.*', url = 'http://$(HOSTNAME):3100/metrics', namespace = 'adx-mon', pod = 'collector', container = 'collector' },
  ]

  # Scrape interval in seconds.
  scrape-interval = 30

  # Scrape timeout in seconds.
  scrape-timeout = 25

  # Disable metrics forwarding to endpoints.
  disable-metrics-forwarding = false

  # Regexes of metrics to keep from scraping source.
  keep-metrics = ['^adxmon.*', '^sample.*']

  # Regexes of metrics to drop from scraping source.
  drop-metrics = ['^go.*', '^process.*', '^promhttp.*']

# Defines a prometheus remote write endpoint.
[[prometheus-remote-write]]

  # Database to store metrics in.
  database = 'Metrics'

  # The path to listen on for prometheus remote write requests.  Defaults to /receive.
  path = '/receive'

  # Regexes of metrics to drop.
  drop-metrics = []

  # Disable metrics forwarding to endpoints.
  disable-metrics-forwarding = false

  # Key/value pairs of labels to add to this source.
  [prometheus-remote-write.add-labels]

# Defines an OpenTelemetry log endpoint.
[otel-log]
  # Attributes lifted from the Body and added to Attributes.
  lift-attributes = ['kusto.database', 'kusto.table']

[[host-log]]
  parsers = ['json']

  journal-target = [
    # matches are optional and are parsed like MATCHES in journalctl.
    # If different fields are matched, only entries matching all terms are included.
    # If the same fields are matched, entries matching any term are included.
    # + can be added between to include a disjunction of terms.
    # See examples under man 1 journalctl
    { matches = [ '_SYSTEMD_UNIT=kubelet.service' ], database = 'Logs', table = 'Kubelet' }
  ]
`,
	},
}
