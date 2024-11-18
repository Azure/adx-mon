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
	Sections []Section
}

type Section struct {
	Title       string
	Description string
	Config      interface{}
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

	contents := Contents{
		Sections: []Section{
			{
				Title:       "Global Config",
				Description: "This is the top level configuration for the collector. The only required fields are `Endpoint` and `StorageDir`.",
				Config: config.Config{
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
					WALFlushIntervalMilliSeconds: 100,

					StorageDir:  "/var/lib/adx-mon",
					EnablePprof: true,

					DefaultDropMetrics: boolPtr(false),
					AddLabels: map[string]string{
						"collectedBy": "collector",
					},
					DropLabels: map[string]string{
						"podname": ".*",
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
				},
			},
			{
				Title:       "Prometheus Scrape",
				Description: "Prometheus scrape discovers pods with the `adx-mon/scrape` annotation as well as any defined static scrape targets. It ships any metrics to the defined ADX database.",
				Config: config.Config{
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
				Config: config.Config{
					PrometheusRemoteWrite: []*config.PrometheusRemoteWrite{
						{
							Database: "Metrics",
							Path:     "/receive",
							AddLabels: map[string]string{
								"cluster": "cluster1",
							},
							DropLabels: map[string]string{
								"podname": ".*",
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
				Config: config.Config{
					OtelLog: &config.OtelLog{
						AddAttributes: map[string]string{
							"cluster": "cluster1",
							"geo":     "eu",
						},
						LiftAttributes: []string{
							"host",
						},
					},
				},
			},
			{
				Title:       "Otel Metrics",
				Description: "The Otel metrics endpoint accepts [OTLP/HTTP and/or OTLP/gRPC](https://opentelemetry.io/docs/specs/otlp/) metrics from an OpenTelemetry sender.",
				Config: config.Config{
					OtelMetric: []*config.OtelMetric{
						{
							Database: "Metrics",
							Path:     "/v1/otlpmetrics",
							GrpcPort: 4317,
							AddLabels: map[string]string{
								"cluster": "cluster1",
							},
							DropLabels: map[string]string{
								"podname": ".*",
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
				Description: "The host log config configures file and journald log collection. By default, Kubernetes pods with `adx-mon/log-destination` annotation will have their logs scraped and sent to the appropriate destinations.",
				Config: config.Config{
					HostLog: []*config.HostLog{
						{
							AddAttributes: map[string]string{
								"cluster": "cluster1",
								"geo":     "eu",
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
						},
					},
				},
			},
		},
	}

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

func boolPtr(val bool) *bool {
	return &val
}
