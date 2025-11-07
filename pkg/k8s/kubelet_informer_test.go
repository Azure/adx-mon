package k8s

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	clocktesting "k8s.io/utils/clock/testing"

	"github.com/stretchr/testify/require"
)

func TestKubeletPodInformerEmitsEvents(t *testing.T) {
	fakeClient := newFakePodClient()
	clk := clocktesting.NewFakeClock(time.Now())

	informer, err := NewKubeletPodInformer(KubeletInformerOptions{
		ClientFactory: func() (podListClient, error) { return fakeClient, nil },
		PollInterval:  time.Minute,
		Clock:         clk,
	})
	require.NoError(t, err)

	handler := newFakeHandler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", UID: types.UID("pod-1")}}
	fakeClient.UpsertPod(pod)

	reg, err := informer.Add(ctx, handler)
	require.NoError(t, err)
	require.True(t, reg.HasSynced())

	addEvent := handler.waitAdd(t)
	require.True(t, addEvent.Initial)
	require.Equal(t, "pod-1", addEvent.Pod.Name)
	handler.assertNoMoreEvents(t)

	updated := pod.DeepCopy()
	updated.Labels = map[string]string{"k": "v"}
	fakeClient.UpsertPod(*updated)
	clk.Step(time.Minute)

	updateEvent := handler.waitUpdate(t)
	require.Equal(t, "pod-1", updateEvent.OldPod.Name)
	require.Equal(t, "v", updateEvent.NewPod.Labels["k"])
	handler.assertNoMoreEvents(t)

	fakeClient.RemovePod(pod.UID)
	clk.Step(time.Minute)

	deleteEvent := handler.waitDelete(t)
	require.Equal(t, "pod-1", deleteEvent.Pod.Name)
	handler.assertNoMoreEvents(t)

	// Add a new pod after initial sync
	newPod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-2", UID: types.UID("pod-2")}}
	fakeClient.UpsertPod(newPod)
	clk.Step(time.Minute)

	addEvent2 := handler.waitAdd(t)
	require.False(t, addEvent2.Initial, "new pod should not be marked as initial")
	require.Equal(t, "pod-2", addEvent2.Pod.Name)
	handler.assertNoMoreEvents(t)

	require.NoError(t, informer.Remove(reg))
}

func TestKubeletPodInformerMultipleAddsUpdatesDeletes(t *testing.T) {
	fakeClient := newFakePodClient()
	clk := clocktesting.NewFakeClock(time.Now())

	informer, err := NewKubeletPodInformer(KubeletInformerOptions{
		ClientFactory: func() (podListClient, error) { return fakeClient, nil },
		PollInterval:  time.Minute,
		Clock:         clk,
	})
	require.NoError(t, err)

	handler := newFakeHandler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start with two pods
	pod1 := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", UID: types.UID("pod-1")}}
	pod2 := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-2", UID: types.UID("pod-2")}}
	fakeClient.UpsertPod(pod1)
	fakeClient.UpsertPod(pod2)

	reg, err := informer.Add(ctx, handler)
	require.NoError(t, err)
	require.True(t, reg.HasSynced())

	// Both should be added initially
	add1 := handler.waitAdd(t)
	add2 := handler.waitAdd(t)
	require.True(t, add1.Initial)
	require.True(t, add2.Initial)

	addedNames := []string{add1.Pod.Name, add2.Pod.Name}
	require.ElementsMatch(t, []string{"pod-1", "pod-2"}, addedNames)
	handler.assertNoMoreEvents(t)

	// Add a new pod
	pod3 := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-3", UID: types.UID("pod-3")}}
	fakeClient.UpsertPod(pod3)
	clk.Step(time.Minute)

	add3 := handler.waitAdd(t)
	require.False(t, add3.Initial, "pod-3 should not be initial")
	require.Equal(t, "pod-3", add3.Pod.Name)
	handler.assertNoMoreEvents(t)

	// Poll again with no changes - should not generate any events
	clk.Step(time.Minute)
	time.Sleep(50 * time.Millisecond) // Give poll cycle time to complete
	handler.assertNoMoreEvents(t)

	// Update pod-1
	updated1 := pod1.DeepCopy()
	updated1.Labels = map[string]string{"updated": "true"}
	fakeClient.UpsertPod(*updated1)
	clk.Step(time.Minute)

	update1 := handler.waitUpdate(t)
	require.Equal(t, "pod-1", update1.OldPod.Name)
	require.Equal(t, "pod-1", update1.NewPod.Name)
	require.Equal(t, "true", update1.NewPod.Labels["updated"])
	handler.assertNoMoreEvents(t)

	// Add pod-4 and delete pod-2 in the same poll
	pod4 := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-4", UID: types.UID("pod-4")}}
	fakeClient.UpsertPod(pod4)
	fakeClient.RemovePod(pod2.UID)
	clk.Step(time.Minute)

	// Should see both add and delete
	add4 := handler.waitAdd(t)
	require.Equal(t, "pod-4", add4.Pod.Name)
	delete2 := handler.waitDelete(t)
	require.Equal(t, "pod-2", delete2.Pod.Name)
	handler.assertNoMoreEvents(t)

	// Update pod-4 and delete pod-3 in the same poll
	updated4 := pod4.DeepCopy()
	updated4.Labels = map[string]string{"updated": "true"}
	fakeClient.UpsertPod(*updated4)
	fakeClient.RemovePod(pod3.UID)
	clk.Step(time.Minute)

	// Should see both update and delete
	update4 := handler.waitUpdate(t)
	require.Equal(t, "pod-4", update4.OldPod.Name)
	require.Equal(t, "pod-4", update4.NewPod.Name)
	require.Equal(t, "true", update4.NewPod.Labels["updated"])

	deleteEvent3 := handler.waitDelete(t)
	require.Equal(t, "pod-3", deleteEvent3.Pod.Name)
	handler.assertNoMoreEvents(t)

	// Delete remaining pods
	fakeClient.RemovePod(pod1.UID)
	fakeClient.RemovePod(pod4.UID)
	clk.Step(time.Minute)

	delete1 := handler.waitDelete(t)
	delete4 := handler.waitDelete(t)

	deletedNames := []string{delete1.Pod.Name, delete4.Pod.Name}
	require.ElementsMatch(t, []string{"pod-1", "pod-4"}, deletedNames)
	handler.assertNoMoreEvents(t)

	require.NoError(t, informer.Remove(reg))
}

