package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"

	"text/template"

	"github.com/Azure/adx-mon/cmd/collector/config"
	"github.com/pelletier/go-toml/v2"
)

//go:embed config.md
var tmpl string

type Contents struct {
	Sections         []Section
	MetadataSections []Section
	ExporterSections []Section
}

type Section struct {
	Title       string
	Description string
	Config      ConfigEntry
}

type ConfigEntry interface {
	Validate() error
}

func main() {
	confBuffer := &bytes.Buffer{}
	confEncoder := toml.NewEncoder(confBuffer)
	confEncoder.SetIndentTables(true)
	confEncoder.SetArraysMultiline(true)

	funcMap := template.FuncMap{
		"configToToml": func(config interface{}) (string, error) {
			confBuffer.Reset()

			if err := confEncoder.Encode(config); err != nil {
				return "", fmt.Errorf("failed to encode config: %w", err)
			}

			return confBuffer.String(), nil
		},
	}

	contents := getContents()

	pageTemplate, err := template.New("").Funcs(funcMap).Parse(tmpl)
	if err != nil {
		panic(err)
	}

	output, err := os.Create("docs/config.md")
	if err != nil {
		panic(err)
	}
	if err := pageTemplate.Execute(output, contents); err != nil {
		panic(err)
	}
}

func getContents() Contents {
	return Contents{
		Sections: []Section{
			{
				Title:       "Global Config",
				Description: "This is the top level configuration for the collector. The only required fields are `Endpoint` and `StorageDir`.",
				Config: &config.Config{
					Endpoint:           "https://ingestor.adx-mon.svc.cluster.local",
					Kubeconfig:         ".kube/config",
					InsecureSkipVerify: true,
					ListenAddr:         ":8080",
					Region:             "eastus",
					TLSKeyFile:         "/etc/certs/collector.key",
					TLSCertFile:        "/etc/certs/collector.pem",

					MaxConnections:               100,
					MaxBatchSize:                 1000,
					MaxSegmentAgeSeconds:         30,
					MaxSegmentSize:               52428800,
					MaxDiskUsage:                 53687091200,
					MaxTransferConcurrency:       100,
					WALFlushIntervalMilliSeconds: 100,

					StorageDir:  "/var/lib/adx-mon",
					EnablePprof: true,

					DefaultDropMetrics: boolPtr(false),
					AddLabels: map[string]string{
						"collectedBy": "collector",
					},
					DropLabels: map[string]string{
						"^nginx_connections_accepted": "^pid$",
					},
					DropMetrics: []string{
						"^kube_pod_ips$",
						"etcd_grpc.*",
					},
					KeepMetrics: []string{
						"nginx.*",
					},
					KeepMetricsWithLabelValue: []config.LabelMatcher{
						{
							LabelRegex: "owner",
							ValueRegex: "platform",
						},
						{
							LabelRegex: "type",
							ValueRegex: "frontend|backend",
						},
					},
					LiftLabels: []*config.LiftLabel{
						{
							Name: "Host",
						},
						{
							Name:       "cluster_name",
							ColumnName: "Cluster",
						},
					},

					AddAttributes: map[string]string{
						"cluster": "cluster1",
						"geo":     "eu",
					},
					LiftAttributes: []string{
						"host",
					},

					Exporters: &config.Exporters{
						OtlpMetricExport: []*config.OtlpMetricExport{
							{
								Name:               "to-otlp",
								Destination:        "http://localhost:4318/v1/metrics",
								DefaultDropMetrics: boolPtr(true),
								AddLabels: map[string]string{
									"forwarded_to": "otlp",
								},
								DropLabels: map[string]string{
									"^kube_pod_ips$": "^ip_family",
								},
								KeepMetrics: []string{
									"^kube_pod_ips$",
								},
								AddResourceAttributes: map[string]string{
									"destination_namespace": "prod-metrics",
								},
							},
						},
					},
				},
			},
			{
				Title:       "Prometheus Scrape",
				Description: "Prometheus scrape discovers pods with the `adx-mon/scrape` annotation as well as any defined static scrape targets. It ships any metrics to the defined ADX database.",
				Config: &config.Config{
					PrometheusScrape: &config.PrometheusScrape{
						Database:              "Metrics",
						ScrapeIntervalSeconds: 10,
						ScrapeTimeout:         5,
						DropMetrics: []string{
							"^kube_pod_ips$",
							"etcd_grpc.*",
						},
						KeepMetrics: []string{
							"nginx.*",
						},
						KeepMetricsWithLabelValue: []config.LabelMatcher{
							{
								LabelRegex: "owner",
								ValueRegex: "platform",
							},
							{
								LabelRegex: "type",
								ValueRegex: "frontend|backend",
							},
						},
						StaticScrapeTarget: []*config.ScrapeTarget{
							{
								HostRegex: ".*",
								URL:       "http://localhost:9090/metrics",
								Namespace: "monitoring",
								Pod:       "host-monitor",
								Container: "host-monitor",
							},
						},
					}},
			},
			{
				Title:       "Prometheus Remote Write",
				Description: "Prometheus remote write accepts metrics from [Prometheus remote write protocol](https://prometheus.io/docs/specs/remote_write_spec/). It ships metrics to the defined ADX database.",
				Config: &config.Config{
					PrometheusRemoteWrite: []*config.PrometheusRemoteWrite{
						{
							Database: "Metrics",
							Path:     "/receive",
							AddLabels: map[string]string{
								"cluster": "cluster1",
							},
							DropLabels: map[string]string{
								"^nginx_connections_accepted": "^pid$",
							},
							DropMetrics: []string{
								"^kube_pod_ips$",
								"etcd_grpc.*",
							},
							KeepMetrics: []string{
								"nginx.*",
							},
							KeepMetricsWithLabelValue: []config.LabelMatcher{
								{
									LabelRegex: "owner",
									ValueRegex: "platform",
								},
								{
									LabelRegex: "type",
									ValueRegex: "frontend|backend",
								},
							},
						},
					},
				},
			},
			{
				Title:       "Otel Log",
				Description: "The Otel log endpoint accepts [OTLP/HTTP](https://opentelemetry.io/docs/specs/otlp/) logs from an OpenTelemetry sender. By default, this listens under the path `/v1/logs`.",
				Config: &config.Config{
					OtelLog: &config.OtelLog{
						AddAttributes: map[string]string{
							"cluster": "cluster1",
							"geo":     "eu",
						},
						LiftAttributes: []string{
							"host",
						},
						Transforms: []*config.LogTransform{
							{
								Name: "addattributes",
								Config: map[string]interface{}{
									"environment": "production",
								},
							},
						},
					},
				},
			},
			{
				Title:       "Otel Metrics",
				Description: "The Otel metrics endpoint accepts [OTLP/HTTP and/or OTLP/gRPC](https://opentelemetry.io/docs/specs/otlp/) metrics from an OpenTelemetry sender.",
				Config: &config.Config{
					OtelMetric: []*config.OtelMetric{
						{
							Database: "Metrics",
							Path:     "/v1/otlpmetrics",
							GrpcPort: 4317,
							AddLabels: map[string]string{
								"cluster": "cluster1",
							},
							DropLabels: map[string]string{
								"^nginx_connections_accepted": "^pid$",
							},
							DropMetrics: []string{
								"^kube_pod_ips$",
								"etcd_grpc.*",
							},
							KeepMetrics: []string{
								"nginx.*",
							},
							KeepMetricsWithLabelValue: []config.LabelMatcher{
								{
									LabelRegex: "owner",
									ValueRegex: "platform",
								},
								{
									LabelRegex: "type",
									ValueRegex: "frontend|backend",
								},
							},
						},
					},
				},
			},
			{
				Title:       "Host Log",
				Description: HostLogDescription,
				Config: &config.Config{
					HostLog: []*config.HostLog{
						{
							DisableKubeDiscovery: true,
							StaticPodTargets: []*config.PodTarget{
								{
									Namespace: "default",
									Name:      "myapp",
									LabelTargets: map[string]string{
										"app": "myapp",
									},
									Parsers:     []string{"json"},
									Destination: "Logs:MyApp",
								},
							},
							Transforms: []*config.LogTransform{
								{
									Name: "addattributes",
									Config: map[string]interface{}{
										"environment": "production",
									},
								},
							},
							StaticFileTargets: []*config.TailTarget{
								{
									FilePath: "/var/log/nginx/access.log",
									LogType:  "plain",
									Database: "Logs",
									Table:    "NginxAccess",
								},
								{
									FilePath: "/var/log/myservice/service.log",
									LogType:  "plain",
									Database: "Logs",
									Table:    "NginxAccess",
									Parsers: []string{
										"json",
									},
								},
							},
							JournalTargets: []*config.JournalTarget{
								{
									Matches: []string{
										"_SYSTEMD_UNIT=docker.service",
										"_TRANSPORT=journal",
									},
									Database: "Logs",
									Table:    "Docker",
								},
							},
							KernelTargets: []*config.KernelTarget{
								{
									Database: "Logs",
									Table:    "Kernel",
									Priority: "warning",
								},
							},
						},
					},
				},
			},
		},
		MetadataSections: []Section{
			{
				Title:       "Kubernetes Node Metadata Watching",
				Description: "Enable watching Kubernetes node labels and annotations to add them as labels to all metrics and logs. Requires both metadata-watch and add-metadata-labels sections to be configured.\n\n> **RBAC requirements**: The collector's service account must be able to `get`, `list`, and `watch` the core `nodes` resource. Without these permissions the watcher fails with a \"Failed to watch\" error when attempting to read node metadata.",
				Config: &config.Config{
					MetadataWatch: &config.MetadataWatch{
						KubernetesNode: &config.MetadataWatchKubernetesNode{},
					},
					AddMetadataLabels: &config.AddMetadataLabels{
						KubernetesNode: &config.AddMetadataKubernetesNode{
							Labels: map[string]string{
								"kubernetes.io/role":               "node_role",
								"node.kubernetes.io/instance-type": "instance_type",
							},
							Annotations: map[string]string{
								"cluster-autoscaler.kubernetes.io/safe-to-evict": "safe_to_evict",
								"node.alpha.kubernetes.io/ttl":                   "node_ttl",
							},
						},
					},
				},
			},
			{
				Title:       "Layered Metadata for Host Logs",
				Description: "Pair a global node metadata mapping with host log-specific aliases so different destinations receive tailored labels while still using the same watcher stream.",
				Config: &config.Config{
					MetadataWatch: &config.MetadataWatch{
						KubernetesNode: &config.MetadataWatchKubernetesNode{},
					},
					AddMetadataLabels: &config.AddMetadataLabels{
						KubernetesNode: &config.AddMetadataKubernetesNode{
							Labels: map[string]string{
								"topology.kubernetes.io/region": "region",
								"beta.kubernetes.io/os":         "os",
							},
							Annotations: map[string]string{
								"cluster-autoscaler.kubernetes.io/safe-to-evict": "global_safe_to_evict",
							},
						},
					},
					HostLog: []*config.HostLog{
						{
							StaticFileTargets: []*config.TailTarget{
								{
									FilePath: "/var/log/containers/frontend.log",
									LogType:  "kubernetes",
									Database: "Logs",
									Table:    "Frontend",
								},
							},
							AddMetadataLabels: &config.AddMetadataLabels{
								KubernetesNode: &config.AddMetadataKubernetesNode{
									Labels: map[string]string{
										"kubernetes.io/hostname":      "node_hostname",
										"topology.kubernetes.io/zone": "log_zone",
									},
									Annotations: map[string]string{
										"node.alpha.kubernetes.io/ttl": "log_node_ttl",
									},
								},
							},
						},
					},
				},
			},
			{
				Title:       "Metadata for Prometheus Scrape Metrics",
				Description: "Configure Prometheus scrape to expose zone and instance details with names tailored to downstream consumers without globally adding node labels",
				Config: &config.Config{
					MetadataWatch: &config.MetadataWatch{
						KubernetesNode: &config.MetadataWatchKubernetesNode{},
					},
					PrometheusScrape: &config.PrometheusScrape{
						Database:              "Metrics",
						ScrapeIntervalSeconds: 15,
						ScrapeTimeout:         10,
						StaticScrapeTarget: []*config.ScrapeTarget{
							{
								HostRegex: "frontend-.*",
								URL:       "http://frontend.monitoring.svc:9090/metrics",
								Namespace: "prod",
								Pod:       "frontend",
								Container: "web",
							},
						},
						AddMetadataLabels: &config.AddMetadataLabels{
							KubernetesNode: &config.AddMetadataKubernetesNode{
								Labels: map[string]string{
									"topology.kubernetes.io/zone":      "availability_zone",
									"node.kubernetes.io/instance-type": "machine_type",
								},
								Annotations: map[string]string{
									"cluster-autoscaler.kubernetes.io/safe-to-evict": "metric_safe_to_evict",
								},
							},
						},
					},
				},
			},
		},
		ExporterSections: []Section{
			{
				Title:       "Metric Exporters",
				Description: MetricExporterDescription,
				Config: &config.Config{
					Exporters: &config.Exporters{
						OtlpMetricExport: []*config.OtlpMetricExport{
							{
								Name:               "to-local-otlp",
								Destination:        "http://localhost:4318/v1/metrics",
								DefaultDropMetrics: boolPtr(true),
								AddLabels: map[string]string{
									"forwarded_to": "otlp",
								},
								DropLabels: map[string]string{
									"^kube_pod_ips$": "^ip_family",
								},
								KeepMetrics: []string{
									"^kube_pod_ips$",
								},
								AddResourceAttributes: map[string]string{
									"destination_namespace": "prod-metrics",
								},
							},
							{
								Name:               "to-remote-otlp",
								Destination:        "https://metrics.contoso.org/v1/metrics",
								DefaultDropMetrics: boolPtr(true),
								AddLabels: map[string]string{
									"forwarded_to": "otlp",
								},
								DropLabels: map[string]string{
									"^service_hit_count$": "^origin_ip$",
								},
								KeepMetrics: []string{
									"^service_hit_count$",
									"^service_latency$",
								},
								AddResourceAttributes: map[string]string{
									"destination_namespace": "primary-metrics",
								},
							},
						},
					},

					PrometheusScrape: &config.PrometheusScrape{
						Database:              "Metrics",
						ScrapeIntervalSeconds: 10,
						ScrapeTimeout:         5,
						Exporters: []string{
							"to-local-otlp",
							"to-remote-otlp",
						},
					},
				},
			},
			{
				Title:       "Log Exporters",
				Description: LogExporterDescription,
				Config: &config.Config{
					Exporters: &config.Exporters{
						FluentForwardLogExport: []*config.FluentForwardLogExport{
							{
								Name:         "fluentd-tcp",
								Destination:  "tcp://localhost:24224",
								TagAttribute: "fluent-output-tag-tcp",
							},
							{
								Name:         "fluentd-unix",
								Destination:  "unix:///var/run/fluent.sock",
								TagAttribute: "fluent-output-tag-unix",
							},
						},
					},
					HostLog: []*config.HostLog{
						{
							Exporters: []string{
								"fluentd-tcp",
								"fluentd-unix",
							},
						},
					},
				},
			},
		},
	}
}

