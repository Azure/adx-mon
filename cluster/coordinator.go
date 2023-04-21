package cluster

import (
	"context"
	"fmt"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/adx-mon/pkg/service"
	"github.com/Azure/adx-mon/pool"
	"github.com/Azure/adx-mon/prompb"
	"github.com/Azure/adx-mon/promremote"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v12 "k8s.io/client-go/listers/core/v1"
	"log"
	"net"
	"os"
	"sync"
	"time"
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
	opts *CoordinatorOpts

	mu   sync.RWMutex
	part *Partitioner
	pcli *promremote.Client

	factory informers.SharedInformerFactory

	tsw          TimeSeriesWriter
	kcli         kubernetes.Interface
	pl           v12.PodLister
	hostEntpoint string
	hostname     string
	cancel       context.CancelFunc
}

func (c *coordinator) OnAdd(obj interface{}) {
	p := obj.(*v1.Pod)
	if p.Namespace != "prom-adx" {
		return
	}
	if err := c.syncPeers(); err != nil {
		logger.Error("Failed to reconfigure peers: %s", err)
	}
}

func (c *coordinator) OnUpdate(oldObj, newObj interface{}) {
	p := newObj.(*v1.Pod)
	if p.Namespace != "prom-adx" {
		return
	}

	if err := c.syncPeers(); err != nil {
		logger.Error("Failed to reconfigure peers: %s", err)
	}

}

func (c *coordinator) OnDelete(obj interface{}) {
	p := obj.(*v1.Pod)
	if p.Namespace != "prom-adx" {
		return
	}

	if err := c.syncPeers(); err != nil {
		logger.Error("Failed to reconfigure peers: %s", err)
	}
}

type CoordinatorOpts struct {
	WriteTimeSeriesFn TimeSeriesWriter
	K8sCli            *kubernetes.Clientset

	// InsecureSkipVerify controls whether a client verifies the server's certificate chain and host name.
	InsecureSkipVerify bool
}

func NewCoordinator(opts *CoordinatorOpts) (Coordinator, error) {
	pcli, err := promremote.NewClient(15*time.Second, opts.InsecureSkipVerify)
	if err != nil {
		return nil, err
	}

	return &coordinator{

		opts: opts,
		kcli: opts.K8sCli,
		pcli: pcli,
	}, nil
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

	hostName, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("failed to determin hostname")
	}

	hostEndpoint := fmt.Sprintf("https://%s:9090/transfer", myIP.To4().String())
	c.hostEntpoint = hostEndpoint
	c.hostname = hostName

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

	pods, err := c.pl.Pods("prom-adx").List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list pods: %w", err)
	}

	set := make(map[string]string)
	set[c.hostname] = c.hostEntpoint

	for _, p := range pods {
		logger.Debug("%s %s", p.Name, p.Status.Phase)
		if p.Status.PodIP == "" || !IsPodReady(p) {
			continue
		}
		set[p.Name] = fmt.Sprintf("https://%s:9090/transfer", p.Status.PodIP)

		logger.Debug("Peer %s", p.Status.PodIP, p.Name)
	}

	for i, v := range set {
		logger.Debug("sync %d %s", i, v)
	}

	part, err := NewPartition(set)
	if err != nil {
		return err
	}
	c.part = part

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
