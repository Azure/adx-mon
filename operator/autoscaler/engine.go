package autoscaler

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	ingestormetrics "github.com/Azure/adx-mon/pkg/metrics/ingestor"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	record "k8s.io/client-go/tools/record"
	"k8s.io/utils/clock"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	autoscalerEventScaleUp   = "IngestorScaleUp"
	autoscalerEventScaleDown = "IngestorScaleDown"
	autoscalerEventSkip      = "IngestorScaleSkip"

	shutdownRequestedAnnotation = "shutdown-requested"
	shutdownCompletedAnnotation = "shutdown-completed"

	shutdownTimeout          = 15 * time.Minute
	defaultRequeue           = time.Minute
	defaultInterval          = 5 * time.Minute
	defaultCPUWindow         = 15 * time.Minute
	defaultBaseGrowthPercent = int32(25)
	defaultCapPerStep        = int32(5)
	defaultScaleUp           = 70.0
	defaultScaleDown         = 40.0
)

// MetricsCollector represents the metrics abstraction used by the autoscaler.
type MetricsCollector interface {
	AverageCPU(ctx context.Context, window time.Duration) (float64, error)
}

// Engine executes autoscaling decisions for a single ingestor instance.
type Engine struct {
	client    ctrlclient.Client
	collector MetricsCollector
	recorder  record.EventRecorder
	clock     clock.Clock

	mu sync.Mutex
}

// Config captures the resolved autoscaler settings for an ingestor.
type Config struct {
	Enabled               bool
	MinReplicas           int32
	MaxReplicas           int32
	ScaleUpCPUThreshold   float64
	ScaleDownCPUThreshold float64
	ScaleInterval         time.Duration
	CPUWindow             time.Duration
	ScaleUpBasePercent    int32
	ScaleUpCapPerCycle    int32
	DryRun                bool
	CollectMetrics        bool
}

// NewEngine creates a new autoscaler engine.
func NewEngine(client ctrlclient.Client, collector MetricsCollector, recorder record.EventRecorder, clk clock.Clock) *Engine {
	if clk == nil {
		clk = clock.RealClock{}
	}
	return &Engine{
		client:    client,
		collector: collector,
		recorder:  recorder,
		clock:     clk,
	}
}

// Run executes a single autoscaler iteration and returns the suggested requeue interval.
func (e *Engine) Run(ctx context.Context, ingestor *adxmonv1.Ingestor) (time.Duration, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	sts := &appsv1.StatefulSet{}
	if err := e.client.Get(ctx, types.NamespacedName{Namespace: ingestor.Namespace, Name: ingestor.Name}, sts); err != nil {
		if apierrors.IsNotFound(err) {
			return defaultRequeue, e.writeStatus(ctx, ingestor, adxmonv1.AutoscalerActionSkip, 0, "statefulset not found", false)
		}
		return defaultRequeue, err
	}

	cfg := deriveConfig(ingestor, sts)

	if !cfg.Enabled {
		return cfg.ScaleInterval, e.writeStatus(ctx, ingestor, adxmonv1.AutoscalerActionNone, 0, "autoscaler disabled", false)
	}

	if !statefulSetSynced(sts) {
		return defaultRequeue, e.writeStatus(ctx, ingestor, adxmonv1.AutoscalerActionSkip, 0, "statefulset not yet synced", false)
	}

	cpu, err := e.collector.AverageCPU(ctx, cfg.CPUWindow)
	if err != nil {
		return defaultRequeue, e.writeStatus(ctx, ingestor, adxmonv1.AutoscalerActionSkip, 0, fmt.Sprintf("metrics unavailable: %v", err), false)
	}

	if !intervalElapsed(ingestor.Status.Autoscaler, e.clock.Now(), cfg.ScaleInterval) {
		return cfg.ScaleInterval, e.writeStatus(ctx, ingestor, adxmonv1.AutoscalerActionSkip, cpu, "scale interval not elapsed", false)
	}

	currentReplicas := int32(0)
	if sts.Spec.Replicas != nil {
		currentReplicas = *sts.Spec.Replicas
	}

	if cpu > cfg.ScaleUpCPUThreshold {
		return e.scaleUp(ctx, ingestor, sts, cfg, currentReplicas, cpu)
	}

	if cpu <= cfg.ScaleDownCPUThreshold {
		return e.scaleDown(ctx, ingestor, sts, cfg, currentReplicas, cpu)
	}

	return cfg.ScaleInterval, e.writeStatus(ctx, ingestor, adxmonv1.AutoscalerActionSkip, cpu, "cpu within thresholds", false)
}