func boolPtr(val bool) *bool {
	return &val
}

var HostLogDescription = "The host log config configures file and journald log collection. By default, Kubernetes pods with `adx-mon/log-destination` annotation will have their logs scraped and sent to the appropriate destinations.\n\n" +
	"### Log Type\n\n" +
	"The `log-type` setting defines the format of the underlying log file and determines how timestamps and log messages are extracted from structured log entries. This setting is used in `file-target` configurations to properly parse different log formats.\n\n" +
	"Available log types:\n\n" +
	"*   **`docker`**: Parses Docker JSON-formatted logs from the Docker daemon. These logs contain structured JSON with `log`, `stream`, and `time` fields.\n" +
	"*   **`cri`**: Parses Container Runtime Interface (CRI) formatted logs. These logs have a specific format with timestamp, stream type, and log content separated by spaces.\n" +
	"*   **`kubernetes`**: Auto-detects whether the log file is in Docker or CRI format and applies the appropriate parser automatically.\n" +
	"*   **`plain`**: Treats each line as plain text without any structured format. Uses the current timestamp as the log timestamp.\n" +
	"*   **unset/empty**: Defaults to plain text parsing when no log type is specified.\n\n" +
	"### Log Parsers\n\n" +
	"Parsers are used within `file-target`, `journal-target`, and in pod `adx-mon/log-parsers` annotations. These configurations configure processing the raw log message extracted from the source (e.g., a file line or a journald entry). They are defined in the `parsers` array and are applied sequentially.\n\n" +
	"The `parsers` array accepts a list of strings, each specifying a parser type. The collector attempts to apply each parser in the order they are listed. The first parser that successfully processes the log message stops the parsing process for that message. If a parser succeeds, the resulting fields are added to the log's body.\n\n" +
	"If no parser in the list succeeds, the original raw log message is kept in the `message` field of the log body.\n\n" +
	"Available parser types:\n\n" +
	"*   **`json`**: Attempts to parse the entire log message string as a JSON object. If successful, the key-value pairs from the JSON object are merged into the log body. The original `message` field is typically removed or overwritten by a field from the JSON payload if one exists with the key \"message\".\n" +
	"*   **`keyvalue`**: Parses log messages formatted as `key1=value1 key2=\"quoted value\" key3=value3 ...`. It extracts these key-value pairs and adds them to the log body. Keys and values are strings. Values containing spaces should be quoted.\n" +
	"*   **`space`**: Splits the log message string by whitespace (using `strings.Fields`, which handles multiple spaces, tabs, etc.). Each resulting part is added to the log body with keys named sequentially: `field0`, `field1`, `field2`, and so on. All resulting fields are strings.\n"

var MetricExporterDescription = "\n" +
	"Metrics currently support exporting to [OpenTelemetry OTLP/HTTP](https://opentelemetry.io/docs/specs/otlp/) endpoints with `otlp-metric-exporter`. The exporter can be configured to drop metrics by default, and only keep metrics that match a regex or have a specific label and value.\n\n" +
	"Metric collectors process metrics through their own metric filters and transforms prior to forwarding them to any defined exporters. The exporters then apply their own filters and transforms before sending the metrics to the destination.\n"

var LogExporterDescription = "\n" +
	"Logs currently support exporting to [fluent-forward](https://github.com/fluent/fluentd/wiki/Forward-Protocol-Specification-v1.5) tcp or unix domain socket endpoints with `fluent-forward-log-export`. This exporter forwards logs to the remote endpoint with a tag based on the value of the attribute `tag-attribute` within the log.\n\n" +
	"As an example, if 'tag-attribute' is set to 'fluent-output-tag', logs with an attribute of `fluent-output-tag` -> `service-logs` will be emitted with the tag `service-logs`. If the attribute is not present, the log will not be emitted by this exporter.\n"
