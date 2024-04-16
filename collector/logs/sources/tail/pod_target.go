package tail

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Azure/adx-mon/collector/logs/transforms/parser"
	"github.com/Azure/adx-mon/pkg/logger"
	v1 "k8s.io/api/core/v1"
)

const (
	// Defined as <DBName>:<Table>
	AdxMonLogDestinationAnnotation = "adx-mon/log-destination"
	// Defined as comma separated parser names
	AdxMonLogParsersAnnotation = "adx-mon/log-parsers"
)

// getFileTargets generates a list of FileTailTargets for the containers within a given pod
func getFileTargets(pod *v1.Pod, nodeName string) []FileTailTarget {
	// Only look for targets on our node
	if pod.Spec.NodeName != nodeName {
		return nil
	}

	if logger.IsDebug() {
		logger.Debugf("Checking for targets for pod %s/%s", pod.Namespace, pod.Name)
	}
	// Skip the pod if it has not opted in to scraping
	if !strings.EqualFold(getAnnotationOrDefault(pod, "adx-mon/scrape", "false"), "true") {
		if logger.IsDebug() {
			logger.Debugf("Pod %s/%s has not opted in to scraping", pod.Namespace, pod.Name)
		}
		return nil
	}

	// Skip the pod if it does not have a log database or table configured
	// Destination in form of "database:table"
	dest := getAnnotationOrDefault(pod, AdxMonLogDestinationAnnotation, "")
	destPair := strings.Split(dest, ":")
	if len(destPair) != 2 {
		if logger.IsDebug() {
			logger.Debugf("Pod %s/%s has no log destination configured", pod.Namespace, pod.Name)
		}
		return nil
	}
	podDB := destPair[0]
	podTable := destPair[1]
	if podDB == "" || podTable == "" {
		if logger.IsDebug() {
			logger.Debugf("Pod %s/%s has no log destination configured", pod.Namespace, pod.Name)
		}
		return nil
	}

	parserList := []string{}
	parsers := getAnnotationOrDefault(pod, AdxMonLogParsersAnnotation, "")
	if parsers != "" {
		parserList = strings.Split(parsers, ",")
		for _, currParser := range parserList {
			if !parser.IsValidParser(currParser) {
				logger.Warnf("Invalid parser %s for pod %s/%s", currParser, pod.Namespace, pod.Name)
				return nil
			}
		}
	}

	podName := pod.Name
	podUid := pod.ObjectMeta.UID
	namespaceName := pod.Namespace

	targets := make([]FileTailTarget, 0, len(pod.Spec.Containers))
	baseDir := fmt.Sprintf("/var/log/pods/%s_%s_%s", namespaceName, podName, podUid)
	// TODO - other fields as well contain containers
	for _, container := range pod.Spec.Containers {
		logFile := filepath.Join(baseDir, container.Name, "0.log")
		if logger.IsDebug() {
			logger.Debugf("Found target: file:%s database:%s table:%s parsers:%v", logFile, podDB, podTable, parserList)
		}
		targets = append(targets, FileTailTarget{
			FilePath: logFile,
			LogType:  LogTypeDocker,
			Database: podDB,
			Table:    podTable,
			Parsers:  parserList,
			Attributes: map[string]interface{}{
				"pod":       podName,
				"namespace": namespaceName,
				"container": container.Name,
			},
		})
	}
	return targets
}

func getAnnotationOrDefault(p *v1.Pod, key, def string) string {
	if value, ok := p.Annotations[key]; ok && value != "" {
		return value
	}
	return def
}

func isTargetChanged(old, new FileTailTarget) bool {
	if len(old.Parsers) != len(new.Parsers) {
		return true
	}

	for i := range old.Parsers {
		if old.Parsers[i] != new.Parsers[i] {
			return true
		}
	}

	if len(old.Attributes) != len(new.Attributes) {
		return true
	}
	for k, v := range old.Attributes {
		if new.Attributes[k] != v {
			return true
		}
	}

	return old.Database != new.Database || old.Table != new.Table
}