func (e *Engine) scaleUp(ctx context.Context, ingestor *adxmonv1.Ingestor, sts *appsv1.StatefulSet, cfg Config, current int32, cpu float64) (time.Duration, error) {
	if current >= cfg.MaxReplicas {
		return cfg.ScaleInterval, e.writeStatus(ctx, ingestor, adxmonv1.AutoscalerActionSkip, cpu, "already at max replicas", false)
	}

	increase := int32(0)
	if cfg.ScaleUpBasePercent > 0 {
		increase = int32((int64(current)*int64(cfg.ScaleUpBasePercent) + 99) / 100)
	}
	if increase < 1 {
		increase = 1
	}
	if cfg.ScaleUpCapPerCycle > 0 && increase > cfg.ScaleUpCapPerCycle {
		increase = cfg.ScaleUpCapPerCycle
	}

	target := current + increase
	if target > cfg.MaxReplicas {
		target = cfg.MaxReplicas
	}
	if target == current {
		return cfg.ScaleInterval, e.writeStatus(ctx, ingestor, adxmonv1.AutoscalerActionSkip, cpu, "scale up calculation produced no change", false)
	}

	if cfg.DryRun {
		logger.Infof("[autoscaler] dry-run scale up for %s/%s: current=%d target=%d cpu=%.2f", ingestor.Namespace, ingestor.Name, current, target, cpu)
		return cfg.ScaleInterval, e.writeStatus(ctx, ingestor, adxmonv1.AutoscalerActionSkip, cpu, fmt.Sprintf("dry-run scale up to %d", target), false)
	}

	original := sts.DeepCopy()
	sts.Spec.Replicas = ptr.To(target)
	if err := e.client.Patch(ctx, sts, ctrlclient.MergeFrom(original)); err != nil {
		return cfg.ScaleInterval, err
	}

	e.recorder.Eventf(ingestor, corev1.EventTypeNormal, autoscalerEventScaleUp, "Scaled ingestor from %d to %d replicas (CPU %.2f%%)", current, target, cpu)
	logger.Infof("[autoscaler] scaled up %s/%s from %d to %d replicas (cpu %.2f%%)", ingestor.Namespace, ingestor.Name, current, target, cpu)

	return cfg.ScaleInterval, e.writeStatus(ctx, ingestor, adxmonv1.AutoscalerActionScaleUp, cpu, fmt.Sprintf("scaled up to %d replicas", target), true)
}