func TestKubeletPodInformerConcurrentHandlersRestart(t *testing.T) {
	fakeClient := newFakePodClient()
	clk := clocktesting.NewFakeClock(time.Now())

	informer, err := NewKubeletPodInformer(KubeletInformerOptions{
		ClientFactory: func() (podListClient, error) { return fakeClient, nil },
		PollInterval:  time.Minute,
		Clock:         clk,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", UID: types.UID("pod-1")}}
	fakeClient.UpsertPod(pod)

	const handlerCount = 8
	handlers := make([]*fakeHandler, handlerCount)
	regs := make([]cache.ResourceEventHandlerRegistration, handlerCount)

	var addErr error
	var addErrMu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < handlerCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			handler := newFakeHandler()
			reg, err := informer.Add(ctx, handler)
			if err != nil {
				addErrMu.Lock()
				if addErr == nil {
					addErr = err
				}
				addErrMu.Unlock()
				return
			}

			handlers[idx] = handler
			regs[idx] = reg
		}(i)
	}

	wg.Wait()
	require.NoError(t, addErr)

	for _, handler := range handlers {
		addEvent := handler.waitAdd(t)
		require.True(t, addEvent.Initial)
		require.Equal(t, pod.Name, addEvent.Pod.Name)
	}

	var removeErr error
	var removeErrMu sync.Mutex
	wg = sync.WaitGroup{}

	for _, reg := range regs {
		wg.Add(1)
		go func(r cache.ResourceEventHandlerRegistration) {
			defer wg.Done()
			if err := informer.Remove(r); err != nil {
				removeErrMu.Lock()
				if removeErr == nil {
					removeErr = err
				}
				removeErrMu.Unlock()
			}
		}(reg)
	}

	wg.Wait()
	require.NoError(t, removeErr)

	// Ensure informer can start again after full shutdown.
	restartHandler := newFakeHandler()
	restartReg, err := informer.Add(ctx, restartHandler)
	require.NoError(t, err)
	addEvent := restartHandler.waitAdd(t)
	require.True(t, addEvent.Initial)
	require.Equal(t, pod.Name, addEvent.Pod.Name)
	require.NoError(t, informer.Remove(restartReg))
}

type fakePodClient struct {
	mu   sync.RWMutex
	pods map[types.UID]corev1.Pod
}

