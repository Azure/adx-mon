package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	v1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

type SummaryRuleInfo struct {
	Rule          *v1.SummaryRule
	ClusterLabels map[string]string
	RenderedQuery string
	TimeInfo      TimeInfo
}

type TimeInfo struct {
	LastExecutionTime  *time.Time
	TimeSinceExecution string
	TimeUntilNext      string
	CurrentWindowStart time.Time
	CurrentWindowEnd   time.Time
	NextExecutionTime  time.Time
}

func main() {
	var (
		kubeconfig = flag.String("kubeconfig", "", "Path to kubeconfig file")
		namespace  = flag.String("namespace", "", "Namespace of the SummaryRule")
		name       = flag.String("name", "", "Name of the SummaryRule")
	)
	flag.Parse()

	if *namespace == "" || *name == "" {
		fmt.Fprintf(os.Stderr, "Usage: %s -namespace <namespace> -name <name> [-kubeconfig <path>]\n", os.Args[0])
		os.Exit(1)
	}

	// Set up Kubernetes client
	client, err := createKubeClient(*kubeconfig)
	if err != nil {
		logger.Errorf("Failed to create Kubernetes client: %v", err)
		os.Exit(1)
	}

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		logger.Infof("Received shutdown signal")
		cancel()
	}()

	// Main monitoring loop
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Display initial information
	displaySummaryRuleInfo(ctx, client, *namespace, *name)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Clear screen and redisplay
			fmt.Print("\033[H\033[2J")
			displaySummaryRuleInfo(ctx, client, *namespace, *name)
		}
	}
}

func createKubeClient(kubeconfig string) (ctrlclient.Client, error) {
	scheme := clientgoscheme.Scheme
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add client-go scheme: %w", err)
	}
	if err := v1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add adx-mon scheme: %w", err)
	}

	var config *rest.Config
	var err error

	if kubeconfig == "" {
		// Try in-cluster config first
		config, err = rest.InClusterConfig()
		if err != nil {
			// Fall back to default kubeconfig
			config, err = clientcmd.BuildConfigFromFlags("", "")
		}
	} else {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	client, err := ctrlclient.New(config, ctrlclient.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create controller client: %w", err)
	}

	return client, nil
}