func (e *Engine) scaleDown(ctx context.Context, ingestor *adxmonv1.Ingestor, sts *appsv1.StatefulSet, cfg Config, current int32, cpu float64) (time.Duration, error) {
	if current <= cfg.MinReplicas {
		return cfg.ScaleInterval, e.writeStatus(ctx, ingestor, adxmonv1.AutoscalerActionSkip, cpu, "at or below min replicas", false)
	}

	pods, err := e.ingestorPods(ctx, ingestor)
	if err != nil {
		return cfg.ScaleInterval, err
	}

	annotated := findPodWithAnnotation(pods, shutdownRequestedAnnotation)
	now := e.clock.Now()

	if annotated != nil {
		if _, ok := annotated.Annotations[shutdownCompletedAnnotation]; ok {
			if cfg.DryRun {
				return cfg.ScaleInterval, e.writeStatus(ctx, ingestor, adxmonv1.AutoscalerActionSkip, cpu, "dry-run scale down delete", false)
			}

			if err := e.client.Delete(ctx, annotated); err != nil && !apierrors.IsNotFound(err) {
				return cfg.ScaleInterval, err
			}

			original := sts.DeepCopy()
			target := current - 1
			if target < cfg.MinReplicas {
				target = cfg.MinReplicas
			}
			sts.Spec.Replicas = ptr.To(target)
			if err := e.client.Patch(ctx, sts, ctrlclient.MergeFrom(original)); err != nil {
				return cfg.ScaleInterval, err
			}

			e.recorder.Eventf(ingestor, corev1.EventTypeNormal, autoscalerEventScaleDown, "Scaled ingestor from %d to %d replicas (CPU %.2f%%)", current, target, cpu)
			logger.Infof("[autoscaler] scaled down %s/%s from %d to %d replicas (cpu %.2f%%)", ingestor.Namespace, ingestor.Name, current, target, cpu)

			return cfg.ScaleInterval, e.writeStatus(ctx, ingestor, adxmonv1.AutoscalerActionScaleDown, cpu, fmt.Sprintf("scaled down to %d replicas", target), true)
		}

		reqTime, err := parseAnnotationTime(annotated.Annotations[shutdownRequestedAnnotation])
		if err != nil {
			logger.Warnf("[autoscaler] invalid shutdown-requested timestamp on pod %s/%s: %v", annotated.Namespace, annotated.Name, err)
		}

		if reqTime != nil && now.Sub(*reqTime) > shutdownTimeout {
			return cfg.ScaleInterval, fmt.Errorf("shutdown timeout exceeded for pod %s", annotated.Name)
		}

		return cfg.ScaleInterval, e.writeStatus(ctx, ingestor, adxmonv1.AutoscalerActionScaleDown, cpu, "waiting for shutdown completion", false)
	}

	candidate := highestOrdinalRunningPod(pods)
	if candidate == nil {
		return cfg.ScaleInterval, e.writeStatus(ctx, ingestor, adxmonv1.AutoscalerActionSkip, cpu, "no running pods available for scale down", false)
	}

	if cfg.DryRun {
		logger.Infof("[autoscaler] dry-run annotate shutdown for pod %s/%s", candidate.Namespace, candidate.Name)
		return cfg.ScaleInterval, e.writeStatus(ctx, ingestor, adxmonv1.AutoscalerActionScaleDown, cpu, fmt.Sprintf("dry-run annotate %s", candidate.Name), false)
	}

	if err := e.annotatePod(ctx, candidate, shutdownRequestedAnnotation, now.Format(time.RFC3339)); err != nil {
		return cfg.ScaleInterval, err
	}

	logger.Infof("[autoscaler] requested shutdown for pod %s/%s", candidate.Namespace, candidate.Name)
	return cfg.ScaleInterval, e.writeStatus(ctx, ingestor, adxmonv1.AutoscalerActionScaleDown, cpu, fmt.Sprintf("requested shutdown for %s", candidate.Name), false)
}

func (e *Engine) annotatePod(ctx context.Context, pod *corev1.Pod, key, value string) error {
	original := pod.DeepCopy()
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	pod.Annotations[key] = value
	return e.client.Patch(ctx, pod, ctrlclient.MergeFrom(original))
}

func (e *Engine) ingestorPods(ctx context.Context, ing *adxmonv1.Ingestor) ([]corev1.Pod, error) {
	podList := &corev1.PodList{}
	selector := ingestormetrics.DefaultLabelSelector()
	if err := e.client.List(ctx, podList,
		ctrlclient.InNamespace(ing.Namespace),
		ctrlclient.MatchingLabelsSelector{Selector: selector},
	); err != nil {
		return nil, err
	}

	pods := make([]corev1.Pod, 0, len(podList.Items))
	for _, pod := range podList.Items {
		if !strings.HasPrefix(pod.Name, ing.Name+"-") {
			continue
		}
		pods = append(pods, pod)
	}
	return pods, nil
}

