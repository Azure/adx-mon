package cluster

import (
	"context"
	"fmt"
	"maps"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/otlp"
	"github.com/Azure/adx-mon/pkg/service"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v12 "k8s.io/client-go/listers/core/v1"
)

type OTLPLogsWriter func(ctx context.Context, database, table string, logs *otlp.Logs) error

type Coordinator interface {
	MetricPartitioner
	service.Component

	// IsLeader returns true if the current node is the leader.
	IsLeader() bool

	// Peers returns the number of ingestor peers and this node's rank among them.
	Peers() PeerInfo

	// SubscribePeers registers fn to be called when the peer count or this node's rank changes.  fn is called
	// synchronously and must return quickly.  The returned func unsubscribes fn.
	SubscribePeers(fn func(PeerInfo)) (unsubscribe func())
}

// PeerInfo describes this node's position among its ingestor peers.
type PeerInfo struct {
	// Count is the number of ready peers including this node.
	Count int

	// Rank is this node's zero based index among the peers sorted by name.  Every peer computes the same ordering so
	// ranks are unique across peers that observe the same peer set.
	Rank int
}

// Coordinator manages the cluster state and writes to the correct peer.
type coordinator struct {
	opts      *CoordinatorOpts
	namespace string

	mu    sync.RWMutex
	peers map[string]string
	part  *Partitioner

	factory informers.SharedInformerFactory

	kcli         kubernetes.Interface
	pl           v12.PodLister
	hostEntpoint string
	hostname     string
	groupName    string
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	leader       bool

	// peerInfo is the last computed peer info.  It is guarded by mu.
	peerInfo PeerInfo

	// expiry re-syncs the peers when the next not-ready member's grace period ends, so every coordinator removes it
	// at about the same time rather than at its next periodic resync.  It is guarded by mu.
	expiry *time.Timer
	// done is closed when the coordinator is closed.
	done <-chan struct{}

	subMu       sync.Mutex
	subscribers map[int]func(PeerInfo)
	nextSubID   int
}

type CoordinatorOpts struct {
	K8sCli kubernetes.Interface

	// Namespace is the namespace used to discover peers.  If not specified, the coordinator will
	// try to use the namespace of the current pod.
	Namespace string

	// Hostname is the hostname of the current node.  This should be the statefulset hostname format
	// in order to discover peers correctly
	Hostname string

	// InsecureSkipVerify controls whether a client verifies the server's certificate chain and host name.
	InsecureSkipVerify bool

	// PartitionSize is the max size of the group of nodes forming a partition.  A partition is a set of nodes where
	// keys are distributed.
	// NOTE: This is not used in the current implementation.
	PartitionSize int
}

func NewCoordinator(opts *CoordinatorOpts) (Coordinator, error) {
	ns := opts.Namespace
	groupName := opts.Hostname
	if ns == "" {
		logger.Warnf("No namespace found, peer discovery disabled")
	} else {
		logger.Infof("Using namespace %s for peer discovery", ns)
		if !strings.Contains(groupName, "-") {
			logger.Warnf("Hostname not in statefuleset format, peer discovery disabled")
		} else {
			rindex := strings.LastIndex(groupName, "-")
			groupName = groupName[:rindex]
			logger.Infof("Using statefuleset %s for peer discovery", groupName)
		}
	}

	return &coordinator{
		groupName:   groupName,
		hostname:    opts.Hostname,
		namespace:   ns,
		opts:        opts,
		kcli:        opts.K8sCli,
		peerInfo:    PeerInfo{Count: 1},
		subscribers: make(map[int]func(PeerInfo)),
	}, nil
}

// peerRemovalDelay is how long a member of the partition may be not ready before it is removed.  An ingestor is not
// ready while its backlog is over its limits, and removing it at once would move its tables to other ingestors and
// back as the backlog drains.  The load balancer still stops sending it new data while it is not ready.
var peerRemovalDelay = 5 * time.Minute

// isMember reports whether p belongs in the partition.  A pod joins once it is ready and stays while it is not ready
// for up to peerRemovalDelay.  A terminating pod leaves at once.
//
// Membership depends only on the pod's status in the API, never on what this coordinator has seen before, so every
// coordinator, including one that just started, puts the same pods in the partition and elects the same leader.
func isMember(p *v1.Pod, now time.Time) bool {
	if p.DeletionTimestamp != nil {
		return false
	}
	if IsPodReady(p) {
		return true
	}
	expires, ok := graceExpiry(p)
	return ok && now.Before(expires)
}