func displaySummaryRuleInfo(ctx context.Context, client ctrlclient.Client, namespace, name string) {
	// Fetch SummaryRule
	rule := &v1.SummaryRule{}
	err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, rule)
	if err != nil {
		fmt.Printf("Error fetching SummaryRule %s/%s: %v\n", namespace, name, err)
		return
	}

	// Fetch cluster labels from ingestor StatefulSet
	clusterLabels, err := getClusterLabelsFromIngestor(ctx, client)
	if err != nil {
		fmt.Printf("Warning: Failed to get cluster labels from ingestor: %v\n", err)
		clusterLabels = make(map[string]string)
	}

	// Calculate time information
	timeInfo := calculateTimeInfo(rule)

	// Render the query
	renderedQuery := renderQuery(rule.Spec.Body, timeInfo.CurrentWindowStart, timeInfo.CurrentWindowEnd, clusterLabels)

	// Display header
	fmt.Printf("╔════════════════════════════════════════════════════════════════════════════════════════════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║ SummaryRule Monitor: %s/%s%s║\n", namespace, name, strings.Repeat(" ", 120-len(namespace)-len(name)-24))
	fmt.Printf("║ Last Updated: %s%s║\n", time.Now().Format("2006-01-02 15:04:05 MST"), strings.Repeat(" ", 95))
	fmt.Printf("╚════════════════════════════════════════════════════════════════════════════════════════════════════════════════════════════════════════════════════╝\n\n")

	// Display rule basic information
	fmt.Printf("📊 Rule Configuration:\n")
	fmt.Printf("   Database: %s\n", rule.Spec.Database)
	fmt.Printf("   Table: %s\n", rule.Spec.Table)
	fmt.Printf("   Interval: %s\n", rule.Spec.Interval.Duration.String())
	fmt.Printf("\n")

	// Display timing information
	fmt.Printf("⏰ Execution Timing:\n")
	if timeInfo.LastExecutionTime != nil {
		fmt.Printf("   Last Execution: %s (%s ago)\n",
			timeInfo.LastExecutionTime.Format("2006-01-02 15:04:05 MST"),
			timeInfo.TimeSinceExecution)
		fmt.Printf("   Next Execution: %s (in %s)\n",
			timeInfo.NextExecutionTime.Format("2006-01-02 15:04:05 MST"),
			timeInfo.TimeUntilNext)
	} else {
		fmt.Printf("   Last Execution: Never\n")
		fmt.Printf("   Next Execution: When conditions are met\n")
	}
	fmt.Printf("   Current Window: %s to %s\n",
		timeInfo.CurrentWindowStart.Format("2006-01-02 15:04:05 MST"),
		timeInfo.CurrentWindowEnd.Format("2006-01-02 15:04:05 MST"))
	fmt.Printf("\n")

	// Display cluster labels if any
	if len(clusterLabels) > 0 {
		fmt.Printf("🏷️  Cluster Labels:\n")
		var keys []string
		for k := range clusterLabels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("   %s: %s\n", k, clusterLabels[k])
		}
		fmt.Printf("\n")
	}

	// Display rendered query
	fmt.Printf("📝 Rendered Query (Current Time Window):\n")
	fmt.Printf("┌────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┐\n")
	queryLines := strings.Split(renderedQuery, "\n")
	for _, line := range queryLines {
		if len(line) > 138 {
			line = line[:135] + "..."
		}
		fmt.Printf("│ %-138s │\n", line)
	}
	fmt.Printf("└────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────┘\n\n")

	// Display async operations
	asyncOps := rule.GetAsyncOperations()
	if len(asyncOps) > 0 {
		fmt.Printf("🔄 Outstanding Async Operations (%d):\n", len(asyncOps))
		for i, op := range asyncOps {
			startTime, _ := time.Parse(time.RFC3339Nano, op.StartTime)
			endTime, _ := time.Parse(time.RFC3339Nano, op.EndTime)
			fmt.Printf("   %d. Operation ID: %s\n", i+1, op.OperationId)
			fmt.Printf("      Time Window: %s to %s\n",
				startTime.Format("2006-01-02 15:04:05"),
				endTime.Format("2006-01-02 15:04:05"))
			fmt.Printf("      Duration: %s\n", endTime.Sub(startTime).String())
			if i < len(asyncOps)-1 {
				fmt.Printf("\n")
			}
		}
	} else {
		fmt.Printf("🔄 Outstanding Async Operations: None\n")
	}

	// Display rule status
	fmt.Printf("\n📋 Rule Status:\n")
	condition := rule.GetCondition()
	if condition != nil {
		fmt.Printf("   Status: %s\n", condition.Status)
		if condition.Reason != "" {
			fmt.Printf("   Reason: %s\n", condition.Reason)
		}
		if condition.Message != "" {
			message := condition.Message
			if len(message) > 100 {
				message = message[:97] + "..."
			}
			fmt.Printf("   Message: %s\n", message)
		}
		fmt.Printf("   Last Transition: %s\n", condition.LastTransitionTime.Format("2006-01-02 15:04:05 MST"))
		fmt.Printf("   Observed Generation: %d\n", condition.ObservedGeneration)
	} else {
		fmt.Printf("   Status: No condition set\n")
	}

	fmt.Printf("\n" + strings.Repeat("─", 140) + "\n")
	fmt.Printf("Press Ctrl+C to exit. Refreshing every 10 seconds...\n")
}

func getClusterLabelsFromIngestor(ctx context.Context, client ctrlclient.Client) (map[string]string, error) {
	// Get the ingestor StatefulSet from adx-mon namespace
	sts := &appsv1.StatefulSet{}
	err := client.Get(ctx, types.NamespacedName{Namespace: "adx-mon", Name: "ingestor"}, sts)
	if err != nil {
		return nil, fmt.Errorf("failed to get ingestor StatefulSet: %w", err)
	}

	// Parse cluster labels from container arguments
	clusterLabels := make(map[string]string)
	if len(sts.Spec.Template.Spec.Containers) > 0 {
		args := sts.Spec.Template.Spec.Containers[0].Args
		for i, arg := range args {
			if arg == "--cluster-labels" && i+1 < len(args) {
				labelPair := args[i+1]
				if strings.Contains(labelPair, "=") {
					parts := strings.SplitN(labelPair, "=", 2)
					if len(parts) == 2 {
						clusterLabels[parts[0]] = parts[1]
					}
				}
			} else if strings.HasPrefix(arg, "--cluster-labels=") {
				labelPair := strings.TrimPrefix(arg, "--cluster-labels=")
				if strings.Contains(labelPair, "=") {
					parts := strings.SplitN(labelPair, "=", 2)
					if len(parts) == 2 {
						clusterLabels[parts[0]] = parts[1]
					}
				}
			}
		}
	}

	return clusterLabels, nil
}

