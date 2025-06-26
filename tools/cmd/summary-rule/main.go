package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	v1 "github.com/Azure/adx-mon/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1).
			MarginBottom(1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			MarginTop(1)

	infoBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#874BFD")).
			Padding(1, 2).
			MarginBottom(1)

	queryBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#F25D94")).
			Padding(1, 2).
			MarginBottom(1)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575"))

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFAA00"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5555"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#777777"))

	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#333333")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1)
)

// Messages
type tickMsg time.Time
type dataMsg struct {
	rule          *v1.SummaryRule
	clusterLabels map[string]string
	timeInfo      TimeInfo
	renderedQuery string
	err           error
}

// Model for Bubble Tea
type model struct {
	client        ctrlclient.Client
	namespace     string
	name          string
	rule          *v1.SummaryRule
	clusterLabels map[string]string
	timeInfo      TimeInfo
	renderedQuery string
	lastUpdate    time.Time
	err           error
	width         int
	height        int
	ready         bool
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
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	// Create initial model
	m := model{
		client:    client,
		namespace: *namespace,
		name:      *name,
	}

	// Start Bubble Tea program
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running program: %v", err)
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchData(m.client, m.namespace, m.name), tick())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			// Manual refresh
			return m, fetchData(m.client, m.namespace, m.name)
		}

	case tickMsg:
		return m, tea.Batch(fetchData(m.client, m.namespace, m.name), tick())

	case dataMsg:
		m.rule = msg.rule
		m.clusterLabels = msg.clusterLabels
		m.timeInfo = msg.timeInfo
		m.renderedQuery = msg.renderedQuery
		m.err = msg.err
		m.lastUpdate = time.Now()
		return m, nil
	}

	return m, nil
}

func (m model) View() string {
	if !m.ready {
		return "Loading..."
	}

	var content []string

	// Title
	title := titleStyle.Width(m.width).Render(fmt.Sprintf("SummaryRule Monitor: %s/%s", m.namespace, m.name))
	content = append(content, title)

	if m.err != nil {
		errorBox := errorStyle.Render(fmt.Sprintf("Error: %v", m.err))
		content = append(content, errorBox)
		return strings.Join(content, "\n")
	}

	if m.rule == nil {
		content = append(content, "Loading rule information...")
		return strings.Join(content, "\n")
	}

	// Calculate responsive layout
	halfWidth := (m.width - 4) / 2

	// Rule configuration and timing side by side
	ruleInfo := m.renderRuleInfo(halfWidth)
	timeInfo := m.renderTimeInfo(halfWidth)
	topSection := lipgloss.JoinHorizontal(lipgloss.Top, ruleInfo, timeInfo)
	content = append(content, topSection)

	// Cluster labels (if any)
	if len(m.clusterLabels) > 0 {
		labelsInfo := m.renderClusterLabels(m.width - 4)
		content = append(content, labelsInfo)
	}

	// Rendered query
	queryInfo := m.renderQuery(m.width - 4)
	content = append(content, queryInfo)

	// Async operations and status side by side
	opsInfo := m.renderAsyncOperations(halfWidth)
	statusInfo := m.renderStatus(halfWidth)
	bottomSection := lipgloss.JoinHorizontal(lipgloss.Top, opsInfo, statusInfo)
	content = append(content, bottomSection)

	// Status bar
	statusBar := m.renderStatusBar()
	content = append(content, statusBar)

	return strings.Join(content, "\n")
}

func (m model) renderRuleInfo(width int) string {
	var info []string
	info = append(info, headerStyle.Render("📊 Rule Configuration"))
	info = append(info, fmt.Sprintf("Database: %s", successStyle.Render(m.rule.Spec.Database)))
	info = append(info, fmt.Sprintf("Table: %s", successStyle.Render(m.rule.Spec.Table)))
	info = append(info, fmt.Sprintf("Interval: %s", successStyle.Render(m.rule.Spec.Interval.Duration.String())))

	return infoBoxStyle.Width(width).Render(strings.Join(info, "\n"))
}

func (m model) renderTimeInfo(width int) string {
	var info []string
	info = append(info, headerStyle.Render("⏰ Execution Timing"))

	if m.timeInfo.LastExecutionTime != nil {
		info = append(info, fmt.Sprintf("Last: %s (%s ago)",
			m.timeInfo.LastExecutionTime.Format("15:04:05"),
			successStyle.Render(m.timeInfo.TimeSinceExecution)))
		info = append(info, fmt.Sprintf("Next: %s (in %s)",
			m.timeInfo.NextExecutionTime.Format("15:04:05"),
			warningStyle.Render(m.timeInfo.TimeUntilNext)))
	} else {
		info = append(info, dimStyle.Render("Last: Never"))
		info = append(info, dimStyle.Render("Next: When conditions are met"))
	}

	info = append(info, fmt.Sprintf("Window: %s to %s",
		m.timeInfo.CurrentWindowStart.Format("15:04:05"),
		m.timeInfo.CurrentWindowEnd.Format("15:04:05")))

	return infoBoxStyle.Width(width).Render(strings.Join(info, "\n"))
}

func (m model) renderClusterLabels(width int) string {
	var info []string
	info = append(info, headerStyle.Render("🏷️  Cluster Labels"))

	var keys []string
	for k := range m.clusterLabels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		info = append(info, fmt.Sprintf("%s: %s", dimStyle.Render(k), successStyle.Render(m.clusterLabels[k])))
	}

	return infoBoxStyle.Width(width).Render(strings.Join(info, "\n"))
}

