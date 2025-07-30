package operator

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
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

// entityGroupNameRegex validates entity-group names for use in KQL commands.
// Entity-group names must be 1-256 characters and contain only alphanumeric, underscore, and hyphen characters.
var entityGroupNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,256}$`)

type AdxReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	// ADXClusterCreatingReason denotes a cluster that is being configured.
	ADXClusterCreatingReason = "Creating"
	// ADXClusterWaitingReason denotes a cluster that is fully configured and is waiting to become available.
	ADXClusterWaitingReason = "Waiting"
	// PartitionsSuffix is the suffix used for entity-group names
	PartitionsSuffix = "_Partitions"
)

// isValidEntityGroupName validates that an entity-group name is safe to use in KQL commands
func isValidEntityGroupName(name string) bool {
	return entityGroupNameRegex.MatchString(name)
}

func (r *AdxReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cluster adxmonv1.ADXCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
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

	setClusterStatusCondition := func(reason, message string) error {
		logger.Infof("ADXCluster %s: updating status - %s: %s", cluster.Spec.ClusterName, reason, message)
		c := metav1.Condition{
			Type:               adxmonv1.ADXClusterConditionOwner,
			Status:             metav1.ConditionUnknown,
			ObservedGeneration: cluster.GetGeneration(),
			LastTransitionTime: metav1.Now(),
			Reason:             reason,
			Message:            message,
		}
		if meta.SetStatusCondition(&cluster.Status.Conditions, c) {
			if err := r.Status().Update(ctx, cluster); err != nil {
				return fmt.Errorf("failed to update status: %w", err)
			}
		}
		return nil
	}

	// Set an initial status to communicate our current state.
	if err := setClusterStatusCondition(ADXClusterCreatingReason, fmt.Sprintf("Creating ADX cluster %s", cluster.Name)); err != nil {
		return ctrl.Result{}, err
	}
	// ADXCluster has many configuration options, but also supports zero-config; however, in order to create a functioning cluster,
	// we need to ensure certain options are specified, either by the user or by default values.
	if err := applyDefaults(ctx, r, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to apply defaults: %w", err)
	}
	// If the cluster already has an Endpoint, we assume it exists (either user-provided or previously created),
	// so the create routine has no work left to do.
	if cluster.Spec.Endpoint != "" {
		logger.Infof("ADXCluster %s: endpoint already exists (%s), marking as ready", cluster.Spec.ClusterName, cluster.Spec.Endpoint)
		c := metav1.Condition{
			Type:               adxmonv1.ADXClusterConditionOwner,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: cluster.GetGeneration(),
			LastTransitionTime: metav1.Now(),
			Reason:             "ClusterReady",
			Message:            fmt.Sprintf("Cluster %s is ready", cluster.Spec.ClusterName),
		}
		if meta.SetStatusCondition(&cluster.Status.Conditions, c) {
			if err := r.Status().Update(ctx, cluster); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
			}
		}
		return ctrl.Result{}, nil // Goal state reached, cluster is ready
	}

	// Ensure the ADX provider is registered for this subscription.
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create default credential: %w", err)
	}
	registered, err := ensureAdxProvider(ctx, cred, cluster.Spec.Provision.SubscriptionId)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to ensure Kusto provider is registered: %w", err)
	}
	if !registered {
		logger.Infof("ADXCluster %s: waiting for provider registration, requeuing in 5 minutes", cluster.Spec.ClusterName)
		_ = setClusterStatusCondition(ADXClusterCreatingReason, fmt.Sprintf("Registering provider for subscription %s", cluster.Spec.Provision.SubscriptionId))
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
		// We must wait for the cluster to be in a ready state before we can continue configuration.
		logger.Infof("ADXCluster %s: cluster not ready yet, requeuing in 1 minute", cluster.Spec.ClusterName)
		_ = setClusterStatusCondition(ADXClusterCreatingReason, fmt.Sprintf("Provisioning ADX cluster %s", cluster.Spec.ClusterName))
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	logger.Infof("ADXCluster %s: ensuring databases", cluster.Spec.ClusterName)
	dbCreated, err := ensureDatabases(ctx, cluster, cred)
	if err != nil {
		return ctrl.Result{}, err
	}
	if dbCreated {
		// Wait for databases to be created.
		logger.Infof("ADXCluster %s: databases created, requeuing in 1 minute", cluster.Spec.ClusterName)
		_ = setClusterStatusCondition(ADXClusterCreatingReason, fmt.Sprintf("Provisioning ADX cluster %s databases", cluster.Spec.ClusterName))
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}
	_ = setClusterStatusCondition(ADXClusterWaitingReason, "Provisioning ADX clusters")

	logger.Infof("ADXCluster %s: ensuring heartbeat table", cluster.Spec.ClusterName)
	tblCreated, err := ensureHeartbeatTable(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}
	if tblCreated {
		// Tables are created synchronously, no waiting is necessary.
		logger.Infof("ADXCluster %s: heartbeat table created", cluster.Spec.ClusterName)
		_ = setClusterStatusCondition(ADXClusterCreatingReason, "Provisioned Heartbeat Table")
	}

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

// databaseExists checks if a database exists in the Kusto cluster by querying .show databases
func databaseExists(ctx context.Context, cluster *adxmonv1.ADXCluster, databaseName string) (bool, error) {
	ep := kusto.NewConnectionStringBuilder(cluster.Spec.Endpoint)
	if strings.HasPrefix(cluster.Spec.Endpoint, "https://") {
		// Enables kustainer integration testing
		ep.WithDefaultAzureCredential()
	}
	client, err := kusto.New(ep)
	if err != nil {
		return false, fmt.Errorf("failed to create Kusto client: %w", err)
	}

	q := kql.New(".show databases | where DatabaseName == ParamDatabaseName | count")
	params := kql.NewParameters().AddString("ParamDatabaseName", databaseName)

	// Use any database for the management command - we'll use the first database or default to "master"
	queryDatabase := "master"
	if len(cluster.Spec.Databases) > 0 {
		queryDatabase = cluster.Spec.Databases[0].DatabaseName
	}

	result, err := client.Mgmt(ctx, queryDatabase, q, kusto.QueryParameters(params))
	if err != nil {
		return false, fmt.Errorf("failed to check database existence: %w", err)
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
			return false, fmt.Errorf("failed to retrieve databases: %w", errFinal)
		}

		var db DatabaseExistsRec
		if err := row.ToStruct(&db); err != nil {
			return false, fmt.Errorf("failed to parse database record: %w", err)
		}
		if db.Count > 0 {
			return true, nil
		}
	}

	return false, nil
}

// ensureDatabases creates databases if they do not exist, returns true if any were created
func ensureDatabases(ctx context.Context, cluster *adxmonv1.ADXCluster, cred azcore.TokenCredential) (bool, error) {
	databasesClient, err := armkusto.NewDatabasesClient(cluster.Spec.Provision.SubscriptionId, cred, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create databases client: %w", err)
	}
	databases := cluster.Spec.Databases
	if cluster.Spec.Federation.HeartbeatDatabase != nil {
		databases = append(databases, adxmonv1.ADXClusterDatabaseSpec{
			DatabaseName:  *cluster.Spec.Federation.HeartbeatDatabase,
			TelemetryType: adxmonv1.DatabaseTelemetryLogs,
		})
	}
	var dbCreated bool
	for _, db := range databases {
		// First check if the database already exists using Kusto query
		exists, err := databaseExists(ctx, cluster, db.DatabaseName)
		if err != nil {
			logger.Warnf("Failed to check if database %s exists using Kusto query: %v, falling back to ARM API", db.DatabaseName, err)
		} else if exists {
			logger.Infof("Database %s already exists in cluster %s", db.DatabaseName, cluster.Spec.ClusterName)
			continue
		}

		// If database doesn't exist or we couldn't check, use ARM API to verify availability and create
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
	ep := kusto.NewConnectionStringBuilder(cluster.Spec.Endpoint)
	if strings.HasPrefix(cluster.Spec.Endpoint, "https://") {
		// Enables kustainer integration testing
		ep.WithDefaultAzureCredential()
	}
	client, err := kusto.New(ep)
	if err != nil {
		return false, fmt.Errorf("failed to create Kusto client: %w", err)
	}

	q := kql.New(".show tables | where TableName == ParamTableName | count")
	params := kql.NewParameters().AddString("ParamTableName", *cluster.Spec.Federation.HeartbeatTable)

	result, err := client.Mgmt(ctx, *cluster.Spec.Federation.HeartbeatDatabase, q, kusto.QueryParameters(params))
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
			return false, fmt.Errorf("failed to retrieve tables: %w", err)
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
		logger.Infof("ADXCluster %s: no provision spec, marking as complete", cluster.Spec.ClusterName)
		c := metav1.Condition{
			Type:               adxmonv1.ADXClusterConditionOwner,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: cluster.GetGeneration(),
			LastTransitionTime: metav1.Now(),
			Reason:             "Complete",
			Message:            "Cluster is already reconciled",
		}
		if meta.SetStatusCondition(&cluster.Status.Conditions, c) {
			if err := r.Status().Update(ctx, cluster); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
			}
		}
		return ctrl.Result{}, nil // Since we don't have a previous state, we can't update
	}
	appliedProvisionState, err := cluster.Spec.Provision.LoadAppliedProvisioningState()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to load applied provisioning state: %w", err)
	}
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
	if diffIdentities(resp, appliedProvisionState, cluster, &clusterUpdate) {
		updated = true
	}

	if !updated {
		logger.Infof("ADXCluster %s: no updates needed, marking as complete", cluster.Spec.ClusterName)
		c := metav1.Condition{
			Type:               adxmonv1.ADXClusterConditionOwner,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: cluster.GetGeneration(),
			LastTransitionTime: metav1.Now(),
			Reason:             "Complete",
			Message:            "Cluster is already reconciled",
		}
		if meta.SetStatusCondition(&cluster.Status.Conditions, c) {
			if err := r.Status().Update(ctx, cluster); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
			}
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

func (r *AdxReconciler) CheckStatus(ctx context.Context, cluster *adxmonv1.ADXCluster) (ctrl.Result, error) {
	logger.Infof("ADXCluster %s: checking cluster status", cluster.Spec.ClusterName)

	// Note: Like UpdateCluster, this method handles 403 Forbidden errors gracefully
	// to support federation scenarios with limited permissions.

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
			logger.Warnf("ADXCluster %s: insufficient permissions to check cluster status (403 Forbidden), assuming cluster is ready for federation tasks", cluster.Spec.ClusterName)
			c := metav1.Condition{
				Type:               adxmonv1.ADXClusterConditionOwner,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: cluster.GetGeneration(),
				LastTransitionTime: metav1.Now(),
				Reason:             "PermissionRestricted",
				Message:            "Cluster status check restricted due to insufficient permissions, federation features remain available",
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
	if resp.Properties == nil || resp.Properties.State == nil {
		logger.Infof("ADXCluster %s: cluster properties not available yet, requeuing in 1 minute", cluster.Spec.ClusterName)
		return ctrl.Result{RequeueAfter: time.Minute}, nil // Not ready yet
	}

	logger.Infof("ADXCluster %s: cluster state is %s", cluster.Spec.ClusterName, string(*resp.Properties.State))

	// If the cluster is running, we're done
	if *resp.Properties.State == armkusto.StateRunning {
		logger.Infof("ADXCluster %s: cluster is running, marking as ready", cluster.Spec.ClusterName)
		c := metav1.Condition{
			Type:               adxmonv1.ADXClusterConditionOwner,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: cluster.GetGeneration(),
			LastTransitionTime: metav1.Now(),
			Reason:             "ClusterReady",
			Message:            fmt.Sprintf("Cluster %s is ready", cluster.Spec.ClusterName),
		}
		if meta.SetStatusCondition(&cluster.Status.Conditions, c) {
			if err := r.Status().Update(ctx, cluster); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
			}
		}

		if err := cluster.Spec.Provision.StoreAppliedProvisioningState(); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to store applied provisioning state: %w", err)
		}
		cluster.Spec.Endpoint = *resp.Properties.URI
		if err := r.Update(ctx, cluster); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update cluster endpoint: %w", err)
		}

		return ctrl.Result{}, nil // Goal state reached
	}

	// If the cluster has failed to provision, we're done
	if resp.Properties.ProvisioningState != nil && *resp.Properties.ProvisioningState == armkusto.ProvisioningStateFailed {
		logger.Errorf("ADXCluster %s: cluster provisioning failed", cluster.Spec.ClusterName)
		c := metav1.Condition{
			Type:               adxmonv1.ADXClusterConditionOwner,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: cluster.GetGeneration(),
			LastTransitionTime: metav1.Now(),
			Reason:             string(armkusto.ProvisioningStateFailed),
			Message:            "Cluster creation failed",
		}
		if meta.SetStatusCondition(&cluster.Status.Conditions, c) {
			if err := r.Status().Update(ctx, cluster); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update status: %w", err)
			}
		}
		return ctrl.Result{}, nil // This is a terminal failure
	}

	// For all other states, we can safely continue to wait
	logger.Infof("ADXCluster %s: cluster not ready yet (state: %s), requeuing in 1 minute", cluster.Spec.ClusterName, string(*resp.Properties.State))
	return ctrl.Result{RequeueAfter: time.Minute}, nil // Not ready yet
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
	partitionClusterEndpoint := cluster.Spec.Endpoint
	ep := kusto.NewConnectionStringBuilder(partitionClusterEndpoint)
	if strings.HasPrefix(partitionClusterEndpoint, "https://") {
		// Enables kustainer integration testing
		ep.WithDefaultAzureCredential()
	}
	client, err := kusto.New(ep)
	if err != nil {
		return fmt.Errorf("failed to create Kusto client: %w", err)
	}

	q := kql.New(".show databases")
	result, err := client.Mgmt(ctx, target.HeartbeatDatabase, q)
	if err != nil {
		return fmt.Errorf("failed to query databases: %w", err)
	}
	defer result.Stop()

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
			return fmt.Errorf("failed to retrieve databases: %w", err)
		}

		var dbr DatabaseRec
		if err := row.ToStruct(&dbr); err != nil {
			return fmt.Errorf("failed to parse database: %w", err)
		}
		databases = append(databases, dbr.DatabaseName)
	}

	var schema []ADXClusterSchema
	for _, database := range databases {
		s := ADXClusterSchema{
			Database: database,
		}
		q := kql.New(".show tables")
		result, err := client.Mgmt(ctx, database, q)
		if err != nil {
			return fmt.Errorf("failed to query tables: %w", err)
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
				return fmt.Errorf("failed to retrieve tables: %w", err)
			}

			var tbl TableRec
			if err := row.ToStruct(&tbl); err != nil {
				return fmt.Errorf("failed to parse table: %w", err)
			}
			s.Tables = append(s.Tables, tbl.TableName)
		}

		q = kql.New(".show functions")
		result, err = client.Mgmt(ctx, database, q)
		if err != nil {
			return fmt.Errorf("failed to query functions: %w", err)
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
				return fmt.Errorf("failed to retrieve functions: %w", errFinal)
			}

			var fn FunctionRec
			if err := row.ToStruct(&fn); err != nil {
				return fmt.Errorf("failed to parse function: %w", err)
			}

			isView, err := checkIfFunctionIsView(ctx, client, database, fn.Name)
			if err == nil && isView {
				s.Views = append(s.Views, fn.Name)
			}
		}

		schema = append(schema, s)
	}

	federatedClusterEndpoint := target.Endpoint
	ep = kusto.NewConnectionStringBuilder(federatedClusterEndpoint)
	if strings.HasPrefix(federatedClusterEndpoint, "https://") && target.ManagedIdentityClientId != "" {
		// Enables kustainer integration testing
		ep.WithUserManagedIdentity(target.ManagedIdentityClientId)
	}
	client, err = kusto.New(ep)
	if err != nil {
		return fmt.Errorf("failed to create Kusto client: %w", err)
	}

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
	_, err = client.Mgmt(ctx, target.HeartbeatDatabase, stmt)
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

type ADXClusterSchema struct {
	Database string   `json:"database"`
	Tables   []string `json:"tables"`
	Views    []string `json:"views"`
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
	ep := kusto.NewConnectionStringBuilder(cluster.Spec.Endpoint)
	if strings.HasPrefix(cluster.Spec.Endpoint, "https://") {
		ep.WithDefaultAzureCredential()
	}
	client, err := kusto.New(ep)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create Kusto client: %w", err)
	}

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
	cluster.Spec.Databases = dbSpecs
	_, err = ensureDatabases(ctx, cluster, cred)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to ensure databases: %w", err)
	}

	// Step 5.5: Ensure entity-groups for federation
	logger.Infof("ADXCluster %s: ensuring entity-groups for %d databases", cluster.Spec.ClusterName, len(dbSet))
	if err := ensureEntityGroups(ctx, client, dbSet, schemaByEndpoint); err != nil {
		logger.Errorf("ADXCluster %s: failed to ensure entity-groups: %v", cluster.Spec.ClusterName, err)
		// Continue with function generation even if entity-groups fail
	}

	// Step 6: Map tables to endpoints
	dbTableEndpoints := mapTablesToEndpoints(schemaByEndpoint)

	// Step 7: Generate function definitions
	funcsByDB := generateKustoFunctionDefinitions(dbTableEndpoints)

	// Step 8/9: For each database, split scripts and execute
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

func applyDefaults(ctx context.Context, r *AdxReconciler, cluster *adxmonv1.ADXCluster) error {
	// Get IMDS metadata for defaults
	imdsLocation, imdsSub, imdsRG, _, imdsOK := getIMDSMetadata(ctx, "")

	// Authenticate early since we need it for SKU selection
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("failed to create default credential: %w", err)
	}

	updated := false

	// Initialize provision and status if needed
	if cluster.Spec.Provision == nil {
		logger.Infof("Zero config specified for cluster, discovering defaults")
		cluster.Spec.Provision = &adxmonv1.ADXClusterProvisionSpec{}
		updated = true
	}

	// Fill in defaults from IMDS when available
	if cluster.Spec.Provision.Location == "" && imdsOK {
		logger.Infof("Setting location to %s for cluster", imdsLocation)
		cluster.Spec.Provision.Location = imdsLocation
		updated = true
	}

	if cluster.Spec.Provision.SubscriptionId == "" && imdsOK {
		logger.Infof("Setting subscription ID to %s for cluster", imdsSub)
		cluster.Spec.Provision.SubscriptionId = imdsSub
		updated = true
	}

	if cluster.Spec.Provision.ResourceGroup == "" && imdsOK {
		logger.Infof("Setting resource group to %s for cluster", imdsRG)
		cluster.Spec.Provision.ResourceGroup = imdsRG
		updated = true
	}

	// Get best available SKU if not specified and Endpoint isn't already set, which means the cluster is already provisioned
	if cluster.Spec.Provision.SkuName == "" && cluster.Spec.Endpoint == "" {
		sku, tier, err := getBestAvailableSKU(ctx, cluster.Spec.Provision.SubscriptionId, cluster.Spec.Provision.Location, cred)
		if err != nil {
			return fmt.Errorf("failed to determine SKU: %w", err)
		}
		cluster.Spec.Provision.SkuName = sku
		cluster.Spec.Provision.Tier = tier
		updated = true
	}

	if len(cluster.Spec.Databases) == 0 {
		// Default to two databases if none specified
		cluster.Spec.Databases = []adxmonv1.ADXClusterDatabaseSpec{
			{
				DatabaseName:  "Metrics",
				TelemetryType: adxmonv1.DatabaseTelemetryMetrics,
			},
			{
				DatabaseName:  "Logs",
				TelemetryType: adxmonv1.DatabaseTelemetryLogs,
			},
		}
		updated = true
	}

	// Persist any changes back to the API server
	if updated {
		if err := r.Update(ctx, cluster); err != nil {
			logger.Errorf("Failed to update cluster information: %v", err)
			return fmt.Errorf("failed to update cluster spec: %w", err)
		}
	}

	return nil
}

func diffSkus(resp armkusto.ClustersClientGetResponse, appliedProvisionState *adxmonv1.AppliedProvisionState, cluster *adxmonv1.ADXCluster) (armkusto.Cluster, bool) {
	clusterUpdate := armkusto.Cluster{
		Location:   resp.Location,
		SKU:        resp.SKU,
		Identity:   resp.Identity,
		Properties: resp.Properties,
	}
	if resp.SKU == nil {
		return clusterUpdate, false
	}

	var updated bool
	if resp.SKU.Name != nil && string(*resp.SKU.Name) == appliedProvisionState.SkuName && appliedProvisionState.SkuName != cluster.Spec.Provision.SkuName {
		logger.Infof("Updating cluster %s sku from %s to %s", *resp.Name, string(*resp.SKU.Name), cluster.Spec.Provision.SkuName)
		clusterUpdate.SKU.Name = toSku(cluster.Spec.Provision.SkuName)
		updated = true
	}
	if resp.SKU.Tier != nil && string(*resp.SKU.Tier) == appliedProvisionState.Tier && appliedProvisionState.Tier != cluster.Spec.Provision.Tier {
		logger.Infof("Updating cluster %s tier from %s to %s", *resp.Name, string(*resp.SKU.Tier), cluster.Spec.Provision.Tier)
		clusterUpdate.SKU.Tier = toTier(cluster.Spec.Provision.Tier)
		updated = true
	}
	return clusterUpdate, updated
}

func diffIdentities(resp armkusto.ClustersClientGetResponse, appliedProvisionState *adxmonv1.AppliedProvisionState, cluster *adxmonv1.ADXCluster, clusterUpdate *armkusto.Cluster) bool {
	var updated bool
	if resp.Identity != nil && resp.Identity.Type != nil && *resp.Identity.Type == armkusto.IdentityTypeUserAssigned && appliedProvisionState.UserAssignedIdentities != nil {
		// This block is responsible for reconciling the set of user-assigned identities on the cluster.
		// The goal is to ensure that only the identities managed by the operator (i.e., those specified in the CRD)
		// are added or removed, without disturbing any identities that may have been added out-of-band (e.g., manually in Azure).
		//
		// To do this, we compare the set of user-assigned identities from the last-applied state (appliedProvisionState.UserAssignedIdentities)
		// with the current desired state from the CRD (cluster.Spec.Provision.UserAssignedIdentities):
		//   - For any identity present in the CRD but not in the applied state, we add it to resp.Identity.UserAssignedIdentities
		//     (but only if it isn't already present, to avoid overwriting manual additions).
		//   - For any identity present in the applied state but not in the CRD, we remove it from resp.Identity.UserAssignedIdentities
		//     (but only if it is present, and only if it was previously managed by the operator).
		//
		// This approach ensures that we do not inadvertently remove or alter any identities that a user may have added
		// directly in Azure or through other means. Only the identities that the operator is responsible for are managed here.
		// The 'updated' flag is set to true if any changes are made, so that the update can be persisted.

		// Build sets for comparison
		currentSet := make(map[string]struct{})
		for _, id := range cluster.Spec.Provision.UserAssignedIdentities {
			currentSet[id] = struct{}{}
		}
		appliedSet := make(map[string]struct{})
		for _, id := range appliedProvisionState.UserAssignedIdentities {
			appliedSet[id] = struct{}{}
		}
		// Additions: in currentSet but not in appliedSet
		for id := range currentSet {
			if _, wasApplied := appliedSet[id]; !wasApplied {
				if resp.Identity.UserAssignedIdentities == nil {
					clusterUpdate.Identity.UserAssignedIdentities = make(map[string]*armkusto.ComponentsSgqdofSchemasIdentityPropertiesUserassignedidentitiesAdditionalproperties)
				}
				if _, exists := resp.Identity.UserAssignedIdentities[id]; !exists {
					clusterUpdate.Identity.UserAssignedIdentities[id] = &armkusto.ComponentsSgqdofSchemasIdentityPropertiesUserassignedidentitiesAdditionalproperties{}
					updated = true
				}
			}
		}
		// Deletions: in appliedSet but not in currentSet
		for id := range appliedSet {
			if _, isCurrent := currentSet[id]; !isCurrent {
				if resp.Identity.UserAssignedIdentities != nil {
					if _, exists := resp.Identity.UserAssignedIdentities[id]; exists {
						delete(clusterUpdate.Identity.UserAssignedIdentities, id)
						updated = true
					}
				}
			}
		}
	}
	return updated
}

func toSku(sku string) *armkusto.AzureSKUName {
	for _, v := range armkusto.PossibleAzureSKUNameValues() {
		if string(v) == sku {
			return &v
		}
	}
	return nil
}

func toTier(tier string) *armkusto.AzureSKUTier {
	for _, v := range armkusto.PossibleAzureSKUTierValues() {
		if string(v) == tier {
			return &v
		}
	}
	return nil
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

// Helper: Map tables to endpoints for each database
func mapTablesToEndpoints(schemas map[string][]ADXClusterSchema) map[string]map[string][]string {
	dbTableEndpoints := make(map[string]map[string][]string)
	for endpoint, dbSchemas := range schemas {
		for _, schema := range dbSchemas {
			db := schema.Database
			if dbTableEndpoints[db] == nil {
				dbTableEndpoints[db] = make(map[string][]string)
			}
			for _, table := range schema.Tables {
				dbTableEndpoints[db][table] = append(dbTableEndpoints[db][table], endpoint)
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
			macro := fmt.Sprintf("macro-expand entity_group [%s] as X { X.%s }", strings.Join(clusters, ", "), table)
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

func checkIfFunctionIsView(ctx context.Context, client *kusto.Client, database, functionName string) (bool, error) {
	q := kql.New(".show function ParamFunctionName schema as json | project FunctionKind = todynamic(Schema).FunctionKind")
	params := kql.NewParameters().AddString("ParamFunctionName", functionName)

	result, err := client.Mgmt(ctx, database, q, kusto.QueryParameters(params))
	if err != nil {
		return false, fmt.Errorf("failed to query function schema: %w", err)
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
			return false, fmt.Errorf("failed to retrieve function schema: %w", errFinal)
		}

		var fk FunctionKind
		if err := row.ToStruct(&fk); err != nil {
			return false, fmt.Errorf("failed to parse function kind: %w", err)
		}
		return fk.Kind == "ViewFunction", nil
	}

	return false, nil
}

// ensureEntityGroups creates/updates entity-groups for active databases and cleans up stale ones
// containing all active partition cluster endpoints for that database
func ensureEntityGroups(ctx context.Context, client *kusto.Client, dbSet map[string]struct{}, schemaByEndpoint map[string][]ADXClusterSchema) error {
	// Safety check: If no heartbeat data, skip entity-group operations to prevent mass deletion
	if len(schemaByEndpoint) == 0 {
		logger.Warnf("No heartbeat data received from partition clusters, skipping entity-group operations to prevent accidental cleanup")
		return nil
	}

	// Get list of databases to process
	databases := make([]string, 0, len(dbSet))
	for db := range dbSet {
		databases = append(databases, db)
	}

	// Process each database: create/update entity-groups and cleanup stale ones
	for _, database := range databases {
		// Step 1: Query existing entity-groups in this database to identify what needs cleanup
		existingEntityGroups := make(map[string]bool)
		q := kql.New(".show entity_groups")
		result, err := client.Mgmt(ctx, database, q)
		if err != nil {
			logger.Warnf("Failed to query existing entity-groups in database %s: %v", database, err)
			// Continue with creation, skip cleanup for this database
		} else {
			for {
				row, errInline, errFinal := result.NextRowOrError()
				if errFinal == io.EOF {
					break
				}
				if errInline != nil {
					continue
				}
				if errFinal != nil {
					logger.Warnf("Failed to retrieve entity-groups in database %s: %v", database, errFinal)
					break
				}

				var entityGroup struct {
					Name string `kusto:"Name"`
				}
				if err := row.ToStruct(&entityGroup); err != nil {
					logger.Warnf("Failed to parse entity-group in database %s: %v", database, err)
					continue
				}

				// Track entity-groups with _Partitions suffix for cleanup consideration
				if strings.HasSuffix(entityGroup.Name, PartitionsSuffix) {
					existingEntityGroups[entityGroup.Name] = true
				}
			}
			result.Stop()
		}

		// Step 2: Create/update entity-groups for databases that have heartbeat data
		if _, hasDatabase := dbSet[database]; hasDatabase {
			// Collect all cluster endpoints that have this database
			var entityReferences []string
			for endpoint, dbSchemas := range schemaByEndpoint {
				for _, schema := range dbSchemas {
					if schema.Database == database {
						entityRef := fmt.Sprintf("cluster('%s').database('%s')", endpoint, database)
						entityReferences = append(entityReferences, entityRef)
						break // Found the database in this endpoint, move to next endpoint
					}
				}
			}

			if len(entityReferences) > 0 {
				// Create entity-group name following pattern: {DatabaseName}_Partitions
				entityGroupName := fmt.Sprintf("%s%s", database, PartitionsSuffix)

				// Mark this entity-group as active (don't clean it up)
				delete(existingEntityGroups, entityGroupName)

				// Build entity references string for KQL command
				entityRefsStr := strings.Join(entityReferences, ", ")

				// Check if entity-group already exists
				exists, err := entityGroupExists(ctx, client, database, entityGroupName)
				if err != nil {
					logger.Errorf("Failed to check if entity-group %s exists in database %s: %v", entityGroupName, database, err)
					continue
				}

				// Validate entity-group name to prevent injection
				if !isValidEntityGroupName(entityGroupName) {
					logger.Errorf("Invalid entity-group name: %s", entityGroupName)
					continue
				}

				var stmt *kql.Builder
				if exists {
					// Update existing entity-group
					alterCmd := fmt.Sprintf(".alter entity_group %s (%s)", entityGroupName, entityRefsStr)
					stmt = kql.New("").AddUnsafe(alterCmd)
					logger.Infof("Updating existing entity-group %s with %d partition clusters", entityGroupName, len(entityReferences))
				} else {
					// Create new entity-group
					createCmd := fmt.Sprintf(".create entity_group %s (%s)", entityGroupName, entityRefsStr)
					stmt = kql.New("").AddUnsafe(createCmd)
					logger.Infof("Creating new entity-group %s with %d partition clusters", entityGroupName, len(entityReferences))
				}

				_, err = client.Mgmt(ctx, database, stmt)
				if err != nil {
					logger.Errorf("Failed to create/update entity-group %s in database %s: %v", entityGroupName, database, err)
					continue
				}

				logger.Infof("Successfully created/updated entity-group %s", entityGroupName)
			}
		}

		// Step 3: Clean up stale entity-groups (those not updated above)
		for staleEntityGroup := range existingEntityGroups {
			logger.Infof("Removing stale entity-group %s from database %s", staleEntityGroup, database)
			if !isValidEntityGroupName(staleEntityGroup) {
				logger.Warnf("Skipping invalid entity-group name: %s", staleEntityGroup)
				continue
			}
			dropCmd := fmt.Sprintf(".drop entity_group %s", staleEntityGroup)
			stmt := kql.New("").AddUnsafe(dropCmd)
			_, err := client.Mgmt(ctx, database, stmt)
			if err != nil {
				logger.Errorf("Failed to drop stale entity-group %s from database %s: %v", staleEntityGroup, database, err)
			} else {
				logger.Infof("Successfully removed stale entity-group %s", staleEntityGroup)
			}
		}
	}

	return nil
}

// entityGroupExists checks if an entity-group exists in the specified database
func entityGroupExists(ctx context.Context, client *kusto.Client, database, entityGroupName string) (bool, error) {
	q := kql.New(".show entity_groups | where Name == ParamEntityGroupName | count")
	params := kql.NewParameters().AddString("ParamEntityGroupName", entityGroupName)

	result, err := client.Mgmt(ctx, database, q, kusto.QueryParameters(params))
	if err != nil {
		return false, fmt.Errorf("failed to query entity-groups: %w", err)
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
			return false, fmt.Errorf("failed to retrieve entity-groups: %w", errFinal)
		}

		var count struct {
			Count int64 `kusto:"Count"`
		}
		if err := row.ToStruct(&count); err != nil {
			return false, fmt.Errorf("failed to parse entity-group count: %w", err)
		}
		return count.Count > 0, nil
	}

	return false, nil
}
