package cluster

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	logsv1 "buf.build/gen/go/opentelemetry/opentelemetry/protocolbuffers/go/opentelemetry/proto/logs/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/adx-mon/pkg/pool"
	"github.com/Azure/adx-mon/pkg/prompb"
	"github.com/Azure/adx-mon/pkg/promremote"
	"github.com/Azure/adx-mon/pkg/service"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v12 "k8s.io/client-go/listers/core/v1"
)

var (
	tsPool = pool.NewGeneric(1024, func(sz int) interface{} {
		return &prompb.TimeSeries{}
	})
	bytesPool = pool.NewBytes(1024)
)

type TimeSeriesWriter func(ctx context.Context, database string, ts []prompb.TimeSeries) error

type OTLPLogsWriter func(ctx context.Context, database, table string, logs []*logsv1.LogRecord) error

type Coordinator interface {
	MetricPartitioner
	service.Component

	// Write writes the time series to the correct peer.
	Write(ctx context.Context, database string, wr prompb.WriteRequest) error

	// WriteOTLPLogs writes the logs to the correct peer.
	WriteOTLPLogs(ctx context.Context, database, table string, logs []*logsv1.LogRecord) error
}

// Coordinator manages the cluster state and writes to the correct peer.
type coordinator struct {
	opts      *CoordinatorOpts
	namespace string

	mu    sync.RWMutex
	peers map[string]string
	part  *Partitioner
	pcli  *promremote.Client

	factory informers.SharedInformerFactory

	tsw               TimeSeriesWriter
	lw                OTLPLogsWriter
	kcli              kubernetes.Interface
	pl                v12.PodLister
	discoveryDisabled bool
	hostEntpoint      string
	hostname          string
	groupName         string
	cancel            context.CancelFunc
	wg                sync.WaitGroup
}

type CoordinatorOpts struct {
	WriteTimeSeriesFn TimeSeriesWriter
	WriteOTLPLogsFn   OTLPLogsWriter
	K8sCli            kubernetes.Interface

	// Namespace is the namespace used to discover peers.  If not specified, the coordinator will
	// try to use the namespace of the current pod.
	Namespace string

	// Hostname is the hostname of the current node.  This should be the statefulset hostname format
	// in order to discover peers correctly
	Hostname string

	// InsecureSkipVerify controls whether a client verifies the server's certificate chain and host name.
	InsecureSkipVerify bool

	// DisablePeerDiscovery disables peer discovery and prevents transfers of small segments to an owner.
	// Each instance of ingestor will upload received segments directly.
	DisablePeerDiscovery bool
}

func NewCoordinator(opts *CoordinatorOpts) (Coordinator, error) {
	ns := opts.Namespace
	groupName := opts.Hostname
	discoveryDisabled := opts.DisablePeerDiscovery
	if opts.DisablePeerDiscovery {
		logger.Warnf("Peer discovery disabled")
	} else if ns == "" {
		logger.Warnf("No namespace found, peer discovery disabled")
		discoveryDisabled = true
	} else {
		logger.Infof("Using namespace %s for peer discovery", ns)
		if !strings.Contains(groupName, "-") {
			logger.Warnf("Hostname not in statefuleset format, peer discovery disabled")
			discoveryDisabled = true
		} else {
			rindex := strings.LastIndex(groupName, "-")
			groupName = groupName[:rindex]
			logger.Infof("Using statefuleset %s for peer discovery", groupName)
		}
	}

	return &coordinator{
		discoveryDisabled: discoveryDisabled,
		groupName:         groupName,
		hostname:          opts.Hostname,
		namespace:         ns,
		opts:              opts,
		kcli:              opts.K8sCli,
	}, nil
}

func (c *coordinator) OnAdd(obj interface{}, _ bool) {
	p := obj.(*v1.Pod)
	if !c.isPeer(p) {
		return
	}

	if err := c.syncPeers(); err != nil {
		logger.Errorf("Failed to reconfigure peers: %s", err)
	}
}

func (c *coordinator) OnUpdate(oldObj, newObj interface{}) {
	p := newObj.(*v1.Pod)
	if !c.isPeer(p) {
		return
	}

	if err := c.syncPeers(); err != nil {
		logger.Errorf("Failed to reconfigure peers: %s", err)
	}
}

func (c *coordinator) OnDelete(obj interface{}) {
	p := obj.(*v1.Pod)
	if !c.isPeer(p) {
		return
	}

	if err := c.syncPeers(); err != nil {
		logger.Errorf("Failed to reconfigure peers: %s", err)
	}
}

func (c *coordinator) isPeer(p *v1.Pod) bool {
	// Determine if peer discovery is enabled or not
	if c.discoveryDisabled {
		return false
	}

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

	factory := informers.NewSharedInformerFactory(c.kcli, time.Minute)
	podsInformer := factory.Core().V1().Pods().Informer()

	factory.Start(ctx.Done()) // Start processing these informers.
	factory.WaitForCacheSync(ctx.Done())
	c.factory = factory

	pl := factory.Core().V1().Pods().Lister()
	c.pl = pl

	c.tsw = c.opts.WriteTimeSeriesFn
	c.lw = c.opts.WriteOTLPLogsFn

	myIP, err := GetOutboundIP()
	if err != nil {
		return fmt.Errorf("failed to determin ip: %w", err)
	}
	if myIP == nil || myIP.To4().String() == "" {
		return fmt.Errorf("failed to determine ip")
	}

	hostName := c.opts.Hostname

	hostEndpoint := fmt.Sprintf("https://%s:9090/transfer", myIP.To4().String())
	c.hostEntpoint = hostEndpoint
	c.hostname = hostName

	set := make(map[string]string)
	set[c.hostname] = c.hostEntpoint
	c.peers = set
	c.setPartitioner(set)

	if _, err := podsInformer.AddEventHandler(c); err != nil {
		return err
	}

	if c.isPeerDiscoveryEnabled() {
		if err := c.syncPeers(); err != nil {
			return err
		}

		c.wg.Add(1)
		go c.resyncPeers(ctx)
	}

	return nil
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

func (c *coordinator) Write(ctx context.Context, database string, wr prompb.WriteRequest) error {
	return c.tsw(ctx, database, wr.Timeseries)
}

func (c *coordinator) WriteOTLPLogs(ctx context.Context, database, table string, logs []*logsv1.LogRecord) error {
	return c.lw(ctx, database, table, logs)
}

// syncPeers determines the active set of ingestors and reconfigures the partitioner.
func (c *coordinator) syncPeers() error {
	// Determine if peer discovery is enabled or not
	if !c.isPeerDiscoveryEnabled() {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	pods, err := c.pl.Pods(c.namespace).List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list pods: %w", err)
	}

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

		set[p.Name] = fmt.Sprintf("https://%s:9090/transfer", p.Status.PodIP)
	}

	if err := c.setPartitioner(set); err != nil {
		return err
	}

	return nil
}

func (c *coordinator) isPeerDiscoveryEnabled() bool {
	return !c.discoveryDisabled
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
			c.mu.RLock()
			for peer, addr := range c.peers {
				logger.Infof("Peers updated %s addr=%s ready=%v", peer, addr, "true")
			}
			c.mu.RUnlock()
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

func reverse(b []byte) []byte {
	x := make([]byte, len(b))
	for i := 0; i < len(b); i++ {
		x[i] = b[len(b)-i-1]
	}
	return x
}
