package k8s

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Azure/adx-mon/pkg/logger"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/clock"
)

const (
	kubeletDefaultPort         = 10250
	defaultCAPath              = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	kubeletServiceAccountToken = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)

// KubeletInformerOptions describes configuration for the kubelet-backed pod informer.
type KubeletInformerOptions struct {
	NodeName string

	PollInterval        time.Duration
	RequestTimeout      time.Duration
	DialTimeout         time.Duration
	TLSHandshakeTimeout time.Duration
	KubeletHost         string
	KubeletPort         int
	TokenPath           string
	CAPath              string
	ClientFactory       func() (podListClient, error)
	Clock               clock.WithTicker // Optional, defaults to clock.RealClock{}
}

// KubeletPodInformer implements the informer contract used by the collector by polling the kubelet pods endpoint.
type KubeletPodInformer struct {
	pollInterval time.Duration
	newClient    func() (podListClient, error)
	clock        clock.WithTicker

	// Guards against concurrent modifications of handlers and currentPods.
	// Ensures we are only doing one of the following at a time:
	//  - polling the kubelet, updating currentPods, and applying updates to existing handlers.
	//  - adding a handler and sending initial adds with currentPods.
	//  - removing a handler.
	processingMut sync.Mutex
	handlers      map[*kubeletRegistration]struct{}
	currentPods   map[types.UID]*corev1.Pod

	// Guards the running state of the poller
	runningMut sync.Mutex
	runCtx     context.Context
	cancelRun  context.CancelFunc
	client     podListClient

	wg sync.WaitGroup
}

type podListClient interface {
	ListPods(ctx context.Context) ([]corev1.Pod, error)
	Close() error
}

type kubeletRegistration struct {
	informer *KubeletPodInformer
	handler  cache.ResourceEventHandler
	synced   atomic.Bool
}

func (r *kubeletRegistration) HasSynced() bool {
	return r.synced.Load()
}

// NewKubeletPodInformer creates a new informer that sources pod events from the local kubelet.
func NewKubeletPodInformer(opts KubeletInformerOptions) (*KubeletPodInformer, error) {
	if opts.ClientFactory == nil && opts.NodeName == "" && opts.KubeletHost == "" {
		return nil, errors.New("kubelet pod informer: either NodeName, KubeletHost, or ClientFactory must be provided")
	}

	pollInterval := opts.PollInterval
	if pollInterval <= 0 {
		pollInterval = 10 * time.Second
	}

	requestTimeout := opts.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = 9 * time.Second
	}

	dialTimeout := opts.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 5 * time.Second
	}

	handshakeTimeout := opts.TLSHandshakeTimeout
	if handshakeTimeout <= 0 {
		handshakeTimeout = 5 * time.Second
	}

	tokenPath := opts.TokenPath
	if tokenPath == "" {
		tokenPath = kubeletServiceAccountToken
	}

	caPath := opts.CAPath
	if caPath == "" {
		caPath = defaultCAPath
	}

	kubeletHost := opts.KubeletHost
	if kubeletHost == "" {
		kubeletHost = opts.NodeName
	}
	if kubeletHost == "" {
		kubeletHost = "127.0.0.1"
	}

	kubeletPort := opts.KubeletPort
	if kubeletPort == 0 {
		kubeletPort = kubeletDefaultPort
	}

	kubeletEndpoint := fmt.Sprintf("%s:%d", kubeletHost, kubeletPort)

	clk := opts.Clock
	if clk == nil {
		clk = clock.RealClock{}
	}

	informer := &KubeletPodInformer{
		pollInterval: pollInterval,
		clock:        clk,
		handlers:     make(map[*kubeletRegistration]struct{}),
		currentPods:  make(map[types.UID]*corev1.Pod),
	}

	if opts.ClientFactory != nil {
		informer.newClient = opts.ClientFactory
	} else {
		clientOpts := kubeletClientOptions{
			Endpoint:       kubeletEndpoint,
			DialTimeout:    dialTimeout,
			TLSHandshake:   handshakeTimeout,
			RequestTimeout: requestTimeout,
			TokenPath:      tokenPath,
			CAPath:         caPath,
			Clock:          clk,
		}
		informer.newClient = func() (podListClient, error) {
			return newKubeletClient(clientOpts)
		}
	}

	return informer, nil
}

