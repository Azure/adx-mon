package tail

import (
	"fmt"
	"strings"

	"github.com/Azure/adx-mon/collector/logs/sources/tail/sourceparse"
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
func getFileTargets(pod *v1.Pod, nodeName string, staticPodTargets []*StaticPodTargets) []FileTailTarget {
	// Only look for targets on our node
	if pod.Spec.NodeName != nodeName {
		return nil
	}

	if logger.IsDebug() {
		logger.Debugf("Checking for targets for pod %s/%s", pod.Namespace, pod.Name)
	}
	// Skip the pod if it has not opted in to scraping or are not static pods
	staticPodTarget := getStaticPod(pod, staticPodTargets)
	if !strings.EqualFold(getAnnotationOrDefault(pod, "adx-mon/scrape", "false"), "true") && staticPodTarget == nil {
		if logger.IsDebug() {
			logger.Debugf("Pod %s/%s has not opted in to scraping", pod.Namespace, pod.Name)
		}
		return nil
	}

	// Skip the pod if it does not have a log database or table configured
	// Destination in form of "database:table"
	dest := getAnnotationOrDefault(pod, AdxMonLogDestinationAnnotation, "")
	if staticPodTarget != nil && dest == "" {
		dest = staticPodTarget.Destination
	}

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
	if staticPodTarget != nil && parsers == "" {
		parsers = strings.Join(staticPodTarget.Parsers, ",")
	}
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
	namespaceName := pod.Namespace

	containerCount := len(pod.Spec.Containers) + len(pod.Spec.InitContainers) + len(pod.Spec.EphemeralContainers)
	targets := make([]FileTailTarget, 0, containerCount)
	// example name /var/log/containers/adx-reconciler-85f865d7b5-j5f49_adx-reconciler_adx-reconciler-ea96bea0582a986c502378aef429a275eb75c1d68e1c912ef93c2b5300990b04.log
	// podname_namespace_containername-containerid.log
	logFilePrefix := fmt.Sprintf("/var/log/containers/%s_%s", podName, namespaceName)
	for _, container := range pod.Spec.InitContainers {
		if target, ok := targetForContainer(pod, pod.Status.InitContainerStatuses, parserList, container.Name, logFilePrefix, podDB, podTable); ok {
			targets = append(targets, target)
		}
	}
	for _, container := range pod.Spec.Containers {
		if target, ok := targetForContainer(pod, pod.Status.ContainerStatuses, parserList, container.Name, logFilePrefix, podDB, podTable); ok {
			targets = append(targets, target)
		}
	}
	for _, container := range pod.Spec.EphemeralContainers {
		if target, ok := targetForContainer(pod, pod.Status.EphemeralContainerStatuses, parserList, container.Name, logFilePrefix, podDB, podTable); ok {
			targets = append(targets, target)
		}
	}
	return targets
}

func targetForContainer(pod *v1.Pod, containerStatuses []v1.ContainerStatus, parserList []string, containerName, logFilePrefix, podDB, podTable string) (FileTailTarget, bool) {
	containerId, ok := getContainerID(containerStatuses, containerName)
	if !ok {
		logger.Warnf("Failed to get container ID for container %s in pod %s/%s", containerName, pod.Namespace, pod.Name)
		return FileTailTarget{}, false
	}

	// containerId is <type>://<containerId>
	containerIdWithoutType := containerId
	slashIdx := strings.IndexByte(containerId, '/')
	if slashIdx != -1 && slashIdx+2 < len(containerId) {
		containerIdWithoutType = containerId[slashIdx+2:]
	}

	logFile := fmt.Sprintf("%s_%s-%s.log", logFilePrefix, containerName, containerIdWithoutType)
	if logger.IsDebug() {
		logger.Debugf("Found target: file:%s database:%s table:%s parsers:%v", logFile, podDB, podTable, parserList)
	}

	resourceValues := map[string]interface{}{
		"pod":         pod.Name,
		"namespace":   pod.Namespace,
		"container":   containerName,
		"containerID": containerId,
	}

	for k, v := range pod.GetAnnotations() {
		if !strings.HasPrefix(k, "adx-mon/") {
			key := fmt.Sprintf("annotation.%s", k)
			resourceValues[key] = v
		}
	}
	for k, v := range pod.GetLabels() {
		key := fmt.Sprintf("label.%s", k)
		resourceValues[key] = v
	}
	target := FileTailTarget{
		FilePath:  logFile,
		LogType:   sourceparse.LogTypeKubernetes,
		Database:  podDB,
		Table:     podTable,
		Parsers:   parserList,
		Resources: resourceValues,
	}
	return target, true
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

	if len(old.Resources) != len(new.Resources) {
		return true
	}
	for k, v := range old.Resources {
		if new.Resources[k] != v {
			return true
		}
	}

	return old.Database != new.Database || old.Table != new.Table
}

func getContainerID(containerStatuses []v1.ContainerStatus, containerName string) (string, bool) {
	for _, container := range containerStatuses {
		if container.Name == containerName {
			containerIdPopulated := container.ContainerID != ""
			return container.ContainerID, containerIdPopulated
		}
	}
	return "", false
}

func getStaticPod(pod *v1.Pod, staticPodTargets []*StaticPodTargets) *StaticPodTargets {
	for _, staticPod := range staticPodTargets {
		if checkStaticPodMatch(pod, staticPod) {
			return staticPod
		}
	}
	return nil
}

func checkStaticPodMatch(pod *v1.Pod, staticPod *StaticPodTargets) bool {
	if staticPod.Namespace != "" && pod.Namespace != staticPod.Namespace {
		return false
	}
	if staticPod.Name != "" && pod.Name != staticPod.Name {
		return false
	}
	for label, value := range staticPod.Labels {
		if pod.Labels[label] != value {
			return false
		}
	}
	return true
}
