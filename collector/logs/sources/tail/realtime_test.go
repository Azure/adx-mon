package tail

import (
	"testing"
	"time"

	"github.com/Azure/adx-mon/collector/logs/types"
	"github.com/Azure/adx-mon/pkg/ingestpolicy"
)

func TestRealtimeBatchWaitFollowsDestinationChanges(t *testing.T) {
	policy, err := ingestpolicy.New([]ingestpolicy.Table{{Database: "Logs", Table: "Realtime"}})
	if err != nil {
		t.Fatal(err)
	}
	waitFor := realtimeBatchWait(policy, 100*time.Millisecond)
	log := types.NewLog()
	log.SetAttributeValue(types.AttributeDatabaseName, "Logs")
	log.SetAttributeValue(types.AttributeTableName, "Queued")
	if got := waitFor(log); got != 0 {
		t.Fatalf("queued table wait = %s, want zero", got)
	}
	log.SetAttributeValue(types.AttributeTableName, "Realtime")
	if got := waitFor(log); got != 100*time.Millisecond {
		t.Fatalf("realtime table wait = %s, want 100ms", got)
	}
	log.SetAttributeValue(types.AttributeDatabaseName, "Other")
	if got := waitFor(log); got != 0 {
		t.Fatalf("other database wait = %s, want zero", got)
	}
}