func newFakePodClient() *fakePodClient {
	return &fakePodClient{pods: make(map[types.UID]corev1.Pod)}
}

func (f *fakePodClient) ListPods(ctx context.Context) ([]corev1.Pod, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	pods := make([]corev1.Pod, 0, len(f.pods))
	for _, pod := range f.pods {
		pods = append(pods, *pod.DeepCopy())
	}
	return pods, nil
}

func (f *fakePodClient) Close() error { return nil }

func (f *fakePodClient) UpsertPod(pod corev1.Pod) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pods[pod.UID] = *pod.DeepCopy()
}

func (f *fakePodClient) RemovePod(uid types.UID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.pods, uid)
}

type fakeHandler struct {
	adds    chan addEvent
	updates chan updateEvent
	deletes chan deleteEvent
}

type addEvent struct {
	Pod     *corev1.Pod
	Initial bool
}

type updateEvent struct {
	OldPod *corev1.Pod
	NewPod *corev1.Pod
}

type deleteEvent struct {
	Pod *corev1.Pod
}

func newFakeHandler() *fakeHandler {
	return &fakeHandler{
		adds:    make(chan addEvent, 16),
		updates: make(chan updateEvent, 16),
		deletes: make(chan deleteEvent, 16),
	}
}

func (f *fakeHandler) OnAdd(obj interface{}, isInitialList bool) {
	if pod, ok := obj.(*corev1.Pod); ok {
		f.adds <- addEvent{Pod: pod.DeepCopy(), Initial: isInitialList}
	}
}

func (f *fakeHandler) OnUpdate(oldObj, newObj interface{}) {
	oldPod, okOld := oldObj.(*corev1.Pod)
	newPod, okNew := newObj.(*corev1.Pod)
	if okOld && okNew {
		f.updates <- updateEvent{OldPod: oldPod.DeepCopy(), NewPod: newPod.DeepCopy()}
	}
}

func (f *fakeHandler) OnDelete(obj interface{}) {
	if pod, ok := obj.(*corev1.Pod); ok {
		f.deletes <- deleteEvent{Pod: pod.DeepCopy()}
	}
}

func (f *fakeHandler) waitAdd(t *testing.T) addEvent {
	t.Helper()
	select {
	case ev := <-f.adds:
		return ev
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for add event")
	}
	return addEvent{}
}

func (f *fakeHandler) waitUpdate(t *testing.T) updateEvent {
	t.Helper()
	select {
	case ev := <-f.updates:
		return ev
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for update event")
	}
	return updateEvent{}
}

func (f *fakeHandler) waitDelete(t *testing.T) deleteEvent {
	t.Helper()
	select {
	case ev := <-f.deletes:
		return ev
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delete event")
	}
	return deleteEvent{}
}

func (f *fakeHandler) assertNoMoreEvents(t *testing.T) {
	t.Helper()
	select {
	case ev := <-f.adds:
		t.Fatalf("unexpected add event for %s", ev.Pod.Name)
	case ev := <-f.updates:
		t.Fatalf("unexpected update event for %s", ev.NewPod.Name)
	case ev := <-f.deletes:
		t.Fatalf("unexpected delete event for %s", ev.Pod.Name)
	default:
		// Expected: no events
	}
}

