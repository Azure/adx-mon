package cluster

import (
	"context"
	"fmt"
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

	syncMu      sync.Mutex
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

func (c *coordinator) OnAdd(obj interface{}, isInitialList bool) {
	if p, ok := obj.(*v1.Pod); !ok || !c.isPeer(p) {
		return
	}

	if err := c.syncPeers(); err != nil {
		logger.Errorf("Failed to reconfigure peers: %s", err)
	}
}

func (c *coordinator) OnUpdate(oldObj, newObj interface{}) {
	if p, ok := newObj.(*v1.Pod); !ok || !c.isPeer(p) {
		return
	}

	if err := c.syncPeers(); err != nil {
		logger.Errorf("Failed to reconfigure peers: %s", err)
	}
}

func (c *coordinator) OnDelete(obj interface{}) {
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
		return fmt.Errorf("failed to determine ip: %w", err)
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

func (c *coordinator) Owner(b []byte) (string, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.part.Owner(b)
}

func (c *coordinator) Close() error {
	c.cancel()
	c.wg.Wait()
	c.factory.Shutdown()
	return nil
}

// syncPeers determines the active set of ingestors and reconfigures the partitioner.  Subscribers are notified if the
// peer count or this node's rank changed.
func (c *coordinator) syncPeers() error {
	c.syncMu.Lock()
	defer c.syncMu.Unlock()

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
	set := make(map[string]string, len(c.peers))
	for _, p := range pods {
		if p.Status.PodIP == "" {
			continue
		}

		if !c.isPeer(p) {
			continue
		}

		ready := IsPodReady(p)
		if !ready {
			continue
		}

		set[p.Name] = fmt.Sprintf("https://%s:9090", p.Status.PodIP)

		if p.Name < leastNode || leastNode == "" {
			leastNode = p.Name
		}
	}

	wasLeader := c.leader
	c.leader = c.opts.Hostname == leastNode
	if c.leader != wasLeader {
		logger.Infof("Leader change: WasLeader=%t IsLeader=%t, Hostname=%s, LeastNode=%s", wasLeader, c.leader, c.opts.Hostname, leastNode)
	}

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
	for peer, addr := range set {
		c.peers[peer] = addr
	}

	part, err := NewPartition(set)
	if err != nil {
		return err
	}
	c.part = part
	return nil
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
