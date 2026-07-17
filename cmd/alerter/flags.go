package main

import (
	"strings"

	"github.com/urfave/cli/v2"
)

func parseKustoEndpoints(values []string) (map[string]string, error) {
	endpoints := make(map[string]string)
	for _, value := range values {
		parts := strings.Split(value, "=")
		if len(parts) != 2 {
			return nil, cli.Exit("Invalid kusto-endpoint format, expected <name>=<endpoint>", 1)
		}
		endpoints[parts[0]] = parts[1]
	}
	return endpoints, nil
}

func parseTags(values []string) (map[string]string, error) {
	tags := make(map[string]string)
	for _, value := range values {
		parts := strings.Split(value, "=")
		if len(parts) != 2 {
			return nil, cli.Exit("Invalid tag format, expected <key>=<value>", 1)
		}
		tags[parts[0]] = parts[1]
	}
	return tags, nil
}

func addExecutionTags(tags map[string]string, region, cloud string) {
	tags["region"] = region
	tags["cloud"] = cloud
}
