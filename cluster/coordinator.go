package cluster

import (
	"bytes"
	"context"
	"fmt"
	"github.com/Azure/adx-mon/logger"
	"github.com/Azure/adx-mon/pool"
	"github.com/Azure/adx-mon/prompb"
	"github.com/cespare/xxhash"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	v12 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	tsPool = pool.NewGeneric(1024, func(sz int) interface{} {
		return &prompb.TimeSeries{}
	})
	bytesPool = pool.NewBytes(1024)
)

const MaxPartitions = 256

type TimeSeriesWriter func(ts prompb.TimeSeries) error

type Coordinator struct {
	opts *CoordinatorOpts

	natsOpts *server.Options
	srv      *server.Server
	part     *Partitioner
	conn     *nats.Conn

	factory informers.SharedInformerFactory

	tsw  TimeSeriesWriter
	kcli kubernetes.Interface
	pl   v12.PodLister
	subs []*nats.Subscription
}

func (c *Coordinator) OnAdd(obj interface{}) {
	p := obj.(*v1.Pod)
	if p.Namespace != "prom-adx" {
		return
	}
	if err := c.syncPeers(); err != nil {
		logger.Error("Failed to reconfigure nats peers: %s", err)
	}
}

func (c *Coordinator) OnUpdate(oldObj, newObj interface{}) {
}

func (c *Coordinator) OnDelete(obj interface{}) {
	p := obj.(*v1.Pod)
	if p.Namespace != "prom-adx" {
		return
	}

	if err := c.syncPeers(); err != nil {
		logger.Error("Failed to reconfigure nats peers: %s", err)
	}
}

type CoordinatorOpts struct {
	JoinAddrs         []string
	WriteTimeSeriesFn TimeSeriesWriter
}

func NewCoordinator(opts *CoordinatorOpts) (*Coordinator, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	kcli, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &Coordinator{
		opts: opts,
		kcli: kcli,
	}, nil
}

func (c *Coordinator) Open() error {
	host, err := os.Hostname()
	if err != nil {
		return err
	}

	ctx := context.Background()

	factory := informers.NewSharedInformerFactory(c.kcli, time.Minute)
	podsInformer := factory.Core().V1().Pods().Informer()

	factory.Start(ctx.Done()) // Start processing these informers.
	factory.WaitForCacheSync(ctx.Done())
	c.factory = factory

	pl := factory.Core().V1().Pods().Lister()
	c.pl = pl

	po, err := pl.Pods("prom-adx").List(labels.Everything())
	if err != nil {
		return err
	}

	var seed string
	for _, p := range po {
		if strings.HasSuffix(p.Name, "-0") {
			seed = fmt.Sprintf("nats://%s:4224", p.Status.PodIP)
			println(p.Name, p.Status.PodIP)
			break
		}
	}

	parts := strings.Split(host, "-")
	idx, err := strconv.Atoi(parts[len(parts)-1])
	if err == nil && idx > 0 {
		if seed == "" {
			return fmt.Errorf("failed to find seed node")
		}
		c.opts.JoinAddrs = []string{seed}
	}

	c.natsOpts = &server.Options{
		Debug: true,
		//Trace: true,
		//TraceVerbose: true,

		//Routes: []*url.URL{u},

		Cluster: server.ClusterOpts{
			Name:           "prom-adx",
			Port:           4224,
			ConnectRetries: 10,
		},
	}

	for _, v := range c.opts.JoinAddrs {
		u, err := url.Parse(v)
		if err != nil {
			return err
		}

		c.natsOpts.Routes = append(c.natsOpts.Routes, u)
	}

	// Initialize new server with options
	ns, err := server.NewServer(c.natsOpts)
	if err != nil {
		return err
	}
	ns.ConfigureLogger()

	c.srv = ns

	var nodes []string
	for _, p := range po {
		if p.Status.PodIP == "" {
			continue
		}
		nodes = append(nodes, p.Name)
	}
	part, err := NewPartition(nodes)
	if err != nil {
		return err
	}
	c.part = part

	// Start the server via goroutine
	go ns.Start()

	// Wait for server to be ready for connections
	if !ns.ReadyForConnections(4 * time.Second) {
		return fmt.Errorf("not ready for connections")
	}

	conn, err := nats.Connect("", nats.InProcessServer(ns))
	if err != nil {
		return err
	}
	c.conn = conn
	c.tsw = c.opts.WriteTimeSeriesFn

	if _, err := podsInformer.AddEventHandler(c); err != nil {
		return err
	}

	logger.Info("Listening for peers at: %s", ns.ClusterAddr().String())

	return nil
}

func (c *Coordinator) Close() error {
	c.srv.Shutdown()
	return nil
}

func (c *Coordinator) HandleMsg(msg *nats.Msg) {
	ts := tsPool.Get(0).(*prompb.TimeSeries)
	defer tsPool.Put(ts)
	if _, _, err := ts.Unmarshal(msg.Data, nil, nil); err != nil {
		logger.Error("unmarshal %s", err)
		return
	}

	sort.Slice(ts.Labels, func(i, j int) bool {
		return bytes.Compare(ts.Labels[i].Name, ts.Labels[j].Name) < 0
	})

	if err := c.tsw(*ts); err != nil {
		logger.Error("write timeseries %s", err)
		return
	}
}

func (c *Coordinator) Write(ts prompb.TimeSeries) error {
	n := ts.Labels[0].Value
	hash := xxhash.Sum64(n) % MaxPartitions

	//b := bytesPool.Get(ts.Size())
	//defer bytesPool.Put(b)

	b, err := ts.Marshal()
	if err != nil {
		return err
	}

	part := fmt.Sprintf("partition-%d", hash)
	return c.conn.Publish(part, b)
}

func (c *Coordinator) syncPeers() error {
	pods, err := c.pl.Pods("prom-adx").List(labels.Everything())
	if err != nil {
		return fmt.Errorf("list pods: %w", err)
	}

	var routes []*url.URL
	for _, pod := range pods {
		if pod.Status.PodIP == "" {
			continue
		}
		u, err := url.Parse(fmt.Sprintf("nats://%s:4224", pod.Status.PodIP))
		if err != nil {
			logger.Error("Failed to create nats peer: %s", err)
			continue
		}
		routes = append(routes, u)
	}

	opts := c.natsOpts.Clone()
	opts.Routes = routes
	if err := c.srv.ReloadOptions(opts); err != nil {
		return fmt.Errorf("reconfigure nats: %w", err)
	}
	c.natsOpts = opts

	host, err := os.Hostname()
	if err != nil {
		return err
	}

	for _, sub := range c.subs {
		if err := sub.Unsubscribe(); err != nil {
			logger.Error("Failed to unsubscribe: %s", err)
		}
	}

	var nodes []string
	for _, p := range pods {
		if p.Status.PodIP == "" {
			continue
		}
		nodes = append(nodes, p.Name)
	}

	part, err := NewPartition(nodes)
	if err != nil {
		return err
	}
	c.part = part

	var subs []*nats.Subscription
	for i := 0; i < MaxPartitions; i++ {
		partition := fmt.Sprintf("partition-%d", i)
		owner := c.part.Owner([]byte(partition))
		if owner == host {
			println(partition, owner, host)
			sub, err := c.conn.Subscribe(partition, c.HandleMsg)
			if err != nil {
				return err
			}
			subs = append(subs, sub)
		}
	}
	c.subs = subs

	return nil
}
