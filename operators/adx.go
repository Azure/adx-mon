package operator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/kusto/armkusto"
	kusto "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/kusto/armkusto"
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
)

func (r *AdxReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cluster adxmonv1.ADXCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if cluster.DeletionTimestamp != nil {
		// Note, at this time we are not going to delete any Azure resources.
		logger.Infof("Cluster %s is being deleted", cluster.Spec.ClusterName)
		return ctrl.Result{}, nil
	}

	condition := meta.FindStatusCondition(cluster.Status.Conditions, adxmonv1.ADXClusterConditionOwner)
	switch {
	case condition == nil:
		// First time reconciliation
		return r.CreateCluster(ctx, &cluster)

	case condition.ObservedGeneration != cluster.GetGeneration():
		// CRD updated
		return r.UpdateCluster(ctx, &cluster)

	case condition.Reason == ADXClusterCreatingReason:
		// Cluster is still being configured.
		return r.CreateCluster(ctx, &cluster)

	case condition.Reason == ADXClusterWaitingReason:
		// Check the status of the cluster
		return r.CheckStatus(ctx, &cluster)
	}

	return ctrl.Result{}, nil
}

func (r *AdxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&adxmonv1.ADXCluster{}).
		Complete(r)
}

func (r *AdxReconciler) CreateCluster(ctx context.Context, cluster *adxmonv1.ADXCluster) (ctrl.Result, error) {
	setClusterStatusCondition := func(reason, message string) error {
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
		if err := setClusterStatusCondition(ADXClusterWaitingReason, fmt.Sprintf("Waiting for ADX cluster %s", cluster.Name)); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Minute}, nil
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
		_ = setClusterStatusCondition(ADXClusterCreatingReason, fmt.Sprintf("Registering provider for subscription %s", cluster.Spec.Provision.SubscriptionId))
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
	}

	if err := ensureResourceGroup(ctx, cluster, cred); err != nil {
		return ctrl.Result{}, err
	}

	clusterReady, err := createOrUpdateKustoCluster(ctx, cluster, cred)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !clusterReady {
		// We must wait for the cluster to be in a ready state before we can continue configuration.
		_ = setClusterStatusCondition(ADXClusterCreatingReason, fmt.Sprintf("Provisioning ADX cluster %s", cluster.Spec.ClusterName))
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	dbCreated, err := ensureDatabases(ctx, cluster, cred)
	if err != nil {
		return ctrl.Result{}, err
	}
	if dbCreated {
		// Wait for databases to be created.
		_ = setClusterStatusCondition(ADXClusterCreatingReason, fmt.Sprintf("Provisioning ADX cluster %s databases", cluster.Spec.ClusterName))
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}
	_ = setClusterStatusCondition(ADXClusterWaitingReason, "Provisioning ADX clusters")
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
	clustersClient, err := kusto.NewClustersClient(cluster.Spec.Provision.SubscriptionId, cred, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create clusters client: %w", err)
	}
	available, err := clustersClient.CheckNameAvailability(
		ctx,
		cluster.Spec.Provision.Location,
		kusto.ClusterCheckNameRequest{
			Name: to.Ptr(cluster.Spec.ClusterName),
		},
		nil,
	)
	if err != nil {
		return false, fmt.Errorf("failed to check cluster name availability: %w", err)
	}
	if available.NameAvailable != nil && *available.NameAvailable {
		logger.Infof("Creating Kusto cluster %s...", cluster.Spec.ClusterName)

		var identity *kusto.Identity
		if cluster.Spec.Provision != nil && len(cluster.Spec.Provision.UserAssignedIdentities) != 0 {
			userAssignedIdentities := make(map[string]*kusto.ComponentsSgqdofSchemasIdentityPropertiesUserassignedidentitiesAdditionalproperties)
			for _, u := range cluster.Spec.Provision.UserAssignedIdentities {
				userAssignedIdentities[u] = &kusto.ComponentsSgqdofSchemasIdentityPropertiesUserassignedidentitiesAdditionalproperties{}
			}
			identity = &kusto.Identity{
				Type:                   to.Ptr(kusto.IdentityTypeUserAssigned),
				UserAssignedIdentities: userAssignedIdentities,
			}
		} else {
			identity = &kusto.Identity{
				Type: to.Ptr(kusto.IdentityTypeSystemAssigned),
			}
		}

		var autoScale *kusto.OptimizedAutoscale
		if cluster.Spec.Provision.AutoScale {
			autoScale = &kusto.OptimizedAutoscale{
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
			kusto.Cluster{
				Location: to.Ptr(cluster.Spec.Provision.Location),
				SKU: &kusto.AzureSKU{
					Name: toSku(cluster.Spec.Provision.SkuName),
					Tier: toTier(cluster.Spec.Provision.Tier),
				},
				Identity: identity,
				Properties: &kusto.ClusterProperties{
					EnableAutoStop:     to.Ptr(false),
					EngineType:         to.Ptr(kusto.EngineTypeV3),
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
			return false, fmt.Errorf("failed to get cluster status: %w", err)
		}
		if resp.Properties == nil || resp.Properties.State == nil || *resp.Properties.State != kusto.StateRunning {
			return false, nil // Not ready yet
		}
	}
	return true, nil // Cluster is ready
}

// ensureDatabases creates databases if they do not exist, returns true if any were created
func ensureDatabases(ctx context.Context, cluster *adxmonv1.ADXCluster, cred azcore.TokenCredential) (bool, error) {
	databasesClient, err := kusto.NewDatabasesClient(cluster.Spec.Provision.SubscriptionId, cred, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create databases client: %w", err)
	}
	var dbCreated bool
	for _, db := range cluster.Spec.Databases {
		available, err := databasesClient.CheckNameAvailability(
			ctx,
			cluster.Spec.Provision.ResourceGroup,
			cluster.Spec.ClusterName,
			kusto.CheckNameRequest{
				Name: to.Ptr(db.DatabaseName),
				Type: to.Ptr(kusto.TypeMicrosoftKustoClustersDatabases),
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

func (r *AdxReconciler) UpdateCluster(ctx context.Context, cluster *adxmonv1.ADXCluster) (ctrl.Result, error) {
	// To accurately detect and reconcile user-driven changes to the cluster configuration (such as Sku, Tier, or UserAssignedIdentities),
	// the operator stores a snapshot of the last-applied configuration. This allows the operator to distinguish between changes made
	// via the CRD (which should be reconciled) and any modifications made directly in Azure (which are intentionally ignored).
	// By comparing the current CRD spec to the stored applied state, we can determine exactly which fields the user has updated
	// and ensure only those changes are propagated to the managed cluster.
	if cluster.Spec.Provision == nil {
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

	clustersClient, err := kusto.NewClustersClient(cluster.Spec.Provision.SubscriptionId, cred, nil)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create clusters client: %w", err)
	}
	resp, err := clustersClient.Get(ctx, cluster.Spec.Provision.ResourceGroup, cluster.Spec.ClusterName, nil)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get cluster status: %w", err)
	}

	clusterUpdate, updated := diffSkus(resp, appliedProvisionState, cluster)
	if diffIdentities(resp, appliedProvisionState, cluster, &clusterUpdate) {
		updated = true
	}

	if !updated {
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
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create default credential: %w", err)
	}

	clustersClient, err := kusto.NewClustersClient(cluster.Spec.Provision.SubscriptionId, cred, nil)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create clusters client: %w", err)
	}
	resp, err := clustersClient.Get(ctx, cluster.Spec.Provision.ResourceGroup, cluster.Spec.ClusterName, nil)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get cluster status: %w", err)
	}
	if resp.Properties == nil || resp.Properties.State == nil {
		return ctrl.Result{RequeueAfter: time.Minute}, nil // Not ready yet
	}

	// If the cluster is running, we're done
	if *resp.Properties.State == kusto.StateRunning {
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
	if resp.Properties.ProvisioningState != nil && *resp.Properties.ProvisioningState == kusto.ProvisioningStateFailed {
		c := metav1.Condition{
			Type:               adxmonv1.ADXClusterConditionOwner,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: cluster.GetGeneration(),
			LastTransitionTime: metav1.Now(),
			Reason:             string(kusto.ProvisioningStateFailed),
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
	return ctrl.Result{RequeueAfter: time.Minute}, nil // Not ready yet
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
	clustersClient, err := kusto.NewClustersClient(subscriptionId, cred, nil)
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
	if cluster.Spec.Endpoint != "" {
		return nil // Skip clusters that already have endpoints
	}

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

	// Get best available SKU if not specified
	if cluster.Spec.Provision.SkuName == "" {
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

func diffSkus(resp kusto.ClustersClientGetResponse, appliedProvisionState *adxmonv1.AppliedProvisionState, cluster *adxmonv1.ADXCluster) (kusto.Cluster, bool) {
	clusterUpdate := kusto.Cluster{
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

func diffIdentities(resp kusto.ClustersClientGetResponse, appliedProvisionState *adxmonv1.AppliedProvisionState, cluster *adxmonv1.ADXCluster, clusterUpdate *kusto.Cluster) bool {
	var updated bool
	if resp.Identity != nil && resp.Identity.Type != nil && *resp.Identity.Type == kusto.IdentityTypeUserAssigned && appliedProvisionState.UserAssignedIdentities != nil {
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
					clusterUpdate.Identity.UserAssignedIdentities = make(map[string]*kusto.ComponentsSgqdofSchemasIdentityPropertiesUserassignedidentitiesAdditionalproperties)
				}
				if _, exists := resp.Identity.UserAssignedIdentities[id]; !exists {
					clusterUpdate.Identity.UserAssignedIdentities[id] = &kusto.ComponentsSgqdofSchemasIdentityPropertiesUserassignedidentitiesAdditionalproperties{}
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
