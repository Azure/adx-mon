package operator

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/celutil"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-kusto-go/kusto"
	"github.com/Azure/azure-kusto-go/kusto/kql"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/kusto/armkusto"
	armresources "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type AdxReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	// ADXClusterCreatingReason denotes a cluster that is being configured.
	ADXClusterCreatingReason = "Creating"
	// ADXClusterWaitingReason denotes a cluster that is fully configured and is waiting to become available.
	ADXClusterWaitingReason = "Waiting"

	otlpHubSchemaDefinition = "Timestamp:datetime, ObservedTimestamp:datetime, TraceId:string, SpanId:string, SeverityText:string, SeverityNumber:int, Body:dynamic, Resource:dynamic, Attributes:dynamic"
)

// resolvedClusterEndpoint returns the effective endpoint to use for a cluster,
// preferring the reconciled status endpoint and falling back to the spec when
// the status has not been populated yet.
func resolvedClusterEndpoint(cluster *adxmonv1.ADXCluster) string {
	if cluster.Status.Endpoint != "" {
		return cluster.Status.Endpoint
	}
	return cluster.Spec.Endpoint
}

func (r *AdxReconciler) setClusterCondition(ctx context.Context, cluster *adxmonv1.ADXCluster, status metav1.ConditionStatus, reason, message string, mutate func(*adxmonv1.ADXClusterStatus) bool) error {
	logger.Infof("ADXCluster %s: updating status - %s: %s", cluster.Spec.ClusterName, reason, message)
	condition := metav1.Condition{
		Type:               adxmonv1.ADXClusterConditionOwner,
		Status:             status,
		ObservedGeneration: cluster.GetGeneration(),
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
	changed := false
	if mutate != nil {
		if mutate(&cluster.Status) {
			changed = true
		}
	}
	if meta.SetStatusCondition(&cluster.Status.Conditions, condition) {
		changed = true
	}
	if changed {
		if err := r.Status().Update(ctx, cluster); err != nil {
			return fmt.Errorf("failed to update status: %w", err)
		}
	}
	return nil
}

func appliedProvisionStateEqual(a, b *adxmonv1.AppliedProvisionState) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.SkuName != b.SkuName || a.Tier != b.Tier {
		return false
	}
	if len(a.UserAssignedIdentities) != len(b.UserAssignedIdentities) {
		return false
	}
	// Compare identities as sets (order-independent)
	aSet := make(map[string]struct{}, len(a.UserAssignedIdentities))
	for _, id := range a.UserAssignedIdentities {
		aSet[id] = struct{}{}
	}
	for _, id := range b.UserAssignedIdentities {
		if _, found := aSet[id]; !found {
			return false
		}
	}
	return true
}

func copyAppliedProvisionState(src *adxmonv1.AppliedProvisionState) *adxmonv1.AppliedProvisionState {
	if src == nil {
		return nil
	}
	cp := &adxmonv1.AppliedProvisionState{
		SkuName: src.SkuName,
		Tier:    src.Tier,
	}
	if len(src.UserAssignedIdentities) > 0 {
		cp.UserAssignedIdentities = append([]string(nil), src.UserAssignedIdentities...)
	}
	return cp
}

func (r *AdxReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cluster adxmonv1.ADXCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// CriteriaExpression gating: if specified and evaluates to false, skip reconciliation quietly.
	if expr := cluster.Spec.CriteriaExpression; expr != "" {
		labels := getOperatorClusterLabels()
		ok, err := celutil.EvaluateCriteriaExpression(labels, expr)
		if err != nil {
			logger.Errorf("ADXCluster %s/%s criteriaExpression error: %v", req.Namespace, req.Name, err)
			// Surface error via status condition for visibility. No explicit requeue—this is a
			// terminal error until the user edits the CRD (a CR update will trigger reconcile).
			c := metav1.Condition{Type: adxmonv1.ADXClusterConditionOwner, Status: metav1.ConditionFalse, Reason: "CriteriaExpressionError", Message: err.Error(), ObservedGeneration: cluster.GetGeneration(), LastTransitionTime: metav1.Now()}
			if meta.SetStatusCondition(&cluster.Status.Conditions, c) {
				_ = r.Status().Update(ctx, &cluster)
			}
			return ctrl.Result{}, nil
		}
		if !ok { // Expression false, mark condition and skip until spec changes
			c := metav1.Condition{Type: adxmonv1.ADXClusterConditionOwner, Status: metav1.ConditionFalse, Reason: "CriteriaExpressionFalse", Message: "criteriaExpression evaluated to false; skipping reconciliation", ObservedGeneration: cluster.GetGeneration(), LastTransitionTime: metav1.Now()}
			if meta.SetStatusCondition(&cluster.Status.Conditions, c) {
				_ = r.Status().Update(ctx, &cluster)
			}
			return ctrl.Result{}, nil
		}
	}

	logger.Infof("Reconciling ADXCluster %s/%s (generation %d)", req.Namespace, req.Name, cluster.GetGeneration())

	if cluster.DeletionTimestamp != nil {
		// Note, at this time we are not going to delete any Azure resources.
		logger.Infof("Cluster %s is being deleted", cluster.Spec.ClusterName)
		return ctrl.Result{}, nil
	}

	condition := meta.FindStatusCondition(cluster.Status.Conditions, adxmonv1.ADXClusterConditionOwner)
	switch {
	case condition == nil:
		// First time reconciliation
		logger.Infof("ADXCluster %s: first-time reconciliation (no existing condition)", cluster.Spec.ClusterName)
		return r.CreateCluster(ctx, &cluster)

	case condition.ObservedGeneration != cluster.GetGeneration():
		// CRD updated
		logger.Infof("ADXCluster %s: CRD updated (generation %d -> %d)", cluster.Spec.ClusterName, condition.ObservedGeneration, cluster.GetGeneration())
		return r.UpdateCluster(ctx, &cluster)

	case condition.Reason == ADXClusterCreatingReason:
		// Cluster is still being configured.
		logger.Infof("ADXCluster %s: continuing cluster creation process", cluster.Spec.ClusterName)
		return r.CreateCluster(ctx, &cluster)

	case condition.Reason == ADXClusterWaitingReason:
		// Check the status of the cluster
		logger.Infof("ADXCluster %s: checking cluster status", cluster.Spec.ClusterName)
		return r.CheckStatus(ctx, &cluster)

	case condition.Reason == "CriteriaExpressionError",
		condition.Reason == "CriteriaExpressionFalse":
		// CriteriaExpression now evaluates successfully (we passed the check above),
		// recover from previous error state by re-running creation.
		logger.Infof("ADXCluster %s: recovering from criteria expression state", cluster.Spec.ClusterName)
		return r.CreateCluster(ctx, &cluster)
	}

	// Federated cluster support.
	if meta.IsStatusConditionTrue(cluster.Status.Conditions, adxmonv1.ADXClusterConditionOwner) && cluster.Spec.Role != nil {
		switch *cluster.Spec.Role {
		case adxmonv1.ClusterRolePartition:
			logger.Infof("ADXCluster %s: executing partition cluster heartbeat", cluster.Spec.ClusterName)
			return r.HeartbeatFederatedClusters(ctx, &cluster)
		case adxmonv1.ClusterRoleFederated:
			logger.Infof("ADXCluster %s: executing federated cluster reconciliation", cluster.Spec.ClusterName)
			return r.FederateClusters(ctx, &cluster)
		}
	}

	logger.Infof("ADXCluster %s: reconciliation complete (no action needed)", cluster.Spec.ClusterName)

	return ctrl.Result{}, nil
}

func (r *AdxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&adxmonv1.ADXCluster{}).
		Complete(r)
}