func (m model) renderQuery(width int) string {
	var content []string
	content = append(content, headerStyle.Render("📝 Rendered Query (Current Time Window)"))

	// Wrap long lines
	lines := strings.Split(m.renderedQuery, "\n")
	maxLineWidth := width - 4 // Account for padding
	for _, line := range lines {
		if len(line) <= maxLineWidth {
			content = append(content, line)
		} else {
			// Simple word wrap
			for len(line) > maxLineWidth {
				content = append(content, line[:maxLineWidth])
				line = line[maxLineWidth:]
			}
			if len(line) > 0 {
				content = append(content, line)
			}
		}
	}

	return queryBoxStyle.Width(width).Render(strings.Join(content, "\n"))
}

func (m model) renderAsyncOperations(width int) string {
	var info []string
	info = append(info, headerStyle.Render("🔄 Outstanding Operations"))

	asyncOps := m.rule.GetAsyncOperations()
	if len(asyncOps) == 0 {
		info = append(info, dimStyle.Render("None"))
	} else {
		info = append(info, fmt.Sprintf("Count: %s", warningStyle.Render(fmt.Sprintf("%d", len(asyncOps)))))
		// Calculate available width for operation ID, considering "1. ", "...", and time info
		// Approximate length of time info: "   HH:MM:SS - HH:MM:SS (duration)" which is roughly 30-35 chars
		// "1. " is 3 chars. "..." is 3 chars if truncated.
		// Let's allocate a generous 40 chars for surrounding text and padding.
		opIDMaxWidth := width - 40
		if opIDMaxWidth < 8 { // Ensure at least 8 chars for truncated view
			opIDMaxWidth = 8
		}

		for i, op := range asyncOps {
			if i >= 3 { // Limit display to first 3
				info = append(info, dimStyle.Render(fmt.Sprintf("... and %d more", len(asyncOps)-3)))
				break
			}
			startTime, _ := time.Parse(time.RFC3339Nano, op.StartTime)
			endTime, _ := time.Parse(time.RFC3339Nano, op.EndTime)

			opIDDisplay := op.OperationId
			if len(op.OperationId) > opIDMaxWidth {
				opIDDisplay = op.OperationId[:opIDMaxWidth-3] + "..."
			}

			info = append(info, fmt.Sprintf("%d. %s", i+1, opIDDisplay))
			info = append(info, fmt.Sprintf("   %s - %s (%s)",
				startTime.Format("15:04:05"),
				endTime.Format("15:04:05"),
				dimStyle.Render(endTime.Sub(startTime).String())))
		}
	}

	return infoBoxStyle.Width(width).Render(strings.Join(info, "\n"))
}

func (m model) renderStatus(width int) string {
	var info []string
	info = append(info, headerStyle.Render("📋 Rule Status"))

	condition := m.rule.GetCondition()
	if condition != nil {
		var statusColor lipgloss.Style
		switch condition.Status {
		case "True":
			statusColor = successStyle
		case "False":
			statusColor = errorStyle
		default:
			statusColor = warningStyle
		}

		info = append(info, fmt.Sprintf("Status: %s", statusColor.Render(string(condition.Status))))
		if condition.Reason != "" {
			info = append(info, fmt.Sprintf("Reason: %s", dimStyle.Render(condition.Reason)))
		}
		if condition.Message != "" {
			message := condition.Message
			if len(message) > 50 {
				message = message[:47] + "..."
			}
			info = append(info, fmt.Sprintf("Message: %s", dimStyle.Render(message)))
		}
		info = append(info, fmt.Sprintf("Last Transition: %s",
			dimStyle.Render(condition.LastTransitionTime.Format("15:04:05"))))
		info = append(info, fmt.Sprintf("Generation: %s",
			dimStyle.Render(fmt.Sprintf("%d", condition.ObservedGeneration))))
	} else {
		info = append(info, dimStyle.Render("No condition set"))
	}

	return infoBoxStyle.Width(width).Render(strings.Join(info, "\n"))
}

func (m model) renderStatusBar() string {
	left := fmt.Sprintf("Last updated: %s", m.lastUpdate.Format("15:04:05"))
	right := "Press 'q' to quit, 'r' to refresh manually"

	gap := m.width - len(left) - len(right)
	if gap < 0 {
		gap = 0
	}

	statusText := left + strings.Repeat(" ", gap) + right
	return statusBarStyle.Width(m.width).Render(statusText)
}

// Commands
func tick() tea.Cmd {
	return tea.Tick(10*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func fetchData(client ctrlclient.Client, namespace, name string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		// Fetch SummaryRule
		rule := &v1.SummaryRule{}
		err := client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, rule)
		if err != nil {
			return dataMsg{err: err}
		}

		// Fetch cluster labels from ingestor StatefulSet
		clusterLabels, err := getClusterLabelsFromIngestor(ctx, client)
		if err != nil {
			// Non-fatal error, continue with empty labels
			clusterLabels = make(map[string]string)
		}

		// Calculate time information
		timeInfo := calculateTimeInfo(rule)

		// Render the query
		renderedQuery := renderQuery(rule.Spec.Body, timeInfo.CurrentWindowStart, timeInfo.CurrentWindowEnd, clusterLabels)

		return dataMsg{
			rule:          rule,
			clusterLabels: clusterLabels,
			timeInfo:      timeInfo,
			renderedQuery: renderedQuery,
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
	timeInfo.LastExecutionTime = rule.GetLastExecutionTime()

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
