package cluster

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakek8s "k8s.io/client-go/kubernetes/fake"
)

// Owner can be called while Open is setting up the peers, as the store's ownership sweep does after a restart.  Run
// with -race to catch the partitioner being set without the lock.
func TestCoordinator_OwnerDuringOpen(t *testing.T) {
	self := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "ingestor-0", Namespace: "adx-mon", Labels: map[string]string{"app": "ingestor"}},
		Status: v1.PodStatus{PodIP: "10.200.0.1", Conditions: []v1.PodCondition{
			{Type: v1.PodInitialized, Status: v1.ConditionTrue},
			{Type: v1.PodReady, Status: v1.ConditionTrue},
		}},
	}
	c, err := NewCoordinator(&CoordinatorOpts{
		K8sCli:    fakek8s.NewSimpleClientset(&v1.PodList{Items: []v1.Pod{*self}}),
		Namespace: "adx-mon",
		Hostname:  "ingestor-0",
	})
	require.NoError(t, err)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				c.Owner([]byte("DB_Table"))
			}
		}
	}()
	require.NoError(t, c.Open(context.Background()))
	close(stop)
	wg.Wait()
	owner, _ := c.Owner([]byte("DB_Table"))
	require.Equal(t, "ingestor-0", owner)
	require.NoError(t, c.Close())
}
