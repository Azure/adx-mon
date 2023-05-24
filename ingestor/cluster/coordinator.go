package cluster

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

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

type TimeSeriesWriter func(ctx context.Context, ts []prompb.TimeSeries) error

type Coordinator interface {
	MetricPartitioner
	service.Component

	// Write writes the time series to the correct peer.
	Write(ctx context.Context, wr prompb.WriteRequest) error
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

	tsw          TimeSeriesWriter
	kcli         kubernetes.Interface
	pl           v12.PodLister
	hostEntpoint string
	hostname     string
	groupName    string
	cancel       context.CancelFunc
}

type CoordinatorOpts struct {
	WriteTimeSeriesFn TimeSeriesWriter
	K8sCli            kubernetes.Interface

	// Namespace is the namespace used to discover peers.  If not specified, the coordinator will
	// try to use the namespace of the current pod.
	Namespace string

	// Hostname is the hostname of the current node.  This should be the statefulset hostname format
	// in order to discover peers correctly
	Hostname string

	// InsecureSkipVerify controls whether a client verifies the server's certificate chain and host name.
	InsecureSkipVerify bool
}

func NewCoordinator(opts *CoordinatorOpts) (Coordinator, error) {
	pcli, err := promremote.NewClient(15*time.Second, opts.InsecureSkipVerify)
	if err != nil {
		return nil, err
	}

	ns := opts.Namespace
	if ns == "" {
		logger.Warn("No namespace found, peer discovery disabled")
	} else {
		logger.Info("Using namespace %s for peer discovery", ns)
	}

	groupName := opts.Hostname
	if groupName == "" {
		logger.Warn("No hostname found, peer discovery disabled")
	} else if !strings.Contains(groupName, "-") {
		logger.Warn("Hostname not in statefuleset format, peer discovery disabled")
	} else {
		rindex := strings.LastIndex(groupName, "-")
		groupName = groupName[:rindex]
		logger.Info("Using statefuleset %s for peer discovery", groupName)
	}

	return &coordinator{
		groupName: groupName,
		hostname:  opts.Hostname,
		namespace: ns,
		opts:      opts,
		kcli:      opts.K8sCli,
		pcli:      pcli,
	}, nil
}

func (c *coordinator) OnAdd(obj interface{}) {
	p := obj.(*v1.Pod)
	if !c.isPeer(p) {
		return
	}

	if err := c.syncPeers(); err != nil {
		logger.Error("Failed to reconfigure peers: %s", err)
	}
}

func (c *coordinator) OnUpdate(oldObj, newObj interface{}) {
	p := newObj.(*v1.Pod)
	if !c.isPeer(p) {
		return
	}

	if err := c.syncPeers(); err != nil {
		logger.Error("Failed to reconfigure peers: %s", err)
	}
}

func (c *coordinator) OnDelete(obj interface{}) {
	p := obj.(*v1.Pod)
	if !c.isPeer(p) {
		return
	}

	if err := c.syncPeers(); err != nil {
		logger.Error("Failed to reconfigure peers: %s", err)
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

	factory := informers.NewSharedInformerFactory(c.kcli, time.Minute)
	podsInformer := factory.Core().V1().Pods().Informer()

	factory.Start(ctx.Done()) // Start processing these informers.
	factory.WaitForCacheSync(ctx.Done())
	c.factory = factory

	pl := factory.Core().V1().Pods().Lister()
	c.pl = pl

	c.tsw = c.opts.WriteTimeSeriesFn

	myIP := GetOutboundIP()
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
	logger.Info("Peer changed %s addr=%s ready=%v", hostName, myIP.To4().String(), "true")

	if _, err := podsInformer.AddEventHandler(c); err != nil {
		return err
	}

	if err := c.syncPeers(); err != nil {
		return err
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
	c.factory.Shutdown()
	return nil
}

func (c *coordinator) Write(ctx context.Context, wr prompb.WriteRequest) error {
	return c.tsw(ctx, wr.Timeseries)
}

// syncPeers determines the active set of ingestors and reconfigures the partitioner.
func (c *coordinator) syncPeers() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	pods, err := c.pl.Pods(c.namespace).List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list pods: %w", err)
	}

	for _, p := range pods {
		if p.Status.PodIP == "" {
			continue
		}

		if !c.isPeer(p) {
			continue
		}

		_, isPeer := c.peers[p.Name]
		ready := IsPodReady(p)

		// if the peer is not ready and already in our peer set, remove it.
		if !ready && isPeer {
			logger.Info("Peer changed %s addr=%s ready=%v", p.Name, p.Status.PodIP, ready)
			delete(c.peers, p.Name)
			continue
		}

		// If the peer is not ready and not already in our peer set, skip it.
		if !ready || isPeer {
			continue
		}

		logger.Info("Peer changed %s addr=%s ready=%v", p.Name, p.Status.PodIP, ready)

		c.peers[p.Name] = fmt.Sprintf("https://%s:9090/transfer", p.Status.PodIP)
	}

	set := make(map[string]string, len(c.peers))
	for peer, addr := range c.peers {
		set[peer] = addr
	}

	part, err := NewPartition(set)
	if err != nil {
		return err
	}
	c.part = part
	c.peers = set

	return nil
}

// Get preferred outbound ip of this machine
func GetOutboundIP() net.IP {
	conn, err := net.Dial("udp", "169.254.169.25:80")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP
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