// graceExpiry returns when a pod that went from ready to not ready leaves the partition.  It reports false for a pod
// that has not been ready since its containers started, such as one that is starting or whose container restarted,
// since such a pod has not joined.
//
// The API keeps only the Ready condition's last transition.  A pod starts not ready, before its containers start, so a
// not-ready transition after every container started can only have come from being ready.
func graceExpiry(p *v1.Pod) (time.Time, bool) {
	var notReady time.Time
	for _, cond := range p.Status.Conditions {
		if cond.Type == v1.PodReady && cond.Status != v1.ConditionTrue {
			notReady = cond.LastTransitionTime.Time
		}
	}
	if notReady.IsZero() || len(p.Status.ContainerStatuses) == 0 {
		return time.Time{}, false
	}
	for _, cs := range p.Status.ContainerStatuses {
		if cs.State.Running == nil || !notReady.After(cs.State.Running.StartedAt.Time) {
			return time.Time{}, false
		}
	}
	return notReady.Add(peerRemovalDelay), true
}

func (c *coordinator) OnAdd(obj any, isInitialList bool) {
	if p, ok := obj.(*v1.Pod); !ok || !c.isPeer(p) {
		return
	}

	if err := c.syncPeers(); err != nil {
		logger.Errorf("Failed to reconfigure peers: %s", err)
	}
}

func (c *coordinator) OnUpdate(oldObj, newObj any) {
	if p, ok := newObj.(*v1.Pod); !ok || !c.isPeer(p) {
		return
	}

	if err := c.syncPeers(); err != nil {
		logger.Errorf("Failed to reconfigure peers: %s", err)
	}
}

func (c *coordinator) OnDelete(obj any) {
	if p, ok := obj.(*v1.Pod); !ok || !c.isPeer(p) {
		return
	}

	if err := c.syncPeers(); err != nil {
		logger.Errorf("Failed to reconfigure peers: %s", err)
	}
}

func (c *coordinator) isPeer(p *v1.Pod) bool {
	if p.Namespace != c.namespace {
		return false
	}

	var isPeer bool
	for _, ref := range p.OwnerReferences {
		if ref.Kind == "StatefulSet" && ref.Name == c.groupName {
			isPeer = true
		}
	}

	return isPeer
}

func (c *coordinator) Open(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.done = ctx.Done()

	tweakOptions := informers.WithTweakListOptions(func(lo *metav1.ListOptions) {
		lo.LabelSelector = "app=ingestor"
	})

	factory := informers.NewSharedInformerFactoryWithOptions(c.kcli, time.Minute,
		tweakOptions, informers.WithNamespace(c.namespace))
	podsInformer := factory.Core().V1().Pods().Informer()

	factory.Start(ctx.Done()) // Start processing these informers.
	factory.WaitForCacheSync(ctx.Done())
	c.factory = factory

	pl := factory.Core().V1().Pods().Lister()
	c.pl = pl

	myIP, err := GetOutboundIP()
	if err != nil {
		return fmt.Errorf("failed to determin ip: %w", err)
	}
	if myIP == nil || myIP.To4().String() == "" {
		return fmt.Errorf("failed to determine ip")
	}

	hostName := c.opts.Hostname

	hostEndpoint := fmt.Sprintf("https://%s:9090", myIP.To4().String())
	c.hostEntpoint = hostEndpoint
	c.hostname = hostName

	set := make(map[string]string)
	set[c.hostname] = c.hostEntpoint
	c.peers = set
	c.setPartitioner(set)

	if _, err := podsInformer.AddEventHandler(c); err != nil {
		return err
	}

	if err := c.syncPeers(); err != nil {
		return err
	}

	c.wg.Add(1)
	go c.resyncPeers(ctx)

	return nil
}

func (c *coordinator) IsLeader() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.leader
}

func (c *coordinator) Peers() PeerInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.peerInfo
}

func (c *coordinator) SubscribePeers(fn func(PeerInfo)) func() {
	c.subMu.Lock()
	id := c.nextSubID
	c.nextSubID++
	c.subscribers[id] = fn
	c.subMu.Unlock()

	return func() {
		c.subMu.Lock()
		delete(c.subscribers, id)
		c.subMu.Unlock()
	}
}

// notifyPeers calls subscribers with info.  It must not be called while holding mu.
func (c *coordinator) notifyPeers(info PeerInfo) {
	c.subMu.Lock()
	subs := make([]func(PeerInfo), 0, len(c.subscribers))
	for _, fn := range c.subscribers {
		subs = append(subs, fn)
	}
	c.subMu.Unlock()

	for _, fn := range subs {
		fn(info)
	}
}

// computePeerInfo returns this node's peer count and rank.  This node is always counted since it is running even if
// it is not reported ready yet.  mu must be held.
func (c *coordinator) computePeerInfo() PeerInfo {
	names := make([]string, 0, len(c.peers)+1)
	self := false
	for name := range c.peers {
		names = append(names, name)
		if name == c.hostname {
			self = true
		}
	}
	if !self {
		names = append(names, c.hostname)
	}
	sort.Strings(names)
	return PeerInfo{Count: len(names), Rank: sort.SearchStrings(names, c.hostname)}
}