// Open starts the kubelet polling loop. This must be called before adding any handlers.
func (k *KubeletPodInformer) Open(ctx context.Context) error {
	k.runningMut.Lock()
	defer k.runningMut.Unlock()

	if k.client != nil {
		return fmt.Errorf("kubelet pod informer: already started")
	}

	client, err := k.newClient()
	if err != nil {
		return fmt.Errorf("kubelet pod informer: create kubelet client: %w", err)
	}
	k.client = client

	k.runCtx, k.cancelRun = context.WithCancel(ctx)

	initialSync := make(chan struct{})
	k.wg.Add(1)
	go func() {
		defer k.wg.Done()
		k.run(k.runCtx, initialSync)
	}()
	<-initialSync

	return nil
}

// Close stops the kubelet polling loop and cleans up resources.
func (k *KubeletPodInformer) Close() error {
	k.runningMut.Lock()
	defer k.runningMut.Unlock()

	if k.client == nil {
		return nil
	}

	k.cancelRun()
	k.wg.Wait()

	k.client.Close()
	k.client = nil
	k.cancelRun = nil

	return nil
}

// Add registers a handler for pod events. The handler receives add, update, and delete notifications.
// The informer must be started with Open() before calling Add().
func (k *KubeletPodInformer) Add(ctx context.Context, handler cache.ResourceEventHandler) (cache.ResourceEventHandlerRegistration, error) {
	k.runningMut.Lock()
	if k.client == nil {
		k.runningMut.Unlock()
		return nil, fmt.Errorf("kubelet pod informer: not started - call Open() first")
	}
	k.runningMut.Unlock()

	k.processingMut.Lock()

	for _, pod := range k.currentPods {
		handler.OnAdd(pod, true)
	}
	reg := &kubeletRegistration{informer: k, handler: handler}
	k.handlers[reg] = struct{}{}
	reg.synced.Store(true)

	k.processingMut.Unlock()

	return reg, nil
}

// Remove unregisters the handler.
func (k *KubeletPodInformer) Remove(reg cache.ResourceEventHandlerRegistration) error {
	registration, ok := reg.(*kubeletRegistration)
	if !ok || registration.informer != k {
		return fmt.Errorf("kubelet pod informer: registration does not belong to this informer")
	}

	k.processingMut.Lock()
	defer k.processingMut.Unlock()

	if _, exists := k.handlers[registration]; !exists {
		return fmt.Errorf("kubelet pod informer: handler not registered")
	}
	delete(k.handlers, registration)

	return nil
}

func (k *KubeletPodInformer) run(ctx context.Context, initialSync chan struct{}) {
	ticker := k.clock.NewTicker(k.pollInterval)
	defer ticker.Stop()

	if err := k.syncOnce(ctx); err != nil {
		logger.Errorf("kubelet pod informer: initial sync failed: %v", err)
		// Do not block startup on initial sync failure - just log the error and allow next sync attempts to retry.
	}
	close(initialSync)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			if err := k.syncOnce(ctx); err != nil {
				logger.Errorf("kubelet pod informer: sync failed: %v", err)
			}
		}
	}
}

func (k *KubeletPodInformer) syncOnce(ctx context.Context) error {
	pods, err := k.client.ListPods(ctx)
	if err != nil {
		return err
	}

	k.applyPodList(pods)
	return nil
}

func (k *KubeletPodInformer) applyPodList(pods []corev1.Pod) {
	additions := make([]*corev1.Pod, 0)
	updates := make([]podUpdate, 0)
	deletions := make([]*corev1.Pod, 0)

	k.processingMut.Lock()
	currentPods := k.currentPods

	// Track which pods we've seen in the new list
	seen := make(map[types.UID]struct{}, len(pods))

	for i := range pods {
		uid := pods[i].UID
		seen[uid] = struct{}{}

		if existing, ok := currentPods[uid]; ok {
			// Pod exists - check if it changed
			// Fast path: compare resourceVersion instead of expensive deep equality check.
			// ResourceVersion changes whenever the pod is updated on the API server,
			// including container restarts, status changes, etc.
			if existing.ResourceVersion != pods[i].ResourceVersion {
				// Pod was updated - take pointer and update map
				pod := pods[i].DeepCopy()
				currentPods[uid] = pod
				updates = append(updates, podUpdate{old: existing, new: pod})
			}
		} else {
			// New pod
			pod := pods[i].DeepCopy()
			currentPods[uid] = pod
			additions = append(additions, pod)
		}
	}

	// Find deletions - pods in currentPods but not in seen
	for uid, existing := range currentPods {
		if _, ok := seen[uid]; !ok {
			deletions = append(deletions, existing)
			delete(currentPods, uid)
		}
	}

	for reg := range k.handlers {
		handler := reg.handler
		for _, added := range additions {
			handler.OnAdd(added, false)
		}
		for _, update := range updates {
			handler.OnUpdate(update.old, update.new)
		}
		for _, removed := range deletions {
			handler.OnDelete(removed)
		}
	}
	k.processingMut.Unlock()
}