func deriveConfig(ing *adxmonv1.Ingestor, sts *appsv1.StatefulSet) Config {
	cfg := Config{
		Enabled:               false,
		ScaleInterval:         defaultInterval,
		CPUWindow:             defaultCPUWindow,
		ScaleUpBasePercent:    defaultBaseGrowthPercent,
		ScaleUpCapPerCycle:    defaultCapPerStep,
		ScaleUpCPUThreshold:   defaultScaleUp,
		ScaleDownCPUThreshold: defaultScaleDown,
		CollectMetrics:        true,
	}

	replicas := int32(1)
	if ing.Spec.Replicas > 0 {
		replicas = ing.Spec.Replicas
	}
	if sts.Spec.Replicas != nil && *sts.Spec.Replicas > replicas {
		replicas = *sts.Spec.Replicas
	}

	if ing.Spec.Autoscaler == nil {
		cfg.MinReplicas = replicas
		cfg.MaxReplicas = replicas
		return cfg
	}

	a := ing.Spec.Autoscaler
	cfg.Enabled = a.Enabled

	if a.MinReplicas > 0 {
		cfg.MinReplicas = a.MinReplicas
	} else {
		cfg.MinReplicas = replicas
	}

	if a.MaxReplicas > 0 {
		cfg.MaxReplicas = a.MaxReplicas
	} else {
		cfg.MaxReplicas = replicas
	}

	if cfg.MaxReplicas < cfg.MinReplicas {
		cfg.MaxReplicas = cfg.MinReplicas
	}

	if a.ScaleUpCPUThreshold > 0 {
		cfg.ScaleUpCPUThreshold = float64(a.ScaleUpCPUThreshold)
	}
	if a.ScaleDownCPUThreshold > 0 {
		cfg.ScaleDownCPUThreshold = float64(a.ScaleDownCPUThreshold)
	}
	if a.ScaleInterval.Duration > 0 {
		cfg.ScaleInterval = a.ScaleInterval.Duration
	}
	if a.CPUWindow.Duration > 0 {
		cfg.CPUWindow = a.CPUWindow.Duration
	}
	if a.ScaleUpBasePercent >= 0 {
		cfg.ScaleUpBasePercent = a.ScaleUpBasePercent
	}
	if a.ScaleUpCapPerCycle > 0 {
		cfg.ScaleUpCapPerCycle = a.ScaleUpCapPerCycle
	}
	cfg.DryRun = a.DryRun
	cfg.CollectMetrics = a.CollectMetrics

	return cfg
}

func (e *Engine) writeStatus(ctx context.Context, ing *adxmonv1.Ingestor, action adxmonv1.IngestorAutoscalerAction, cpu float64, reason string, recordScale bool) error {
	var updated adxmonv1.IngestorAutoscalerStatus
	if ing.Status.Autoscaler != nil {
		updated = *ing.Status.Autoscaler
	}

	now := metav1.NewTime(e.clock.Now())
	updated.LastAction = action
	updated.LastActionTime = &now
	updated.LastObservedCPUPercentHundredths = int32(math.Round(cpu * 100))
	updated.Reason = reason
	if recordScale {
		updated.LastScaleTime = &now
	}

	if ing.Status.Autoscaler != nil && autoscalerStatusEqual(ing.Status.Autoscaler, &updated) {
		return nil
	}

	ing.Status.Autoscaler = &updated
	return e.client.Status().Update(ctx, ing)
}

func autoscalerStatusEqual(a, b *adxmonv1.IngestorAutoscalerStatus) bool {
	if a == nil || b == nil {
		return false
	}
	if a.LastAction != b.LastAction || a.Reason != b.Reason || a.LastObservedCPUPercentHundredths != b.LastObservedCPUPercentHundredths {
		return false
	}
	if !timesEqual(a.LastActionTime, b.LastActionTime) || !timesEqual(a.LastScaleTime, b.LastScaleTime) {
		return false
	}
	return true
}

func timesEqual(a, b *metav1.Time) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Time.Equal(b.Time)
}

func intervalElapsed(status *adxmonv1.IngestorAutoscalerStatus, now time.Time, interval time.Duration) bool {
	if interval <= 0 {
		return true
	}
	if status == nil || status.LastScaleTime == nil {
		return true
	}
	return now.Sub(status.LastScaleTime.Time) >= interval
}

func statefulSetSynced(sts *appsv1.StatefulSet) bool {
	if sts.Spec.Replicas == nil {
		return false
	}
	desired := *sts.Spec.Replicas
	return sts.Status.Replicas == desired && sts.Status.ReadyReplicas == desired
}

func parseAnnotationTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func findPodWithAnnotation(pods []corev1.Pod, key string) *corev1.Pod {
	for i := range pods {
		if pods[i].Annotations == nil {
			continue
		}
		if _, ok := pods[i].Annotations[key]; ok {
			return &pods[i]
		}
	}
	return nil
}

func highestOrdinalRunningPod(pods []corev1.Pod) *corev1.Pod {
	var (
		maxOrdinal = -1
		selected   *corev1.Pod
	)

	for i := range pods {
		pod := &pods[i]
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}
		ord, err := ordinalFromName(pod.Name)
		if err != nil {
			continue
		}
		if ord > maxOrdinal {
			maxOrdinal = ord
			selected = pod
		}
	}
	return selected
}

func ordinalFromName(name string) (int, error) {
	idx := strings.LastIndex(name, "-")
	if idx == -1 || idx == len(name)-1 {
		return 0, fmt.Errorf("invalid pod name %q", name)
	}
	return strconv.Atoi(name[idx+1:])
}