// Owner returns the node that owns b and its address, or empty strings before the peers are known.
func (c *coordinator) Owner(b []byte) (string, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.part == nil {
		return "", ""
	}
	return c.part.Owner(b)
}

func (c *coordinator) Close() error {
	c.cancel()
	c.mu.Lock()
	c.scheduleExpiry(time.Time{})
	c.mu.Unlock()
	c.wg.Wait()
	c.factory.Shutdown()
	return nil
}

// syncPeers determines the active set of ingestors and reconfigures the partitioner.  Subscribers are notified if the
// peer count or this node's rank changed.
func (c *coordinator) syncPeers() error {
	c.mu.Lock()
	err := c.syncPeersLocked()
	info := c.computePeerInfo()
	changed := info != c.peerInfo
	c.peerInfo = info
	c.mu.Unlock()

	if changed {
		logger.Infof("Ingestor peers changed: count=%d rank=%d", info.Count, info.Rank)
		c.notifyPeers(info)
	}
	return err
}

// syncPeersLocked is like syncPeers.  mu must be held.
func (c *coordinator) syncPeersLocked() error {
	pods, err := c.pl.Pods(c.namespace).List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list pods: %w", err)
	}

	var leastNode string
	now := time.Now()
	set := make(map[string]string, len(c.peers))
	var nextExpiry time.Time
	for _, p := range pods {
		if p.Status.PodIP == "" {
			continue
		}

		if !c.isPeer(p) {
			continue
		}

		if !isMember(p, now) {
			continue
		}
		if expires, ok := graceExpiry(p); ok && !IsPodReady(p) && (nextExpiry.IsZero() || expires.Before(nextExpiry)) {
			nextExpiry = expires
		}

		set[p.Name] = fmt.Sprintf("https://%s:9090", p.Status.PodIP)

		// The leader is chosen from ready pods only, so a leader that stops being ready hands over its functions,
		// summary rules and management jobs at once rather than after the grace period, which exists only to keep
		// tables from moving.  A pod that is not ready sees so in its own status and steps down too.
		if IsPodReady(p) && (p.Name < leastNode || leastNode == "") {
			leastNode = p.Name
		}
	}

	wasLeader := c.leader
	c.leader = c.opts.Hostname == leastNode
	if c.leader != wasLeader {
		logger.Infof("Leader change: WasLeader=%t IsLeader=%t, Hostname=%s, LeastNode=%s", wasLeader, c.leader, c.opts.Hostname, leastNode)
	}

	c.scheduleExpiry(nextExpiry)

	if len(set) == 0 {
		return nil
	}

	if err := c.setPartitioner(set); err != nil {
		return err
	}

	return nil
}

func (c *coordinator) setPartitioner(set map[string]string) error {
	c.peers = make(map[string]string, len(set))
	maps.Copy(c.peers, set)

	part, err := NewPartition(set)
	if err != nil {
		return err
	}
	c.part = part
	return nil
}

// scheduleExpiry re-syncs the peers at expires, when a not-ready member's grace period ends.  A zero expires cancels
// it.  mu must be held.
func (c *coordinator) scheduleExpiry(expires time.Time) {
	if c.expiry != nil {
		c.expiry.Stop()
		c.expiry = nil
	}
	if expires.IsZero() {
		return
	}
	c.expiry = time.AfterFunc(time.Until(expires), func() {
		if c.done != nil {
			select {
			case <-c.done:
				return
			default:
			}
		}
		if err := c.syncPeers(); err != nil {
			logger.Errorf("Failed to reconfigure peers: %s", err)
		}
	})
}

func (c *coordinator) resyncPeers(ctx context.Context) {
	defer c.wg.Done()

	t := time.NewTicker(time.Minute)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.syncPeers(); err != nil {
				logger.Errorf("Failed to reconfigure peers: %s", err)
			}
			if logger.IsDebug() {
				c.mu.RLock()
				for peer, addr := range c.peers {
					logger.Debugf("Peers updated %s addr=%s ready=%v", peer, addr, "true")
				}
				c.mu.RUnlock()
			}
		}
	}
}

// Get preferred outbound ip of this machine
func GetOutboundIP() (net.IP, error) {
	conn, err := net.Dial("udp", "169.254.169.25:80")
	if err != nil {
		if strings.Contains(err.Error(), "network is unreachable") {
			return net.IPv4(127, 0, 0, 1), nil
		}
		return nil, err
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP, nil
}

// IsPodReady returns true if all containers in a pod are in a ready state
func IsPodReady(pod *v1.Pod) bool {
	if !IsInitReady(pod) {
		return false
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == v1.PodReady && cond.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}

// IsInitReady returns true if all init containers are in a ready state
func IsInitReady(pod *v1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == v1.PodInitialized && cond.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}