func TestKubeletClientListPods(t *testing.T) {
	pod1 := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-1",
			Namespace: "default",
			UID:       types.UID("uid-1"),
		},
	}
	pod2 := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-2",
			Namespace: "kube-system",
			UID:       types.UID("uid-2"),
		},
	}

	podList := corev1.PodList{
		Items: []corev1.Pod{pod1, pod2},
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/pods", r.URL.Path)
		require.Equal(t, "GET", r.Method)

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(podList))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	caPath := writeServerCA(t, server, tmpDir)

	client, err := newKubeletClient(kubeletClientOptions{
		Endpoint:       server.Listener.Addr().String(),
		CAPath:         caPath,
		RequestTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	defer client.Close()

	ctx := context.Background()
	pods, err := client.ListPods(ctx)
	require.NoError(t, err)
	require.Len(t, pods, 2)
	require.Equal(t, "pod-1", pods[0].Name)
	require.Equal(t, "pod-2", pods[1].Name)
}

func TestKubeletClientWithToken(t *testing.T) {
	var authHeader atomic.Value

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		podList := corev1.PodList{Items: []corev1.Pod{}}
		require.NoError(t, json.NewEncoder(w).Encode(podList))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	caPath := writeServerCA(t, server, tmpDir)
	tokenPath := filepath.Join(tmpDir, "token")
	require.NoError(t, os.WriteFile(tokenPath, []byte("test-token-123"), 0600))

	client, err := newKubeletClient(kubeletClientOptions{
		Endpoint:       server.Listener.Addr().String(),
		CAPath:         caPath,
		TokenPath:      tokenPath,
		RequestTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	defer client.Close()

	ctx := context.Background()
	_, err = client.ListPods(ctx)
	require.NoError(t, err)

	auth := authHeader.Load().(string)
	require.Equal(t, "Bearer test-token-123", auth)
}

func TestKubeletClientTokenRefresh(t *testing.T) {
	var authHeader atomic.Value

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		podList := corev1.PodList{Items: []corev1.Pod{}}
		require.NoError(t, json.NewEncoder(w).Encode(podList))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	caPath := writeServerCA(t, server, tmpDir)
	tokenPath := filepath.Join(tmpDir, "token")
	require.NoError(t, os.WriteFile(tokenPath, []byte("initial-token"), 0600))

	clk := clocktesting.NewFakeClock(time.Now())

	client, err := newKubeletClient(kubeletClientOptions{
		Endpoint:       server.Listener.Addr().String(),
		CAPath:         caPath,
		TokenPath:      tokenPath,
		RequestTimeout: 5 * time.Second,
		Clock:          clk,
	})
	require.NoError(t, err)
	defer client.Close()

	ctx := context.Background()
	_, err = client.ListPods(ctx)
	require.NoError(t, err)
	require.Equal(t, "Bearer initial-token", authHeader.Load().(string))

	// Update token file and advance clock to trigger refresh
	require.NoError(t, os.WriteFile(tokenPath, []byte("refreshed-token"), 0600))
	clk.Step(time.Minute)

	// Wait for the refresh goroutine to process the tick and update the token
	require.Eventually(t, func() bool {
		_, err = client.ListPods(ctx)
		if err != nil {
			return false
		}
		auth, ok := authHeader.Load().(string)
		return ok && auth == "Bearer refreshed-token"
	}, time.Second, 10*time.Millisecond, "token should be refreshed after clock step")
}

func TestKubeletClientErrorHandling(t *testing.T) {
	t.Run("non-200 response", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("unauthorized"))
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		caPath := writeServerCA(t, server, tmpDir)

		client, err := newKubeletClient(kubeletClientOptions{
			Endpoint:       server.Listener.Addr().String(),
			CAPath:         caPath,
			RequestTimeout: 5 * time.Second,
		})
		require.NoError(t, err)
		defer client.Close()

		ctx := context.Background()
		_, err = client.ListPods(ctx)
		require.Error(t, err)
		require.Contains(t, err.Error(), "401")
		require.Contains(t, err.Error(), "unauthorized")
	})

	t.Run("invalid json", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("not valid json"))
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		caPath := writeServerCA(t, server, tmpDir)

		client, err := newKubeletClient(kubeletClientOptions{
			Endpoint:       server.Listener.Addr().String(),
			CAPath:         caPath,
			RequestTimeout: 5 * time.Second,
		})
		require.NoError(t, err)
		defer client.Close()

		ctx := context.Background()
		_, err = client.ListPods(ctx)
		require.Error(t, err)
		require.Contains(t, err.Error(), "decode")
	})
}

func TestKubeletClientClose(t *testing.T) {
	tmpDir := t.TempDir()
	tokenPath := filepath.Join(tmpDir, "token")
	require.NoError(t, os.WriteFile(tokenPath, []byte("test-token"), 0600))

	client, err := newKubeletClient(kubeletClientOptions{
		Endpoint:       "127.0.0.1:10250",
		TokenPath:      tokenPath,
		RequestTimeout: 5 * time.Second,
	})
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		client.Close()
		close(done)
	}()

	select {
	case <-done:
		// Success - Close returned
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not complete within timeout")
	}
}

// writeServerCA writes the TLS server's CA certificate to a file and returns the path.
func writeServerCA(t *testing.T, server *httptest.Server, dir string) string {
	t.Helper()

	if server.Certificate() == nil {
		t.Fatal("server has no certificate")
	}

	caPath := filepath.Join(dir, "ca.crt")
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: server.Certificate().Raw,
	})

	require.NoError(t, os.WriteFile(caPath, certPEM, 0600))
	return caPath
}