type podUpdate struct {
	old *corev1.Pod
	new *corev1.Pod
}

type kubeletClientOptions struct {
	Endpoint       string
	DialTimeout    time.Duration
	TLSHandshake   time.Duration
	RequestTimeout time.Duration
	TokenPath      string
	CAPath         string
	Clock          clock.WithTicker // Optional, defaults to clock.RealClock{}
}

type kubeletClient struct {
	opts kubeletClientOptions

	client    *http.Client
	transport *http.Transport
	clock     clock.WithTicker

	mu    sync.RWMutex
	token string

	closing   chan struct{}
	refreshWG sync.WaitGroup
}

func newKubeletClient(opts kubeletClientOptions) (*kubeletClient, error) {
	if opts.Endpoint == "" {
		opts.Endpoint = fmt.Sprintf("127.0.0.1:%d", kubeletDefaultPort)
	}
	if opts.DialTimeout <= 0 {
		opts.DialTimeout = 5 * time.Second
	}
	if opts.TLSHandshake <= 0 {
		opts.TLSHandshake = 5 * time.Second
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = 10 * time.Second
	}
	if opts.CAPath == "" {
		opts.CAPath = defaultCAPath
	}

	dialer := &net.Dialer{Timeout: opts.DialTimeout}
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: opts.TLSHandshake,
	}

	if err := appendCACert(transport, opts.CAPath); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	}

	httpClient := &http.Client{
		Timeout:   opts.RequestTimeout,
		Transport: transport,
	}

	clk := opts.Clock
	if clk == nil {
		clk = clock.RealClock{}
	}

	c := &kubeletClient{
		opts:      opts,
		client:    httpClient,
		transport: transport,
		clock:     clk,
		closing:   make(chan struct{}),
	}

	if opts.TokenPath != "" {
		if token, err := c.readToken(); err == nil {
			c.mu.Lock()
			c.token = token
			c.mu.Unlock()
		} else {
			return nil, fmt.Errorf("kubeletClient: read initial token: %w", err)
		}

		c.refreshWG.Add(1)
		go func() {
			defer c.refreshWG.Done()
			c.refreshToken()
		}()
	}

	return c, nil
}

func (c *kubeletClient) ListPods(ctx context.Context) ([]corev1.Pod, error) {
	podsURL := fmt.Sprintf("https://%s/pods", c.opts.Endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, podsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("kubeletClient: create kubelet pods request: %w", err)
	}

	c.mu.RLock()
	token := c.token
	c.mu.RUnlock()
	if token != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kubeletClient: request kubelet pods endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("kubeletClient: pods endpoint returned status %s: %s", resp.Status, string(body))
	}

	var podList corev1.PodList
	decoder := json.NewDecoder(resp.Body)
	if err := decoder.Decode(&podList); err != nil {
		return nil, fmt.Errorf("kubeletClient: decode kubelet pods response: %w", err)
	}

	pods := make([]corev1.Pod, len(podList.Items))
	copy(pods, podList.Items)
	return pods, nil
}

func (c *kubeletClient) Close() error {
	close(c.closing)
	c.refreshWG.Wait()
	if c.client != nil {
		c.client.CloseIdleConnections()
	}
	return nil
}

func (c *kubeletClient) refreshToken() {
	// Refresh each minute. Tokens are valid for a longer period, but k8s does not document a way to detect rotations.
	// stat does not detect changes to the inode during rotation, so just read periodically like many k8s libraries do.
	ticker := c.clock.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-c.closing:
			return
		case <-ticker.C():
			token, err := c.readToken()
			if err != nil {
				logger.Errorf("kubeletClient: failed to refresh token: %v", err)
				continue
			}
			c.mu.Lock()
			c.token = token
			c.mu.Unlock()
		}
	}
}

func (c *kubeletClient) readToken() (string, error) {
	if c.opts.TokenPath == "" {
		return "", nil
	}
	b, err := os.ReadFile(c.opts.TokenPath)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func appendCACert(transport *http.Transport, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	certPool, err := x509.SystemCertPool()
	if err != nil || certPool == nil {
		certPool = x509.NewCertPool()
	}

	if ok := certPool.AppendCertsFromPEM(data); !ok {
		return fmt.Errorf("failed to append kubelet CA cert from %s", path)
	}

	transport.TLSClientConfig = &tls.Config{
		RootCAs: certPool,
	}
	return nil
}