func (r *AdxReconciler) CreateCluster(ctx context.Context, cluster *adxmonv1.ADXCluster) (ctrl.Result, error) {
	logger.Infof("ADXCluster %s: entering CreateCluster phase", cluster.Spec.ClusterName)

	if cluster.Spec.Provision == nil {
		if cluster.Spec.Endpoint == "" {
			_ = r.setClusterCondition(ctx, cluster, metav1.ConditionFalse, "ProvisioningDisabled", "No provisioning configuration or endpoint provided", func(status *adxmonv1.ADXClusterStatus) bool {
				changed := false
				if status.Endpoint != "" {
					status.Endpoint = ""
					changed = true
				}
				if status.AppliedProvisionState != nil {
					status.AppliedProvisionState = nil
					changed = true
				}
				return changed
			})
			return ctrl.Result{}, nil
		}

		logger.Infof("ADXCluster %s: provision section not specified; using provided endpoint", cluster.Spec.ClusterName)
		if err := r.setClusterCondition(ctx, cluster, metav1.ConditionTrue, "ProvisioningSkipped", fmt.Sprintf("Using existing endpoint %s", cluster.Spec.Endpoint), func(status *adxmonv1.ADXClusterStatus) bool {
			changed := false
			if status.Endpoint != cluster.Spec.Endpoint {
				status.Endpoint = cluster.Spec.Endpoint
				changed = true
			}
			if status.AppliedProvisionState != nil {
				status.AppliedProvisionState = nil
				changed = true
			}
			return changed
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if err := r.setClusterCondition(ctx, cluster, metav1.ConditionUnknown, ADXClusterCreatingReason, fmt.Sprintf("Creating ADX cluster %s", cluster.Name), nil); err != nil {
		return ctrl.Result{}, err
	}

	prov := cluster.Spec.Provision
	missing := []string{}
	if prov.SubscriptionId == "" {
		missing = append(missing, "subscriptionId")
	}
	if prov.ResourceGroup == "" {
		missing = append(missing, "resourceGroup")
	}
	if prov.Location == "" {
		missing = append(missing, "location")
	}
	if prov.SkuName == "" {
		missing = append(missing, "skuName")
	}
	if prov.Tier == "" {
		missing = append(missing, "tier")
	}
	if len(missing) > 0 {
		errMsg := fmt.Sprintf("missing required provisioning fields: %s", strings.Join(missing, ", "))
		logger.Errorf("ADXCluster %s: %s", cluster.Spec.ClusterName, errMsg)
		_ = r.setClusterCondition(ctx, cluster, metav1.ConditionFalse, "ProvisioningInvalid", errMsg, nil)
		return ctrl.Result{}, nil
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create default credential: %w", err)
	}
	registered, err := ensureAdxProvider(ctx, cred, prov.SubscriptionId)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to ensure Kusto provider is registered: %w", err)
	}
	if !registered {
		logger.Infof("ADXCluster %s: waiting for provider registration, requeuing in 5 minutes", cluster.Spec.ClusterName)
		_ = r.setClusterCondition(ctx, cluster, metav1.ConditionUnknown, ADXClusterCreatingReason, fmt.Sprintf("Registering provider for subscription %s", prov.SubscriptionId), nil)
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	logger.Infof("ADXCluster %s: ensuring resource group", cluster.Spec.ClusterName)
	if err := ensureResourceGroup(ctx, cluster, cred); err != nil {
		return ctrl.Result{}, err
	}

	logger.Infof("ADXCluster %s: creating or updating Kusto cluster", cluster.Spec.ClusterName)
	clusterReady, err := createOrUpdateKustoCluster(ctx, cluster, cred)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !clusterReady {
		logger.Infof("ADXCluster %s: cluster not ready yet, requeuing in 1 minute", cluster.Spec.ClusterName)
		_ = r.setClusterCondition(ctx, cluster, metav1.ConditionUnknown, ADXClusterCreatingReason, fmt.Sprintf("Provisioning ADX cluster %s", cluster.Spec.ClusterName), nil)
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	logger.Infof("ADXCluster %s: ensuring databases", cluster.Spec.ClusterName)
	dbCreated, err := ensureDatabases(ctx, cluster, cred)
	if err != nil {
		return ctrl.Result{}, err
	}
	if dbCreated {
		logger.Infof("ADXCluster %s: databases created, requeuing in 1 minute", cluster.Spec.ClusterName)
		_ = r.setClusterCondition(ctx, cluster, metav1.ConditionUnknown, ADXClusterCreatingReason, fmt.Sprintf("Provisioning ADX cluster %s databases", cluster.Spec.ClusterName), nil)
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	_ = r.setClusterCondition(ctx, cluster, metav1.ConditionUnknown, ADXClusterWaitingReason, "Provisioning ADX clusters", nil)

	logger.Infof("ADXCluster %s: CreateCluster phase complete, requeuing in 1 minute", cluster.Spec.ClusterName)
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// ensureResourceGroup checks if the resource group exists and creates it if needed
func ensureResourceGroup(ctx context.Context, cluster *adxmonv1.ADXCluster, cred azcore.TokenCredential) error {
	rgClient, err := armresources.NewResourceGroupsClient(cluster.Spec.Provision.SubscriptionId, cred, nil)
	if err != nil {
		return fmt.Errorf("failed to create resource groups client: %w", err)
	}
	exists, err := rgClient.CheckExistence(ctx, cluster.Spec.Provision.ResourceGroup, nil)
	if err != nil {
		return fmt.Errorf("failed to check resource group existence: %w", err)
	}
	if !exists.Success {
		logger.Infof("Resource group %s not found, creating...", cluster.Spec.Provision.ResourceGroup)
		_, err = rgClient.CreateOrUpdate(ctx,
			cluster.Spec.Provision.ResourceGroup,
			armresources.ResourceGroup{
				Location: to.Ptr(cluster.Spec.Provision.Location),
			},
			nil)
		if err != nil {
			return fmt.Errorf("failed to create resource group: %w", err)
		}
	}
	return nil
}

// createOrUpdateKustoCluster creates the cluster if it doesn't exist, or waits for it to be ready
func createOrUpdateKustoCluster(ctx context.Context, cluster *adxmonv1.ADXCluster, cred azcore.TokenCredential) (bool, error) {
	clustersClient, err := armkusto.NewClustersClient(cluster.Spec.Provision.SubscriptionId, cred, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create clusters client: %w", err)
	}
	available, err := clustersClient.CheckNameAvailability(
		ctx,
		cluster.Spec.Provision.Location,
		armkusto.ClusterCheckNameRequest{
			Name: to.Ptr(cluster.Spec.ClusterName),
		},
		nil,
	)
	if err != nil {
		return false, fmt.Errorf("failed to check cluster name availability: %w", err)
	}
	if available.NameAvailable != nil && *available.NameAvailable {
		logger.Infof("Creating Kusto cluster %s...", cluster.Spec.ClusterName)

		var identity *armkusto.Identity
		if cluster.Spec.Provision != nil && len(cluster.Spec.Provision.UserAssignedIdentities) != 0 {
			userAssignedIdentities := make(map[string]*armkusto.ComponentsSgqdofSchemasIdentityPropertiesUserassignedidentitiesAdditionalproperties)
			for _, u := range cluster.Spec.Provision.UserAssignedIdentities {
				userAssignedIdentities[u] = &armkusto.ComponentsSgqdofSchemasIdentityPropertiesUserassignedidentitiesAdditionalproperties{}
			}
			identity = &armkusto.Identity{
				Type:                   to.Ptr(armkusto.IdentityTypeUserAssigned),
				UserAssignedIdentities: userAssignedIdentities,
			}
		} else {
			identity = &armkusto.Identity{
				Type: to.Ptr(armkusto.IdentityTypeSystemAssigned),
			}
		}

		var autoScale *armkusto.OptimizedAutoscale
		if cluster.Spec.Provision.AutoScale {
			autoScale = &armkusto.OptimizedAutoscale{
				IsEnabled: to.Ptr(cluster.Spec.Provision.AutoScale),
				Maximum:   to.Ptr(int32(cluster.Spec.Provision.AutoScaleMax)),
				Minimum:   to.Ptr(int32(cluster.Spec.Provision.AutoScaleMin)),
				Version:   to.Ptr(int32(1)),
			}
		}
		_, err := clustersClient.BeginCreateOrUpdate(
			ctx,
			cluster.Spec.Provision.ResourceGroup,
			cluster.Spec.ClusterName,
			armkusto.Cluster{
				Location: to.Ptr(cluster.Spec.Provision.Location),
				SKU: &armkusto.AzureSKU{
					Name: toSku(cluster.Spec.Provision.SkuName),
					Tier: toTier(cluster.Spec.Provision.Tier),
				},
				Identity: identity,
				Properties: &armkusto.ClusterProperties{
					EnableAutoStop:     to.Ptr(false),
					EngineType:         to.Ptr(armkusto.EngineTypeV3),
					OptimizedAutoscale: autoScale,
				},
			},
			nil,
		)
		if err != nil {
			return false, fmt.Errorf("failed to create Kusto cluster: %w", err)
		}
		return false, nil // Cluster creation started, not ready yet
	} else {
		resp, err := clustersClient.Get(ctx, cluster.Spec.Provision.ResourceGroup, cluster.Spec.ClusterName, nil)
		if err != nil {
			// Check if this is a 403 Forbidden error for an existing cluster
			var respErr *azcore.ResponseError
			if errors.As(err, &respErr) && respErr.StatusCode == http.StatusForbidden {
				logger.Warnf("ADXCluster %s: insufficient permissions to check existing cluster status (403 Forbidden), assuming cluster exists and is ready", cluster.Spec.ClusterName)
				return true, nil // Assume the existing cluster is ready if we can't check it
			}
			return false, fmt.Errorf("failed to get cluster status: %w", err)
		}
		if resp.Properties == nil || resp.Properties.State == nil || *resp.Properties.State != armkusto.StateRunning {
			return false, nil // Not ready yet
		}
	}
	return true, nil // Cluster is ready
}

// ensureDatabases creates databases if they do not exist, returns true if any were created
func ensureDatabases(ctx context.Context, cluster *adxmonv1.ADXCluster, cred azcore.TokenCredential) (bool, error) {
	if cluster.Spec.Provision == nil {
		logger.Warnf("Skipping database ensure for cluster %s: provision configuration is empty", cluster.Spec.ClusterName)
		return false, nil
	}
	if cluster.Spec.Provision.SubscriptionId == "" || cluster.Spec.Provision.ResourceGroup == "" {
		return false, fmt.Errorf("cluster %s is missing subscriptionId or resourceGroup in provision spec", cluster.Spec.ClusterName)
	}

	databasesClient, err := armkusto.NewDatabasesClient(cluster.Spec.Provision.SubscriptionId, cred, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create databases client: %w", err)
	}
	// Copy the database slice before appending so we don't mutate the
	// CR spec's backing array (Reconcile must treat spec as immutable).
	databases := append([]adxmonv1.ADXClusterDatabaseSpec(nil), cluster.Spec.Databases...)
	if cluster.Spec.Federation != nil && cluster.Spec.Federation.HeartbeatDatabase != nil {
		databases = append(databases, adxmonv1.ADXClusterDatabaseSpec{
			DatabaseName:  *cluster.Spec.Federation.HeartbeatDatabase,
			TelemetryType: adxmonv1.DatabaseTelemetryLogs,
		})
	}
	var dbCreated bool
	for _, db := range databases {
		available, err := databasesClient.CheckNameAvailability(
			ctx,
			cluster.Spec.Provision.ResourceGroup,
			cluster.Spec.ClusterName,
			armkusto.CheckNameRequest{
				Name: to.Ptr(db.DatabaseName),
				Type: to.Ptr(armkusto.TypeMicrosoftKustoClustersDatabases),
			},
			nil,
		)
		if err != nil {
			return false, fmt.Errorf("failed to check database name availability: %w", err)
		}
		if available.NameAvailable != nil && *available.NameAvailable {
			logger.Infof("Creating database %s in cluster %s...", db.DatabaseName, cluster.Spec.ClusterName)
			_, err = databasesClient.BeginCreateOrUpdate(
				ctx,
				cluster.Spec.Provision.ResourceGroup,
				cluster.Spec.ClusterName,
				db.DatabaseName,
				toDatabase(
					cluster.Spec.Provision.SubscriptionId,
					cluster.Spec.ClusterName,
					cluster.Spec.Provision.ResourceGroup,
					cluster.Spec.Provision.Location,
					db.DatabaseName,
				),
				nil,
			)
			if err != nil {
				return false, fmt.Errorf("failed to create database: %w", err)
			}
			dbCreated = true
		} else {
			logger.Infof("Database %s already exists in cluster %s", db.DatabaseName, cluster.Spec.ClusterName)
		}
	}
	return dbCreated, nil
}

func ensureHeartbeatTable(ctx context.Context, cluster *adxmonv1.ADXCluster) (bool, error) {
	if cluster.Spec.Role == nil ||
		*cluster.Spec.Role != adxmonv1.ClusterRoleFederated ||
		cluster.Spec.Federation == nil ||
		cluster.Spec.Federation.HeartbeatDatabase == nil ||
		cluster.Spec.Federation.HeartbeatTable == nil {
		return false, nil
	}
	endpoint := resolvedClusterEndpoint(cluster)
	if strings.TrimSpace(endpoint) == "" {
		return false, fmt.Errorf("heartbeat table cannot be ensured without an endpoint")
	}
	ep := kusto.NewConnectionStringBuilder(endpoint)
	if strings.HasPrefix(endpoint, "https://") {
		// Enables kustainer integration testing
		ep.WithDefaultAzureCredential()
	}
	client, err := kusto.New(ep)
	if err != nil {
		return false, fmt.Errorf("failed to create Kusto client: %w", err)
	}
	defer client.Close()

	q := kql.New(".show tables | where TableName == ").AddString(*cluster.Spec.Federation.HeartbeatTable).AddLiteral(" | count")
	result, err := client.Mgmt(ctx, *cluster.Spec.Federation.HeartbeatDatabase, q)
	if err != nil {
		return false, fmt.Errorf("failed to query Kusto tables: %w", err)
	}
	defer result.Stop()

	for {
		row, errInline, errFinal := result.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			continue
		}
		if errFinal != nil {
			return false, fmt.Errorf("failed to retrieve tables: %w", errFinal)
		}

		var t TableExists
		if err := row.ToStruct(&t); err != nil {
			return false, fmt.Errorf("failed to parse table count: %w", err)
		}
		if t.Count > 0 {
			// Table exists, nothing to do
			return false, nil
		}
	}

	stmt := kql.New(".create table ").AddTable(*cluster.Spec.Federation.HeartbeatTable).AddLiteral("(Timestamp: datetime, ClusterEndpoint: string, Schema: dynamic, PartitionMetadata: dynamic)")
	_, err = client.Mgmt(ctx, *cluster.Spec.Federation.HeartbeatDatabase, stmt)
	return true, err
}

type TableExists struct {
	Count int64 `kusto:"Count"`
}

func (r *AdxReconciler) UpdateCluster(ctx context.Context, cluster *adxmonv1.ADXCluster) (ctrl.Result, error) {
	logger.Infof("ADXCluster %s: entering UpdateCluster phase", cluster.Spec.ClusterName)

	// Note: This method handles existing Kusto clusters where the operator may not have full
	// management permissions. When encountering 403 Forbidden errors from Azure APIs, the operator
	// gracefully degrades to allow federation features to continue working while skipping
	// cluster management operations that require elevated permissions.

	// To accurately detect and reconcile user-driven changes to the cluster configuration (such as Sku, Tier, or UserAssignedIdentities),
	// the operator stores a snapshot of the last-applied configuration. This allows the operator to distinguish between changes made
	// via the CRD (which should be reconciled) and any modifications made directly in Azure (which are intentionally ignored).
	// By comparing the current CRD spec to the stored applied state, we can determine exactly which fields the user has updated
	// and ensure only those changes are propagated to the managed cluster.
	if cluster.Spec.Provision == nil {
		logger.Infof("ADXCluster %s: no provision spec, marking as bring-your-own", cluster.Spec.ClusterName)
		endpoint := strings.TrimSpace(cluster.Spec.Endpoint)
		statusType := metav1.ConditionTrue
		reason := "ProvisioningSkipped"
		message := "Cluster is already reconciled"
		if endpoint == "" {
			statusType = metav1.ConditionFalse
			reason = "ProvisioningDisabled"
			message = "No provisioning configuration or endpoint provided"
		} else {
			message = fmt.Sprintf("Using existing endpoint %s", endpoint)
		}
		if err := r.setClusterCondition(ctx, cluster, statusType, reason, message, func(status *adxmonv1.ADXClusterStatus) bool {
			changed := false
			if status.Endpoint != endpoint {
				status.Endpoint = endpoint
				changed = true
			}
			if status.AppliedProvisionState != nil {
				status.AppliedProvisionState = nil
				changed = true
			}
			return changed
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil // Since we don't have a previous state, we can't update
	}
	appliedProvisionState := cluster.Status.AppliedProvisionState
	if appliedProvisionState == nil {
		appliedProvisionState = &adxmonv1.AppliedProvisionState{}
	}

	// Now get the current state of the cluster
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create default credential: %w", err)
	}

	clustersClient, err := armkusto.NewClustersClient(cluster.Spec.Provision.SubscriptionId, cred, nil)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create clusters client: %w", err)
	}
	resp, err := clustersClient.Get(ctx, cluster.Spec.Provision.ResourceGroup, cluster.Spec.ClusterName, nil)
	if err != nil {
		// Check if this is a 403 Forbidden error, which is expected when the operator
		// is managing existing clusters without sufficient permissions to read cluster details
		var respErr *azcore.ResponseError
		if errors.As(err, &respErr) && respErr.StatusCode == http.StatusForbidden {
			logger.Warnf("ADXCluster %s: insufficient permissions to read cluster details (403 Forbidden), skipping cluster updates - operator can still perform federation tasks", cluster.Spec.ClusterName)
			c := metav1.Condition{
				Type:               adxmonv1.ADXClusterConditionOwner,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: cluster.GetGeneration(),
				LastTransitionTime: metav1.Now(),
				Reason:             "PermissionRestricted",
				Message:            "Cluster management restricted due to insufficient permissions, federation features remain available",
			}
			if meta.SetStatusCondition(&cluster.Status.Conditions, c) {
				if err := r.Status().Update(ctx, cluster); err != nil {
					return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
				}
			}
			return ctrl.Result{}, nil // Non-terminal state, federation can still work
		}
		return ctrl.Result{}, fmt.Errorf("failed to get cluster status: %w", err)
	}

	logger.Infof("ADXCluster %s: checking for cluster configuration changes", cluster.Spec.ClusterName)
	clusterUpdate, updated := diffSkus(resp, appliedProvisionState, cluster)
	var identitiesUpdated bool
	clusterUpdate, identitiesUpdated = diffIdentities(resp, appliedProvisionState, cluster, clusterUpdate)
	if identitiesUpdated {
		updated = true
	}

	if !updated {
		logger.Infof("ADXCluster %s: no updates needed, marking as complete", cluster.Spec.ClusterName)

		var desiredApplied *adxmonv1.AppliedProvisionState
		if cluster.Spec.Provision != nil {
			desiredApplied = &adxmonv1.AppliedProvisionState{
				SkuName: cluster.Spec.Provision.SkuName,
				Tier:    cluster.Spec.Provision.Tier,
			}
			if len(cluster.Spec.Provision.UserAssignedIdentities) > 0 {
				desiredApplied.UserAssignedIdentities = append([]string(nil), cluster.Spec.Provision.UserAssignedIdentities...)
			}
		}

		if err := r.setClusterCondition(ctx, cluster, metav1.ConditionTrue, "Complete", "Cluster is already reconciled", func(status *adxmonv1.ADXClusterStatus) bool {
			changed := false
			if !appliedProvisionStateEqual(status.AppliedProvisionState, desiredApplied) {
				status.AppliedProvisionState = copyAppliedProvisionState(desiredApplied)
				changed = true
			}
			return changed
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil // Terminal state, cluster is up-to-date
	}

	logger.Infof("ADXCluster %s: applying cluster updates", cluster.Spec.ClusterName)
	_, err = clustersClient.BeginCreateOrUpdate(
		ctx,
		cluster.Spec.Provision.ResourceGroup,
		cluster.Spec.ClusterName,
		clusterUpdate,
		nil,
	)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update Kusto cluster: %w", err)
	}

	logger.Infof("ADXCluster %s: cluster update initiated, requeuing in 1 minute", cluster.Spec.ClusterName)
	c := metav1.Condition{
		Type:               adxmonv1.ADXClusterConditionOwner,
		Status:             metav1.ConditionUnknown,
		ObservedGeneration: cluster.GetGeneration(),
		LastTransitionTime: metav1.Now(),
		Reason:             ADXClusterWaitingReason,
		Message:            "Cluster is updating",
	}
	if meta.SetStatusCondition(&cluster.Status.Conditions, c) {
		if err := r.Status().Update(ctx, cluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
		}
	}
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func diffSkus(resp armkusto.ClustersClientGetResponse, applied *adxmonv1.AppliedProvisionState, cluster *adxmonv1.ADXCluster) (armkusto.Cluster, bool) {
	clusterUpdate := armkusto.Cluster{
		Location: resp.Location,
		Identity: resp.Identity,
		Tags:     resp.Tags,
	}

	if cluster.Spec.Provision == nil {
		return clusterUpdate, false
	}

	desiredSku := strings.TrimSpace(cluster.Spec.Provision.SkuName)
	desiredTier := strings.TrimSpace(cluster.Spec.Provision.Tier)

	var currentSku, currentTier string
	if resp.SKU != nil {
		if resp.SKU.Name != nil {
			currentSku = string(*resp.SKU.Name)
		}
		if resp.SKU.Tier != nil {
			currentTier = string(*resp.SKU.Tier)
		}
	}

	var appliedSku, appliedTier string
	if applied != nil {
		appliedSku = strings.TrimSpace(applied.SkuName)
		appliedTier = strings.TrimSpace(applied.Tier)
	}

	// Early return if both desiredSku and desiredTier are empty.
	// This is safe because the CRD contract requires these fields when spec.provision exists.
	// If the contract changes, revisit this logic to ensure state changes (removal/reset) are detected.
	if desiredSku == "" && desiredTier == "" {
		return clusterUpdate, false
	}

	// Nothing to do if the cluster already matches the desired state.
	if desiredSku == currentSku && desiredTier == currentTier {
		return clusterUpdate, false
	}

	// Only attempt an update when the actual state still matches what we previously applied.
	if applied != nil && (appliedSku != currentSku || appliedTier != currentTier) {
		return clusterUpdate, false
	}

	clusterUpdate.SKU = &armkusto.AzureSKU{
		Name: toSku(desiredSku),
		Tier: toTier(desiredTier),
	}
	return clusterUpdate, true
}

func diffIdentities(resp armkusto.ClustersClientGetResponse, applied *adxmonv1.AppliedProvisionState, cluster *adxmonv1.ADXCluster, clusterUpdate armkusto.Cluster) (armkusto.Cluster, bool) {
	if cluster.Spec.Provision == nil {
		return clusterUpdate, false
	}

	desiredSet := make(map[string]struct{})
	for _, id := range cluster.Spec.Provision.UserAssignedIdentities {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		desiredSet[id] = struct{}{}
	}

	appliedSet := make(map[string]struct{})
	if applied != nil {
		for _, id := range applied.UserAssignedIdentities {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			appliedSet[id] = struct{}{}
		}
	}

	actualSet := make(map[string]struct{})
	if resp.Identity != nil && resp.Identity.UserAssignedIdentities != nil {
		for id := range resp.Identity.UserAssignedIdentities {
			actualSet[id] = struct{}{}
		}
	}

	finalSet := make(map[string]*armkusto.ComponentsSgqdofSchemasIdentityPropertiesUserassignedidentitiesAdditionalproperties)
	for id := range actualSet {
		if _, managed := appliedSet[id]; !managed {
			finalSet[id] = &armkusto.ComponentsSgqdofSchemasIdentityPropertiesUserassignedidentitiesAdditionalproperties{}
		}
	}
	for id := range desiredSet {
		finalSet[id] = &armkusto.ComponentsSgqdofSchemasIdentityPropertiesUserassignedidentitiesAdditionalproperties{}
	}

	if len(finalSet) == len(actualSet) {
		same := true
		for id := range actualSet {
			if _, ok := finalSet[id]; !ok {
				same = false
				break
			}
		}
		if same {
			return clusterUpdate, false
		}
	}

	if clusterUpdate.Identity == nil {
		clusterUpdate.Identity = &armkusto.Identity{}
	}
	clusterUpdate.Identity.Type = identityTypeForUpdate(resp.Identity, len(finalSet) > 0)
	if len(finalSet) == 0 {
		clusterUpdate.Identity.UserAssignedIdentities = nil
	} else {
		clusterUpdate.Identity.UserAssignedIdentities = finalSet
	}
	return clusterUpdate, true
}

func identityTypeForUpdate(existing *armkusto.Identity, includeUserAssigned bool) *armkusto.IdentityType {
	hasSystemAssigned := identityHasSystem(existing)
	switch {
	case hasSystemAssigned && includeUserAssigned:
		t := armkusto.IdentityTypeSystemAssignedUserAssigned
		return &t
	case hasSystemAssigned:
		t := armkusto.IdentityTypeSystemAssigned
		return &t
	case includeUserAssigned:
		t := armkusto.IdentityTypeUserAssigned
		return &t
	default:
		t := armkusto.IdentityTypeNone
		return &t
	}
}

func identityHasSystem(identity *armkusto.Identity) bool {
	if identity == nil || identity.Type == nil {
		return false
	}
	switch *identity.Type {
	case armkusto.IdentityTypeSystemAssigned, armkusto.IdentityTypeSystemAssignedUserAssigned:
		return true
	default:
		return strings.Contains(string(*identity.Type), "SystemAssigned")
	}
}

func (r *AdxReconciler) CheckStatus(ctx context.Context, cluster *adxmonv1.ADXCluster) (ctrl.Result, error) {
	logger.Infof("ADXCluster %s: checking cluster status", cluster.Spec.ClusterName)

	if cluster.Spec.Provision == nil {
		endpoint := strings.TrimSpace(resolvedClusterEndpoint(cluster))
		if endpoint == "" {
			_ = r.setClusterCondition(ctx, cluster, metav1.ConditionFalse, "ProvisioningDisabled", "No provisioning configuration or endpoint provided", func(status *adxmonv1.ADXClusterStatus) bool {
				changed := false
				if status.Endpoint != "" {
					status.Endpoint = ""
					changed = true
				}
				if status.AppliedProvisionState != nil {
					status.AppliedProvisionState = nil
					changed = true
				}
				return changed
			})
			return ctrl.Result{}, nil
		}
		if err := r.setClusterCondition(ctx, cluster, metav1.ConditionTrue, "ProvisioningSkipped", fmt.Sprintf("Using existing endpoint %s", endpoint), func(status *adxmonv1.ADXClusterStatus) bool {
			changed := false
			if status.Endpoint != endpoint {
				status.Endpoint = endpoint
				changed = true
			}
			if status.AppliedProvisionState != nil {
				status.AppliedProvisionState = nil
				changed = true
			}
			return changed
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create default credential: %w", err)
	}

	clustersClient, err := armkusto.NewClustersClient(cluster.Spec.Provision.SubscriptionId, cred, nil)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create clusters client: %w", err)
	}
	resp, err := clustersClient.Get(ctx, cluster.Spec.Provision.ResourceGroup, cluster.Spec.ClusterName, nil)
	if err != nil {
		var respErr *azcore.ResponseError
		if errors.As(err, &respErr) && respErr.StatusCode == http.StatusForbidden {
			logger.Warnf("ADXCluster %s: insufficient permissions to check cluster status (403 Forbidden), assuming cluster is ready for federation tasks", cluster.Spec.ClusterName)
			if err := r.setClusterCondition(ctx, cluster, metav1.ConditionTrue, "PermissionRestricted", "Cluster status check restricted due to insufficient permissions, federation features remain available", nil); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get cluster status: %w", err)
	}
	if resp.Properties == nil || resp.Properties.State == nil {
		logger.Infof("ADXCluster %s: cluster properties not available yet, requeuing in 1 minute", cluster.Spec.ClusterName)
		_ = r.setClusterCondition(ctx, cluster, metav1.ConditionUnknown, ADXClusterWaitingReason, "Cluster properties unavailable", nil)
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	logger.Infof("ADXCluster %s: cluster state is %s", cluster.Spec.ClusterName, string(*resp.Properties.State))

	if *resp.Properties.State == armkusto.StateRunning {
		endpoint := ""
		if resp.Properties.URI != nil {
			endpoint = *resp.Properties.URI
		}
		if endpoint == "" {
			endpoint = resolvedClusterEndpoint(cluster)
		}

		var desiredApplied *adxmonv1.AppliedProvisionState
		if cluster.Spec.Provision != nil {
			desiredApplied = &adxmonv1.AppliedProvisionState{
				SkuName: cluster.Spec.Provision.SkuName,
				Tier:    cluster.Spec.Provision.Tier,
			}
			if len(cluster.Spec.Provision.UserAssignedIdentities) > 0 {
				desiredApplied.UserAssignedIdentities = append([]string(nil), cluster.Spec.Provision.UserAssignedIdentities...)
			}
		}

		if err := r.setClusterCondition(ctx, cluster, metav1.ConditionTrue, "ClusterReady", fmt.Sprintf("Cluster %s is ready", cluster.Spec.ClusterName), func(status *adxmonv1.ADXClusterStatus) bool {
			changed := false
			if status.Endpoint != endpoint {
				status.Endpoint = endpoint
				changed = true
			}
			if !appliedProvisionStateEqual(status.AppliedProvisionState, desiredApplied) {
				status.AppliedProvisionState = copyAppliedProvisionState(desiredApplied)
				changed = true
			}
			return changed
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if resp.Properties.ProvisioningState != nil && *resp.Properties.ProvisioningState == armkusto.ProvisioningStateFailed {
		logger.Errorf("ADXCluster %s: cluster provisioning failed", cluster.Spec.ClusterName)
		if err := r.setClusterCondition(ctx, cluster, metav1.ConditionFalse, string(armkusto.ProvisioningStateFailed), "Cluster creation failed", nil); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	logger.Infof("ADXCluster %s: cluster not ready yet (state: %s), requeuing in 1 minute", cluster.Spec.ClusterName, string(*resp.Properties.State))
	_ = r.setClusterCondition(ctx, cluster, metav1.ConditionUnknown, ADXClusterWaitingReason, fmt.Sprintf("Cluster state: %s", string(*resp.Properties.State)), nil)
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *AdxReconciler) HeartbeatFederatedClusters(ctx context.Context, cluster *adxmonv1.ADXCluster) (ctrl.Result, error) {
	if cluster.Spec.Role == nil ||
		*cluster.Spec.Role != adxmonv1.ClusterRolePartition ||
		cluster.Spec.Federation == nil ||
		cluster.Spec.Federation.FederationTargets == nil ||
		cluster.Spec.Federation.Partitioning == nil {
		return ctrl.Result{}, nil
	}

	logger.Infof("ADXCluster %s: sending heartbeats to %d federated clusters", cluster.Spec.ClusterName, len(cluster.Spec.Federation.FederationTargets))
	for _, target := range cluster.Spec.Federation.FederationTargets {
		if err := heartbeatFederatedCluster(ctx, cluster, target); err != nil {
			logger.Errorf("Failed to heartbeat federated cluster %s: %v", target.Endpoint, err)
			continue
		}
		logger.Infof("Heartbeat sent to federated cluster %s", target.Endpoint)
	}
	logger.Infof("ADXCluster %s: heartbeat cycle complete, requeuing in 10 minutes", cluster.Spec.ClusterName)
	return ctrl.Result{RequeueAfter: 10 * time.Minute}, nil
}

func heartbeatFederatedCluster(ctx context.Context, cluster *adxmonv1.ADXCluster, target adxmonv1.ADXClusterFederatedClusterSpec) error {
	partitionClusterEndpoint := resolvedClusterEndpoint(cluster)
	ep := kusto.NewConnectionStringBuilder(partitionClusterEndpoint)
	if strings.HasPrefix(partitionClusterEndpoint, "https://") {
		// Enables kustainer integration testing
		ep.WithDefaultAzureCredential()
	}
	client, err := kusto.New(ep)
	if err != nil {
		return fmt.Errorf("failed to create Kusto client: %w", err)
	}
	defer client.Close()

	q := kql.New(".show databases")
	result, err := client.Mgmt(ctx, target.HeartbeatDatabase, q)
	if err != nil {
		return fmt.Errorf("failed to query databases: %w", err)
	}

	var databases []string
	for {
		row, errInline, errFinal := result.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			continue
		}
		if errFinal != nil {
			result.Stop()
			return fmt.Errorf("failed to retrieve databases: %w", errFinal)
		}

		var dbr DatabaseRec
		if err := row.ToStruct(&dbr); err != nil {
			result.Stop()
			return fmt.Errorf("failed to parse database: %w", err)
		}
		databases = append(databases, dbr.DatabaseName)
	}
	result.Stop()

	var schema []ADXClusterSchema
	for _, database := range databases {
		s := ADXClusterSchema{
			Database:     database,
			TableSchemas: make(map[string]string),
		}
		q := kql.New(".show database schema")
		result, err := client.Mgmt(ctx, database, q)
		if err != nil {
			return fmt.Errorf("failed to query database schema: %w", err)
		}

		tableColumns := make(map[string][]string)
		for {
			row, errInline, errFinal := result.NextRowOrError()
			if errFinal == io.EOF {
				break
			}
			if errInline != nil {
				continue
			}
			if errFinal != nil {
				result.Stop()
				return fmt.Errorf("failed to retrieve database schema: %w", errFinal)
			}

			var rec DatabaseSchemaRec
			if err := row.ToStruct(&rec); err != nil {
				result.Stop()
				return fmt.Errorf("failed to parse database schema: %w", err)
			}
			if rec.TableName == "" {
				continue
			}
			colDef := fmt.Sprintf("['%s']:%s", rec.ColumnName, rec.ColumnType)
			tableColumns[rec.TableName] = append(tableColumns[rec.TableName], colDef)
		}
		result.Stop()

		for tableName, cols := range tableColumns {
			s.Tables = append(s.Tables, tableName)
			s.TableSchemas[tableName] = strings.Join(cols, ", ")
		}
		sort.Strings(s.Tables)

		q = kql.New(".show functions")
		result, err = client.Mgmt(ctx, database, q)
		if err != nil {
			return fmt.Errorf("failed to query functions: %w", err)
		}

		for {
			row, errInline, errFinal := result.NextRowOrError()
			if errFinal == io.EOF {
				break
			}
			if errInline != nil {
				continue
			}
			if errFinal != nil {
				result.Stop()
				return fmt.Errorf("failed to retrieve functions: %w", errFinal)
			}

			var fn FunctionRec
			if err := row.ToStruct(&fn); err != nil {
				result.Stop()
				return fmt.Errorf("failed to parse function: %w", err)
			}

			isView, viewSchema, err := checkIfFunctionIsView(ctx, client, database, fn.Name)
			if err == nil && isView {
				s.Views = append(s.Views, fn.Name)
				s.TableSchemas[fn.Name] = viewSchema
			}
		}
		result.Stop()
		sort.Strings(s.Views)

		schema = append(schema, s)
	}

	federatedClusterEndpoint := target.Endpoint
	ep = kusto.NewConnectionStringBuilder(federatedClusterEndpoint)
	if strings.HasPrefix(federatedClusterEndpoint, "https://") && target.ManagedIdentityClientId != "" {
		// Enables kustainer integration testing
		ep.WithUserManagedIdentity(target.ManagedIdentityClientId)
	}
	federatedClient, err := kusto.New(ep)
	if err != nil {
		return fmt.Errorf("failed to create Kusto client: %w", err)
	}
	defer federatedClient.Close()

	schemaData, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("failed to marshal schema: %w", err)
	}

	partitionMetadata, err := json.Marshal(*cluster.Spec.Federation.Partitioning)
	if err != nil {
		return fmt.Errorf("failed to marshal partition metadata: %w", err)
	}

	// Use encoding/csv to properly escape CSV fields
	var b strings.Builder
	w := csv.NewWriter(&b)
	w.Write([]string{
		time.Now().Format(time.RFC3339),
		partitionClusterEndpoint,
		string(schemaData),
		string(partitionMetadata),
	})
	w.Flush()
	row := strings.TrimRight(b.String(), "\n") // Remove trailing newline added by csv.Writer
	stmt := kql.New(".ingest inline into table ").
		AddTable(target.HeartbeatTable).
		AddLiteral(" <| ").AddUnsafe(row)
	_, err = federatedClient.Mgmt(ctx, target.HeartbeatDatabase, stmt)
	return err
}

type TableRec struct {
	TableName string `json:"TableName"`
}

type DatabaseRec struct {
	DatabaseName string `json:"DatabaseName"`
}

type DatabaseExistsRec struct {
	Count int64 `kusto:"Count"`
}

type DatabaseSchemaRec struct {
	TableName  string `kusto:"TableName"`
	ColumnName string `kusto:"ColumnName"`
	ColumnType string `kusto:"ColumnType"`
}

type ADXClusterSchema struct {
	Database     string            `json:"database"`
	Tables       []string          `json:"tables"`
	TableSchemas map[string]string `json:"tableSchemas,omitempty"`
	Views        []string          `json:"views"`
}

func (r *AdxReconciler) FederateClusters(ctx context.Context, cluster *adxmonv1.ADXCluster) (ctrl.Result, error) {
	if cluster.Spec.Role == nil ||
		*cluster.Spec.Role != adxmonv1.ClusterRoleFederated ||
		cluster.Spec.Federation == nil ||
		cluster.Spec.Federation.HeartbeatDatabase == nil ||
		cluster.Spec.Federation.HeartbeatTable == nil {
		return ctrl.Result{}, nil
	}

	logger.Infof("ADXCluster %s: starting federation reconciliation", cluster.Spec.ClusterName)

	// Step 1: Create Kusto client
	endpoint := strings.TrimSpace(resolvedClusterEndpoint(cluster))
	if endpoint == "" {
		logger.Infof("ADXCluster %s: endpoint unavailable for federation, requeuing", cluster.Spec.ClusterName)
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}
	ep := kusto.NewConnectionStringBuilder(endpoint)
	if strings.HasPrefix(endpoint, "https://") {
		ep.WithDefaultAzureCredential()
	}
	client, err := kusto.New(ep)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create Kusto client: %w", err)
	}
	defer client.Close()

	// Step 2: Ensure heartbeat table exists
	_, err = ensureHeartbeatTable(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to ensure heartbeat table: %w", err)
	}

	// Step 3: Query heartbeat table
	logger.Infof("ADXCluster %s: querying heartbeat table for federation data", cluster.Spec.ClusterName)
	rows, err := queryHeartbeatTable(ctx, client, *cluster.Spec.Federation.HeartbeatDatabase, *cluster.Spec.Federation.HeartbeatTable, *cluster.Spec.Federation.HeartbeatTTL)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to query heartbeat table: %w", err)
	}

	// Step 4: Parse result
	schemaByEndpoint, _ := parseHeartbeatRows(rows)
	logger.Infof("ADXCluster %s: processed heartbeat data from %d partition clusters", cluster.Spec.ClusterName, len(schemaByEndpoint))

	// Step 5: Unique list of databases
	dbSet := extractDatabasesFromSchemas(schemaByEndpoint)
	var dbSpecs []adxmonv1.ADXClusterDatabaseSpec
	for db := range dbSet {
		dbSpecs = append(dbSpecs, adxmonv1.ADXClusterDatabaseSpec{DatabaseName: db})
	}
	logger.Infof("ADXCluster %s: ensuring %d databases exist for federation", cluster.Spec.ClusterName, len(dbSpecs))
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create default credential: %w", err)
	}
	merged := mergeDatabaseSpecs(cluster.Spec.Databases, dbSpecs)
	tempCluster := cluster.DeepCopy()
	tempCluster.Spec.Databases = merged
	_, err = ensureDatabases(ctx, tempCluster, cred)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to ensure databases: %w", err)
	}

	// Step 6: Map tables and views to endpoints
	dbTableEndpoints := mapTablesToEndpoints(schemaByEndpoint)

	// Step 7: Ensure hub tables exist for all tables and views
	logger.Infof("ADXCluster %s: ensuring hub tables exist for alias functions", cluster.Spec.ClusterName)
	for db, tableMap := range dbTableEndpoints {
		// Build a map with empty schemas to use OTLP default for all tables
		tables := make(map[string]string)
		for table := range tableMap {
			tables[table] = ""
		}
		if err := ensureHubTables(ctx, client, db, tables); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to ensure hub tables for database %s: %w", db, err)
		}
	}

	// Step 8: Generate function definitions
	funcsByDB := generateKustoFunctionDefinitions(dbTableEndpoints)

	// Step 9: For each database, split scripts and execute
	logger.Infof("ADXCluster %s: updating Kusto functions for %d databases", cluster.Spec.ClusterName, len(funcsByDB))
	const maxScriptSize = 1024 * 1024 // 1MB
	for db, funcs := range funcsByDB {
		logger.Infof("ADXCluster %s: executing %d functions for database %s", cluster.Spec.ClusterName, len(funcs), db)
		scripts := splitKustoScripts(funcs, maxScriptSize)
		if err := executeKustoScripts(ctx, client, db, scripts); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to execute Kusto scripts for db %s: %w", db, err)
		}
	}

	logger.Infof("ADXCluster %s: federation reconciliation complete, requeuing in 10 minutes", cluster.Spec.ClusterName)
	return ctrl.Result{RequeueAfter: 10 * time.Minute}, nil
}

// getIMDSMetadata queries Azure IMDS for metadata about the current environment
func getIMDSMetadata(ctx context.Context, imdsURL string) (location, subscriptionId, resourceGroup, aksClusterName string, ok bool) {
	if imdsURL == "" {
		imdsURL = "http://169.254.169.254/metadata/instance?api-version=2021-02-01"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", imdsURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("Metadata", "true")
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return
	}
	defer resp.Body.Close()
	var imds struct {
		Compute struct {
			Location       string `json:"location"`
			SubscriptionId string `json:"subscriptionId"`
			ResourceGroup  string `json:"resourceGroupName"`
			Name           string `json:"name"`
		} `json:"compute"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&imds); err != nil {
		return
	}
	if imds.Compute.Location != "" && imds.Compute.SubscriptionId != "" {
		// For AKS nodes, name will be like "aks-nodepool1-12345678-vmss000000"
		// Extract the AKS cluster name from the prefix
		if parts := strings.Split(imds.Compute.Name, "-"); len(parts) > 2 {
			aksClusterName = parts[0]
		}
		return imds.Compute.Location, imds.Compute.SubscriptionId, imds.Compute.ResourceGroup, aksClusterName, true
	}
	return
}

// recommendedSKUs defines the preferred SKUs in priority order
var recommendedSKUs = []string{
	"Standard_L8as_v3",
	"Standard_L16as_v3",
	"Standard_L32as_v3",
}

// getBestAvailableSKU queries Azure for available SKUs and returns the highest priority one
func getBestAvailableSKU(ctx context.Context, subscriptionId string, region string, cred azcore.TokenCredential) (sku, tier string, err error) {
	clustersClient, err := armkusto.NewClustersClient(subscriptionId, cred, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to create clusters client: %w", err)
	}

	// Get list of SKUs available in the region
	availableSKUs := make(map[string]bool)
	pager := clustersClient.NewListSKUsPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return "", "", fmt.Errorf("failed to get SKUs: %w", err)
		}
		for _, sku := range page.Value {
			if sku.ResourceType != nil && *sku.ResourceType == "clusters" &&
				sku.Tier != nil && *sku.Tier == "Standard" {
				for _, loc := range sku.Locations {
					if strings.EqualFold(*loc, region) {
						availableSKUs[*sku.Name] = true
					}
				}
			}
		}
	}

	// Check recommended SKUs in priority order
	for _, recommendedSKU := range recommendedSKUs {
		if availableSKUs[recommendedSKU] {
			return recommendedSKU, "Standard", nil
		}
	}

	// If no recommended SKU is available, pick the first available Standard SKU
	var allSKUs []string
	for sku := range availableSKUs {
		allSKUs = append(allSKUs, sku)
	}
	if len(allSKUs) > 0 {
		sort.Strings(allSKUs) // Sort for deterministic selection
		return allSKUs[0], "Standard", nil
	}

	return "", "", fmt.Errorf("no suitable SKU found in region %s", region)
}

func ensureAdxProvider(ctx context.Context, cred azcore.TokenCredential, subscriptionID string) (bool, error) {
	// Check if Kusto provider is registered
	providersClient, err := armresources.NewProvidersClient(subscriptionID, cred, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create providers client: %w", err)
	}

	provider, err := providersClient.Get(ctx, "Microsoft.Kusto", nil)
	if err != nil {
		return false, fmt.Errorf("failed to get Kusto provider status: %w", err)
	}

	if provider.RegistrationState != nil && *provider.RegistrationState != "Registered" {
		logger.Infof("Registering Microsoft.Kusto resource provider...")
		_, err = providersClient.Register(ctx, "Microsoft.Kusto", nil)
		if err != nil {
			return false, fmt.Errorf("failed to register Kusto provider: %w", err)
		}

		return false, nil // Registration in progress
	}

	return true, nil
}

func toSku(sku string) *armkusto.AzureSKUName {
	if strings.TrimSpace(sku) == "" {
		return nil
	}
	for _, candidate := range armkusto.PossibleAzureSKUNameValues() {
		if string(candidate) == sku {
			value := candidate
			return &value
		}
	}
	value := armkusto.AzureSKUName(sku)
	return &value
}

func toTier(tier string) *armkusto.AzureSKUTier {
	if strings.TrimSpace(tier) == "" {
		return nil
	}
	for _, candidate := range armkusto.PossibleAzureSKUTierValues() {
		if string(candidate) == tier {
			value := candidate
			return &value
		}
	}
	value := armkusto.AzureSKUTier(tier)
	return &value
}

func toDatabase(subId, clusterName, rgName, location, dbName string) *armkusto.Database {
	return &armkusto.Database{
		Kind:     to.Ptr(armkusto.KindReadWrite),
		Location: to.Ptr(location),
		ID:       to.Ptr(fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Kusto/Clusters/%s/databases/%s", subId, rgName, clusterName, dbName)),
		Name:     to.Ptr(dbName),
		Type:     to.Ptr("Microsoft.Kusto/Clusters/Databases"),
	}
}

// HeartbeatRow represents a row in the heartbeat table
// Schema: Timestamp: datetime, ClusterEndpoint: string, Schema: dynamic, PartitionMetadata: dynamic
type HeartbeatRow struct {
	Timestamp         time.Time         `kusto:"Timestamp"`
	ClusterEndpoint   string            `kusto:"ClusterEndpoint"`
	Schema            json.RawMessage   `kusto:"Schema"`
	PartitionMetadata map[string]string `kusto:"PartitionMetadata"`
}

// Helper: Query the heartbeat table for recent entries
func queryHeartbeatTable(ctx context.Context, client *kusto.Client, database, table, ttl string) ([]HeartbeatRow, error) {
	query := fmt.Sprintf("%s | where Timestamp > ago(%s)", table, ttl)
	result, err := client.Query(ctx, database, kql.New("").AddUnsafe(query))
	if err != nil {
		return nil, fmt.Errorf("failed to query heartbeat table: %w", err)
	}
	defer result.Stop()

	var rows []HeartbeatRow
	for {
		row, errInline, errFinal := result.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			continue
		}
		if errFinal != nil {
			return nil, fmt.Errorf("failed to read heartbeat row: %w", errFinal)
		}
		var h HeartbeatRow
		if err := row.ToStruct(&h); err != nil {
			return nil, fmt.Errorf("failed to parse heartbeat row: %w", err)
		}
		rows = append(rows, h)
	}
	return rows, nil
}

// Helper: Parse heartbeat rows into endpoint->schema and endpoint->partitionMetadata
func parseHeartbeatRows(rows []HeartbeatRow) (map[string][]ADXClusterSchema, map[string]map[string]string) {
	schemaByEndpoint := make(map[string][]ADXClusterSchema)
	partitionMetaByEndpoint := make(map[string]map[string]string)
	for _, row := range rows {
		var schema []ADXClusterSchema
		if err := json.Unmarshal(row.Schema, &schema); err != nil {
			logger.Errorf("failed to parse schema for endpoint %s: %v", row.ClusterEndpoint, err)
			continue
		}
		schemaByEndpoint[row.ClusterEndpoint] = schema
		partitionMetaByEndpoint[row.ClusterEndpoint] = row.PartitionMetadata
	}
	return schemaByEndpoint, partitionMetaByEndpoint
}

// Helper: Extract unique databases from schemas
func extractDatabasesFromSchemas(schemas map[string][]ADXClusterSchema) map[string]struct{} {
	dbSet := make(map[string]struct{})
	for _, dbSchemas := range schemas {
		for _, schema := range dbSchemas {
			dbSet[schema.Database] = struct{}{}
		}
	}
	return dbSet
}

func mergeDatabaseSpecs(userDbs, discoveredDbs []adxmonv1.ADXClusterDatabaseSpec) []adxmonv1.ADXClusterDatabaseSpec {
	combined := make(map[string]adxmonv1.ADXClusterDatabaseSpec, len(userDbs)+len(discoveredDbs))
	for _, db := range userDbs {
		combined[db.DatabaseName] = db
	}
	for _, db := range discoveredDbs {
		if existing, ok := combined[db.DatabaseName]; ok {
			if existing.TelemetryType == "" {
				combined[db.DatabaseName] = db
			}
			continue
		}
		combined[db.DatabaseName] = db
	}
	merged := make([]adxmonv1.ADXClusterDatabaseSpec, 0, len(combined))
	for _, db := range combined {
		merged = append(merged, db)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].DatabaseName < merged[j].DatabaseName })
	return merged
}

func collectInventoryByDatabase(schemas map[string][]ADXClusterSchema) map[string]map[string]string {
	inventory := make(map[string]map[string]string)
	for _, dbSchemas := range schemas {
		for _, schema := range dbSchemas {
			if inventory[schema.Database] == nil {
				inventory[schema.Database] = make(map[string]string)
			}
			for _, table := range schema.Tables {
				if strings.TrimSpace(table) == "" {
					continue
				}
				if s, ok := schema.TableSchemas[table]; ok {
					inventory[schema.Database][table] = s
				} else {
					if _, exists := inventory[schema.Database][table]; !exists {
						inventory[schema.Database][table] = ""
					}
				}
			}
			for _, view := range schema.Views {
				if strings.TrimSpace(view) == "" {
					continue
				}
				if s, ok := schema.TableSchemas[view]; ok {
					inventory[schema.Database][view] = s
				} else {
					if _, exists := inventory[schema.Database][view]; !exists {
						inventory[schema.Database][view] = ""
					}
				}
			}
		}
	}
	return inventory
}

func databaseExists(ctx context.Context, cluster *adxmonv1.ADXCluster, database string) (bool, error) {
	endpoint := strings.TrimSpace(resolvedClusterEndpoint(cluster))
	if endpoint == "" {
		return false, fmt.Errorf("cluster endpoint is empty")
	}
	csb := kusto.NewConnectionStringBuilder(endpoint)
	if strings.HasPrefix(endpoint, "https://") {
		csb.WithDefaultAzureCredential()
	}
	client, err := kusto.New(csb)
	if err != nil {
		return false, fmt.Errorf("failed to create Kusto client: %w", err)
	}
	defer client.Close()

	stmt := kql.New(".show databases | where DatabaseName == ").AddString(database).AddLiteral(" | count")
	result, err := client.Mgmt(ctx, "", stmt)
	if err != nil {
		return false, fmt.Errorf("failed to query database existence: %w", err)
	}
	defer result.Stop()

	for {
		row, errInline, errFinal := result.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			continue
		}
		if errFinal != nil {
			return false, fmt.Errorf("failed to read database existence: %w", errFinal)
		}
		var rec DatabaseExistsRec
		if err := row.ToStruct(&rec); err != nil {
			return false, fmt.Errorf("failed to parse database existence: %w", err)
		}
		return rec.Count > 0, nil
	}

	return false, nil
}

func ensureHubTables(ctx context.Context, client *kusto.Client, database string, tables map[string]string) error {
	var tableNames []string
	for t := range tables {
		tableNames = append(tableNames, t)
	}
	sort.Strings(tableNames)

	for _, table := range tableNames {
		schema := tables[table]
		if strings.TrimSpace(table) == "" {
			continue
		}
		exists, err := tableExists(ctx, client, database, table)
		if err != nil {
			return fmt.Errorf("failed to check table existence for %s.%s: %w", database, table, err)
		}
		if exists {
			continue
		}

		schemaDef := otlpHubSchemaDefinition
		if schema != "" {
			schemaDef = schema
		}

		stmt := kql.New(".create table ").
			AddUnsafe(table).
			AddLiteral(" (").
			AddUnsafe(schemaDef).
			AddLiteral(")")
		if _, err := client.Mgmt(ctx, database, stmt); err != nil {
			return fmt.Errorf("failed to create table %s.%s: %w", database, table, err)
		}
		logger.Infof("Created hub table %s.%s using schema: %s", database, table, schemaDef)
	}
	return nil
}

func tableExists(ctx context.Context, client *kusto.Client, database, table string) (bool, error) {
	query := kql.New(".show tables | where TableName == ").AddString(table).AddLiteral(" | count")
	result, err := client.Mgmt(ctx, database, query)
	if err != nil {
		return false, fmt.Errorf("failed to query table existence: %w", err)
	}
	defer result.Stop()

	for {
		row, errInline, errFinal := result.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			continue
		}
		if errFinal != nil {
			return false, fmt.Errorf("failed to read table existence: %w", errFinal)
		}
		var rec DatabaseExistsRec
		if err := row.ToStruct(&rec); err != nil {
			return false, fmt.Errorf("failed to parse table existence: %w", err)
		}
		return rec.Count > 0, nil
	}

	return false, nil
}

// Helper: Map tables and views to endpoints for each database
func mapTablesToEndpoints(schemas map[string][]ADXClusterSchema) map[string]map[string][]string {
	dbTableEndpoints := make(map[string]map[string][]string)
	for endpoint, dbSchemas := range schemas {
		for _, schema := range dbSchemas {
			db := schema.Database
			if dbTableEndpoints[db] == nil {
				dbTableEndpoints[db] = make(map[string][]string)
			}
			// Include both tables and views from spoke clusters
			for _, table := range schema.Tables {
				dbTableEndpoints[db][table] = append(dbTableEndpoints[db][table], endpoint)
			}
			for _, view := range schema.Views {
				dbTableEndpoints[db][view] = append(dbTableEndpoints[db][view], endpoint)
			}
		}
	}
	return dbTableEndpoints
}

// Helper: Generate Kusto function definitions for each table
func generateKustoFunctionDefinitions(dbTableEndpoints map[string]map[string][]string) map[string][]string {
	funcsByDB := make(map[string][]string)
	for db, tableMap := range dbTableEndpoints {
		for table, endpoints := range tableMap {
			var clusters []string
			for _, ep := range endpoints {
				clusters = append(clusters, fmt.Sprintf("cluster('%s').database('%s')", ep, db))
			}
			macro := fmt.Sprintf("macro-expand entity_group [%s] as X ( X.%s )", strings.Join(clusters, ", "), table)
			funcDef := fmt.Sprintf(".create-or-alter function %s() { %s }", table, macro)
			funcsByDB[db] = append(funcsByDB[db], funcDef)
		}
	}
	return funcsByDB
}

// Helper: Split Kusto scripts if they approach 1MB and add script preamble and comments
func splitKustoScripts(funcs []string, maxSize int) [][]string {
	const scriptPreamble = ".execute database script with (ContinueOnErrors=true)\n<|\n"
	var scripts [][]string
	var current []string
	currentSize := len(scriptPreamble)
	for _, f := range funcs {
		// Add comment and newline before each function
		funcWithComment := "//\n" + f + "\n"
		sz := len(funcWithComment)
		if currentSize+sz > maxSize && len(current) > 0 {
			scripts = append(scripts, current)
			current = nil
			currentSize = len(scriptPreamble)
		}
		current = append(current, funcWithComment)
		currentSize += sz
	}
	if len(current) > 0 {
		scripts = append(scripts, current)
	}
	return scripts
}

// Helper: Execute Kusto scripts in a database, using the .execute database script preamble
func executeKustoScripts(ctx context.Context, client *kusto.Client, database string, scripts [][]string) error {
	const scriptPreamble = ".execute database script with (ContinueOnErrors=true)\n<|\n"
	for _, script := range scripts {
		fullScript := scriptPreamble + strings.Join(script, "")
		_, err := client.Mgmt(ctx, database, kql.New("").AddUnsafe(fullScript))
		if err != nil {
			return fmt.Errorf("failed to execute Kusto script: %w", err)
		}
	}
	return nil
}

type FunctionRec struct {
	Name       string `kusto:"Name"`
	Parameters string `kusto:"Parameters"`
	Body       string `kusto:"Body"`
	Folder     string `kusto:"Folder"`
	DocString  string `kusto:"DocString"`
}

type FunctionKind struct {
	Kind string `kusto:"FunctionKind"`
}

type FunctionSchemaRec struct {
	Kind          string          `kusto:"FunctionKind"`
	OutputColumns json.RawMessage `kusto:"OutputColumns"`
}

type OutputColumn struct {
	Name    string `json:"Name"`
	CslType string `json:"CslType"`
}

func checkIfFunctionIsView(ctx context.Context, client *kusto.Client, database, functionName string) (bool, string, error) {
	q := kql.New(".show function ").AddUnsafe(functionName).AddLiteral(" schema as json | project FunctionKind = todynamic(Schema).FunctionKind, OutputColumns = todynamic(Schema).OutputColumns")
	result, err := client.Mgmt(ctx, database, q)
	if err != nil {
		return false, "", fmt.Errorf("failed to query function schema: %w", err)
	}
	defer result.Stop()

	for {
		row, errInline, errFinal := result.NextRowOrError()
		if errFinal == io.EOF {
			break
		}
		if errInline != nil {
			continue
		}
		if errFinal != nil {
			return false, "", fmt.Errorf("failed to retrieve function schema: %w", errFinal)
		}

		var rec FunctionSchemaRec
		if err := row.ToStruct(&rec); err != nil {
			return false, "", fmt.Errorf("failed to parse function schema: %w", err)
		}

		if rec.Kind != "ViewFunction" {
			return false, "", nil
		}

		var cols []OutputColumn
		if err := json.Unmarshal(rec.OutputColumns, &cols); err != nil {
			return false, "", fmt.Errorf("failed to parse output columns: %w", err)
		}

		var parts []string
		for _, col := range cols {
			parts = append(parts, fmt.Sprintf("['%s']:%s", col.Name, col.CslType))
		}
		return true, strings.Join(parts, ", "), nil
	}

	return false, "", nil
}