func calculateTimeInfo(rule *v1.SummaryRule) TimeInfo {
	now := time.Now().UTC()
	alignedNow := now.Truncate(time.Minute)

	timeInfo := TimeInfo{}

	// Get last successful execution time
	timeInfo.LastExecutionTime = rule.GetLastSuccessfulExecutionTime()

	// Calculate current window based on the same logic as SummaryRuleTask
	if timeInfo.LastExecutionTime == nil {
		// First execution: start from current time aligned to interval boundary, going back one interval
		timeInfo.CurrentWindowEnd = alignedNow
		timeInfo.CurrentWindowStart = timeInfo.CurrentWindowEnd.Add(-rule.Spec.Interval.Duration)
		timeInfo.NextExecutionTime = alignedNow
	} else {
		// Subsequent executions: start from where the last successful execution ended
		timeInfo.CurrentWindowStart = *timeInfo.LastExecutionTime
		timeInfo.CurrentWindowEnd = timeInfo.CurrentWindowStart.Add(rule.Spec.Interval.Duration)

		// Ensure we don't execute future windows
		if timeInfo.CurrentWindowEnd.After(alignedNow) {
			timeInfo.CurrentWindowEnd = alignedNow
		}

		// Calculate next execution time
		timeInfo.NextExecutionTime = timeInfo.LastExecutionTime.Add(rule.Spec.Interval.Duration)

		// Calculate time since last execution
		timeSince := now.Sub(*timeInfo.LastExecutionTime)
		timeInfo.TimeSinceExecution = formatDuration(timeSince)

		// Calculate time until next execution
		timeUntil := timeInfo.NextExecutionTime.Sub(now)
		if timeUntil < 0 {
			timeInfo.TimeUntilNext = "overdue"
		} else {
			timeInfo.TimeUntilNext = formatDuration(timeUntil)
		}
	}

	return timeInfo
}

func renderQuery(body string, startTime, endTime time.Time, clusterLabels map[string]string) string {
	// Apply substitutions using the same logic as applySubstitutions in tasks.go
	var letStatements []string

	// Add time parameter definitions
	startTimeStr := startTime.Format("2006-01-02T15:04:05.999999999Z")
	endTimeStr := endTime.Format("2006-01-02T15:04:05.999999999Z")
	letStatements = append(letStatements, fmt.Sprintf("let _startTime=datetime(%s);", startTimeStr))
	letStatements = append(letStatements, fmt.Sprintf("let _endTime=datetime(%s);", endTimeStr))

	// Add cluster label parameter definitions
	var keys []string
	for k := range clusterLabels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := clusterLabels[k]
		// Escape any double quotes in the value
		escapedValue := strconv.Quote(v)
		letStatements = append(letStatements, fmt.Sprintf("let _%s=%s;", k, escapedValue))
	}

	// Construct the full query with let statements
	query := fmt.Sprintf("%s\n%s",
		strings.Join(letStatements, "\n"),
		strings.TrimSpace(body))

	return query
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	} else if d < time.Hour {
		return fmt.Sprintf("%.0fm", d.Minutes())
	} else if d < 24*time.Hour {
		hours := int(d.Hours())
		minutes := int(d.Minutes()) % 60
		if minutes == 0 {
			return fmt.Sprintf("%dh", hours)
		}
		return fmt.Sprintf("%dh%dm", hours, minutes)
	} else {
		days := int(d.Hours()) / 24
		hours := int(d.Hours()) % 24
		if hours == 0 {
			return fmt.Sprintf("%dd", days)
		}
		return fmt.Sprintf("%dd%dh", days, hours)
	}
}
