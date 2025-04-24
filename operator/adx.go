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
	armmsi "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/msi/armmsi"
	armresources "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	corev1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// AdxClusterCreator is a function type that creates an ADX cluster.
type AdxClusterCreator = func(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (ctrl.Result, error)

// AdxClusterDeleter is a function type that checks if an ADX cluster is ready.
type AdxClusterReady = func(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (ctrl.Result, error)

func handleAdxEvent(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (ctrl.Result, error) {
	condition := meta.FindStatusCondition(operator.Status.Conditions, adxmonv1.ADXClusterConditionOwner)
	if condition == nil {
		condition = &metav1.Condition{
			Type:               adxmonv1.ADXClusterConditionOwner,
			Status:             metav1.ConditionUnknown,
			Reason:             string(adxmonv1.OperatorServiceReasonNotInstalled),
			Message:            "Provisioning ADX clusters",
			LastTransitionTime: metav1.NewTime(time.Now()),
			ObservedGeneration: operator.GetGeneration(),
		}
		if meta.SetStatusCondition(&operator.Status.Conditions, *condition) {
			if err := r.Status().Update(ctx, operator); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else if condition.ObservedGeneration != operator.GetGeneration() && condition.Reason == string(adxmonv1.OperatorServiceReasonInstalled) && condition.Status == metav1.ConditionTrue {
		// If the ADX cluster operation had previously completed successfully, but the config has since been updated, attempt to reconcile cluster state.
		condition.Status = metav1.ConditionUnknown
		condition.Reason = string(adxmonv1.OperatorServiceReasonDrifted)
		condition.ObservedGeneration = operator.GetGeneration()
		condition.LastTransitionTime = metav1.NewTime(time.Now())
		condition.Message = "Ensuring ADX cluster configuration"
		if meta.SetStatusCondition(&operator.Status.Conditions, *condition) {
			if err := r.Status().Update(ctx, operator); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	switch condition.Reason {
	case string(adxmonv1.OperatorServiceReasonNotInstalled):
		return r.AdxCtor(ctx, r, operator)

	case string(adxmonv1.OperatorServiceReasonInstalling):
		return r.AdxRdy(ctx, r, operator)

	case string(adxmonv1.OperatorServiceReasonDrifted):
		// Check the cluster CRD configuration against the current state
		return r.AdxUpdate(ctx, r, operator)

	case string(adxmonv1.OperatorServiceTerminalError):
		// Nothing to do, error is terminal
		return ctrl.Result{}, nil

	case string(adxmonv1.OperatorServiceReasonInstalled):
		if condition.Status == metav1.ConditionTrue {
			// Nothing to do, the Ready function can handle it from here
			return ctrl.Result{Requeue: true}, nil
		}
		// We're in an unknown state, so we need to reconcile the cluster
		return r.AdxUpdate(ctx, r, operator)

	default:
		// Ensure cluster topology
		return CreateAdxCluster(ctx, r, operator)
	}
}

// getIMDSMetadata queries Azure IMDS for metadata about the current environment
func getIMDSMetadata(ctx context.Context, imdsURL string) (region, subscriptionId, resourceGroup, aksClusterName string, ok bool) {
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

// lookupManagedIdentityPrincipalID looks up the principal ID (object ID) for a managed identity given its client ID
func lookupManagedIdentityPrincipalID(ctx context.Context, cred azcore.TokenCredential, subscriptionID, resourceGroup, clientID string) (string, error) {
	msiClient, err := armmsi.NewUserAssignedIdentitiesClient(subscriptionID, cred, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create managed identities client: %w", err)
	}

	// List managed identities in the resource group
	pager := msiClient.NewListByResourceGroupPager(resourceGroup, nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to list managed identities: %w", err)
		}
		for _, identity := range page.Value {
			if identity.Properties != nil && identity.Properties.ClientID != nil && *identity.Properties.ClientID == clientID {
				if identity.Properties.PrincipalID == nil {
					return "", fmt.Errorf("managed identity found but principal ID is nil")
				}
				return *identity.Properties.PrincipalID, nil
			}
		}
	}

	return "", fmt.Errorf("managed identity with client ID %s not found", clientID)
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

// CreateAdxCluster creates an ADX cluster and its databases if they don't exist. This function is idempotent.
func CreateAdxCluster(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (ctrl.Result, error) {
	if err := applyDefaults(ctx, r, operator); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to apply defaults: %w", err)
	}

	cred, err := getAzureCredential(ctx, r, operator)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get Azure credential: %w", err)
	}

	for i := range operator.Spec.ADX.Clusters {
		cluster := &operator.Spec.ADX.Clusters[i]

		if cluster.Endpoint != "" {
			continue // Skip clusters that already have endpoints
		}

		// Ensure the Kusto resource provider is registered
		registered, err := ensureAdxProvider(ctx, cred, cluster.Provision.SubscriptionID)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to ensure Kusto provider is registered: %w", err)
		}
		if !registered {
			logger.Infof("Kusto provider is not registered, requeuing...")
			c := &metav1.Condition{
				Type:               adxmonv1.ADXClusterConditionOwner,
				Status:             metav1.ConditionUnknown,
				Reason:             string(adxmonv1.OperatorServiceReasonNotInstalled),
				Message:            fmt.Sprintf("Registering provider for subscription %s", cluster.Provision.SubscriptionID),
				LastTransitionTime: metav1.NewTime(time.Now()),
				ObservedGeneration: operator.GetGeneration(),
			}
			if meta.SetStatusCondition(&operator.Status.Conditions, *c) {
				if err := r.Status().Update(ctx, operator); err != nil {
					return ctrl.Result{}, err
				}
			}
			return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
		}

		// Check if resource group exists and create if needed
		rgClient, err := armresources.NewResourceGroupsClient(cluster.Provision.SubscriptionID, cred, nil)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create resource groups client: %w", err)
		}

		// If the resource group doesn't exist, create it
		exists, err := rgClient.CheckExistence(ctx, cluster.Provision.ResourceGroup, nil)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to check resource group existence: %w", err)
		}
		if !exists.Success {
			logger.Infof("Resource group %s not found, creating...", cluster.Provision.ResourceGroup)
			_, err = rgClient.CreateOrUpdate(ctx,
				cluster.Provision.ResourceGroup,
				armresources.ResourceGroup{
					Location: to.Ptr(cluster.Provision.Region),
				},
				nil)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to create resource group: %w", err)
			}
		}

		// Create Kusto clusters client
		clustersClient, err := kusto.NewClustersClient(cluster.Provision.SubscriptionID, cred, nil)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create clusters client: %w", err)
		}

		// Create the cluster if it doesn't exist
		available, err := clustersClient.CheckNameAvailability(
			ctx,
			cluster.Provision.Region,
			kusto.ClusterCheckNameRequest{
				Name: to.Ptr(cluster.Name),
			},
			nil,
		)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to check cluster name availability: %w", err)
		}

		if available.NameAvailable != nil && *available.NameAvailable {
			logger.Infof("Creating Kusto cluster %s...", cluster.Name)
			_, err := clustersClient.BeginCreateOrUpdate(
				ctx,
				cluster.Provision.ResourceGroup,
				cluster.Name,
				kusto.Cluster{
					Location: to.Ptr(cluster.Provision.Region),
					SKU: &kusto.AzureSKU{
						Name: toSku(cluster.Provision.SKU),
						Tier: toTier(cluster.Provision.Tier),
					},
					Identity: &kusto.Identity{
						Type: to.Ptr(kusto.IdentityTypeSystemAssigned),
					},
					// TODO Support customization of these properties via our CRD
					Properties: &kusto.ClusterProperties{
						EnableAutoStop: to.Ptr(false),
						EngineType:     to.Ptr(kusto.EngineTypeV3),
						OptimizedAutoscale: &kusto.OptimizedAutoscale{
							IsEnabled: to.Ptr(true),
							Maximum:   to.Ptr(int32(10)),
							Minimum:   to.Ptr(int32(2)),
							Version:   to.Ptr(int32(1)),
						},
					},
					// TODO support Zones
				},
				nil,
			)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to create Kusto cluster: %w", err)
			}
			c := metav1.Condition{
				Type:               adxmonv1.ADXClusterConditionOwner,
				Status:             metav1.ConditionUnknown,
				Reason:             string(adxmonv1.OperatorServiceReasonNotInstalled),
				Message:            fmt.Sprintf("Provisioning ADX cluster %s", cluster.Name),
				LastTransitionTime: metav1.NewTime(time.Now()),
				ObservedGeneration: operator.GetGeneration(),
			}
			if meta.SetStatusCondition(&operator.Status.Conditions, c) {
				if err := r.Status().Update(ctx, operator); err != nil {
					return ctrl.Result{}, err
				}
			}
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		} else {
			// Must wait for the cluster to be ready before we can create the databases
			resp, err := clustersClient.Get(ctx, cluster.Provision.ResourceGroup, cluster.Name, nil)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to get cluster status: %w", err)
			}
			if resp.Properties == nil || resp.Properties.State == nil || *resp.Properties.State != kusto.StateRunning {
				return ctrl.Result{RequeueAfter: time.Minute}, nil
			}
		}

		// Create databases
		databasesClient, err := kusto.NewDatabasesClient(cluster.Provision.SubscriptionID, cred, nil)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create databases client: %w", err)
		}

		var dbCreated bool
		for _, db := range cluster.Databases {
			// Check if database exists
			available, err := databasesClient.CheckNameAvailability(
				ctx,
				cluster.Provision.ResourceGroup,
				cluster.Name,
				kusto.CheckNameRequest{
					Name: to.Ptr(db.Name),
					Type: to.Ptr(kusto.TypeMicrosoftKustoClustersDatabases),
				},
				nil,
			)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to check database name availability: %w", err)
			}

			if available.NameAvailable != nil && *available.NameAvailable {
				// Create the database
				logger.Infof("Creating database %s in cluster %s...", db.Name, cluster.Name)
				_, err = databasesClient.BeginCreateOrUpdate(
					ctx,
					cluster.Provision.ResourceGroup,
					cluster.Name,
					db.Name,
					toDatabase(
						cluster.Provision.SubscriptionID,
						cluster.Name,
						cluster.Provision.ResourceGroup,
						cluster.Provision.Region,
						db.Name,
					),
					nil,
				)
				if err != nil {
					return ctrl.Result{}, fmt.Errorf("failed to create database: %w", err)
				}
				dbCreated = true
			}
		}

		if dbCreated {
			c := metav1.Condition{
				Type:               adxmonv1.ADXClusterConditionOwner,
				Status:             metav1.ConditionUnknown,
				Reason:             string(adxmonv1.OperatorServiceReasonNotInstalled),
				Message:            fmt.Sprintf("Provisioning ADX cluster %s databases", cluster.Name),
				LastTransitionTime: metav1.NewTime(time.Now()),
				ObservedGeneration: operator.GetGeneration(),
			}
			if meta.SetStatusCondition(&operator.Status.Conditions, c) {
				if err := r.Status().Update(ctx, operator); err != nil {
					return ctrl.Result{}, err
				}
			}
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
	}

	c := metav1.Condition{
		Type:               adxmonv1.ADXClusterConditionOwner,
		Status:             metav1.ConditionUnknown,
		Reason:             string(adxmonv1.OperatorServiceReasonInstalling),
		Message:            "Provisioning ADX clusters",
		LastTransitionTime: metav1.NewTime(time.Now()),
		ObservedGeneration: operator.GetGeneration(),
	}
	if meta.SetStatusCondition(&operator.Status.Conditions, c) {
		if err := r.Status().Update(ctx, operator); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func EnsureAdxClusterConfiguration(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (ctrl.Result, error) {
	cred, err := getAzureCredential(ctx, r, operator)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get Azure credential: %w", err)
	}

	// TODO drift requires further consideration. For example, if our CRD contains a database
	// name that doesn't currently exist, it could mean that a new database was added, or it
	// might mean the database name has changed. If the database name has changed, do we want
	// to delete the previous one? How would we even identify such a thing? This gets into
	// the territory of state files, which we want to avoid. Therefore, the current implementation
	// takes the explicit stance of upserting resources, but not reaping them.

	for i := range operator.Spec.ADX.Clusters {
		cluster := &operator.Spec.ADX.Clusters[i]

		// Create Kusto clusters client
		clustersClient, err := kusto.NewClustersClient(cluster.Provision.SubscriptionID, cred, nil)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create clusters client: %w", err)
		}

		// Get current cluster configuration
		resp, err := clustersClient.Get(ctx, cluster.Provision.ResourceGroup, cluster.Name, nil)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to get cluster configuration: %w", err)
		}

		needsUpdate := false

		// Check SKU configuration
		if resp.SKU != nil && (string(*resp.SKU.Name) != cluster.Provision.SKU || string(*resp.SKU.Tier) != cluster.Provision.Tier) {
			logger.Infof("Cluster %s SKU mismatch. Current: %s/%s, Desired: %s/%s",
				cluster.Name, *resp.SKU.Name, *resp.SKU.Tier, cluster.Provision.SKU, cluster.Provision.Tier)
			needsUpdate = true
		}

		// Check identity type
		if resp.Identity == nil || *resp.Identity.Type != kusto.IdentityTypeSystemAssigned {
			logger.Infof("Cluster %s identity type mismatch", cluster.Name)
			needsUpdate = true
		}

		if needsUpdate {
			logger.Infof("Updating Kusto cluster %s configuration...", cluster.Name)
			_, err := clustersClient.BeginCreateOrUpdate(
				ctx,
				cluster.Provision.ResourceGroup,
				cluster.Name,
				kusto.Cluster{
					Location: to.Ptr(cluster.Provision.Region),
					SKU: &kusto.AzureSKU{
						Name: toSku(cluster.Provision.SKU),
						Tier: toTier(cluster.Provision.Tier),
					},
					Identity: &kusto.Identity{
						Type: to.Ptr(kusto.IdentityTypeSystemAssigned),
					},
				},
				nil,
			)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to update Kusto cluster: %w", err)
			}

			c := metav1.Condition{
				Type:               adxmonv1.ADXClusterConditionOwner,
				Status:             metav1.ConditionUnknown,
				Reason:             string(adxmonv1.OperatorServiceReasonDrifted),
				Message:            fmt.Sprintf("Updating ADX cluster %s configuration", cluster.Name),
				LastTransitionTime: metav1.NewTime(time.Now()),
				ObservedGeneration: operator.GetGeneration(),
			}
			if meta.SetStatusCondition(&operator.Status.Conditions, c) {
				if err := r.Status().Update(ctx, operator); err != nil {
					return ctrl.Result{}, err
				}
			}
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}

		// Check databases
		databasesClient, err := kusto.NewDatabasesClient(cluster.Provision.SubscriptionID, cred, nil)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create databases client: %w", err)
		}

		// List existing databases
		pager := databasesClient.NewListByClusterPager(cluster.Provision.ResourceGroup, cluster.Name, nil)
		existingDBs := make(map[string]struct{})
		for pager.More() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to list databases: %w", err)
			}
			for _, db := range page.Value {
				existingDBs[*db.GetDatabase().Name] = struct{}{}
			}
		}

		// Create or update databases as needed
		for _, db := range cluster.Databases {
			if _, exists := existingDBs[db.Name]; !exists {
				logger.Infof("Creating missing database %s in cluster %s...", db.Name, cluster.Name)
				_, err = databasesClient.BeginCreateOrUpdate(
					ctx,
					cluster.Provision.ResourceGroup,
					cluster.Name,
					db.Name,
					toDatabase(
						cluster.Provision.SubscriptionID,
						cluster.Name,
						cluster.Provision.ResourceGroup,
						cluster.Provision.Region,
						db.Name,
					),
					nil,
				)
				if err != nil {
					return ctrl.Result{}, fmt.Errorf("failed to create database: %w", err)
				}

				c := metav1.Condition{
					Type:               adxmonv1.ADXClusterConditionOwner,
					Status:             metav1.ConditionUnknown,
					Reason:             string(adxmonv1.OperatorServiceReasonDrifted),
					Message:            fmt.Sprintf("Creating database %s in ADX cluster %s", db.Name, cluster.Name),
					LastTransitionTime: metav1.NewTime(time.Now()),
					ObservedGeneration: operator.GetGeneration(),
				}
				if meta.SetStatusCondition(&operator.Status.Conditions, c) {
					if err := r.Status().Update(ctx, operator); err != nil {
						return ctrl.Result{}, err
					}
				}
				return ctrl.Result{RequeueAfter: time.Minute}, nil
			}
		}
	}

	// All configurations are in sync, move to installation check phase
	c := metav1.Condition{
		Type:               adxmonv1.ADXClusterConditionOwner,
		Status:             metav1.ConditionUnknown,
		Reason:             string(adxmonv1.OperatorServiceReasonInstalling),
		Message:            "Checking ADX cluster installation status",
		LastTransitionTime: metav1.NewTime(time.Now()),
		ObservedGeneration: operator.GetGeneration(),
	}
	if meta.SetStatusCondition(&operator.Status.Conditions, c) {
		if err := r.Status().Update(ctx, operator); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{Requeue: true}, nil
}

// ArmAdxReady checks if the ADX cluster is ready and updates the operator with connection information
func ArmAdxReady(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (ctrl.Result, error) {
	if operator.Spec.ADX == nil {
		return ctrl.Result{}, nil
	}

	cred, err := getAzureCredential(ctx, r, operator)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get Azure credential: %w", err)
	}

	allReady := true
	updated := false

	for i := range operator.Spec.ADX.Clusters {
		cluster := &operator.Spec.ADX.Clusters[i]
		if cluster.Endpoint != "" {
			continue // Skip clusters that already have endpoints
		}

		if cluster.Provision == nil {
			allReady = false
			continue
		}

		// Create Kusto clusters client
		clustersClient, err := kusto.NewClustersClient(cluster.Provision.SubscriptionID, cred, nil)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create clusters client: %w", err)
		}

		// Get cluster status
		resp, err := clustersClient.Get(ctx, cluster.Provision.ResourceGroup, cluster.Name, nil)
		if err != nil {
			logger.Errorf("Failed to get cluster status: %v", err)
			allReady = false
			continue
		}

		if resp.Properties == nil || resp.Properties.State == nil {
			allReady = false
			continue
		}

		// Check if cluster is running
		if *resp.Properties.State != kusto.StateRunning {
			logger.Infof("Cluster %s is in state %s, waiting...", cluster.Name, *resp.Properties.State)
			allReady = false
			continue
		}

		// Get the cluster URI
		if resp.Properties.URI == nil {
			allReady = false
			continue
		}

		// Update the endpoint in the spec
		cluster.Endpoint = *resp.Properties.URI
		updated = true
		logger.Infof("Cluster %s is ready with endpoint %s", cluster.Name, cluster.Endpoint)
	}

	if updated {
		if err := r.Update(ctx, operator); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update operator with endpoints: %w", err)
		}
	}

	if !allReady {
		// Update status to indicate still waiting
		c := metav1.Condition{
			Type:               adxmonv1.ADXClusterConditionOwner,
			Status:             metav1.ConditionUnknown,
			Reason:             string(adxmonv1.OperatorServiceReasonInstalling),
			Message:            "Waiting for ADX clusters to be ready",
			LastTransitionTime: metav1.NewTime(time.Now()),
			ObservedGeneration: operator.GetGeneration(),
		}
		if meta.SetStatusCondition(&operator.Status.Conditions, c) {
			if err := r.Status().Update(ctx, operator); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	// All clusters are ready, update status
	c := metav1.Condition{
		Type:               adxmonv1.ADXClusterConditionOwner,
		Status:             metav1.ConditionTrue,
		Reason:             string(adxmonv1.OperatorServiceReasonInstalled),
		Message:            "ADX clusters are ready",
		LastTransitionTime: metav1.NewTime(time.Now()),
		ObservedGeneration: operator.GetGeneration(),
	}
	if meta.SetStatusCondition(&operator.Status.Conditions, c) {
		if err := r.Status().Update(ctx, operator); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{Requeue: true}, nil
}

func applyDefaults(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) error {
	if operator.Spec.ADX == nil {
		operator.Spec.ADX = &adxmonv1.ADXConfig{}
	}

	// Get IMDS metadata for defaults
	imdsRegion, imdsSub, imdsRG, imdsAksName, imdsOK := getIMDSMetadata(ctx, "")

	// Authenticate early since we need it for SKU selection
	cred, err := getAzureCredential(ctx, r, operator)
	if err != nil {
		return fmt.Errorf("failed to get Azure credential: %w", err)
	}

	updated := false
	for i := range operator.Spec.ADX.Clusters {
		cluster := &operator.Spec.ADX.Clusters[i] // Get pointer to avoid copy
		if cluster.Endpoint != "" {
			continue // Skip clusters that already have endpoints
		}

		// Initialize provision and status if needed
		if cluster.Provision == nil {
			logger.Infof("Zero config specified for cluster, discovering defaults")
			cluster.Provision = &adxmonv1.ADXClusterProvisionSpec{}
			updated = true
		}

		// Fill in defaults from IMDS when available
		if cluster.Provision.Region == "" && imdsOK {
			logger.Infof("Setting region to %s for cluster", imdsRegion)
			cluster.Provision.Region = imdsRegion
			updated = true
		}

		if cluster.Provision.SubscriptionID == "" && imdsOK {
			logger.Infof("Setting subscription ID to %s for cluster", imdsSub)
			cluster.Provision.SubscriptionID = imdsSub
			updated = true
		}

		if cluster.Provision.ResourceGroup == "" && imdsOK {
			logger.Infof("Setting resource group to %s for cluster", imdsRG)
			cluster.Provision.ResourceGroup = imdsRG
			updated = true
		}

		// Generate cluster name if not specified
		if cluster.Name == "" && imdsOK {
			cluster.Name = fmt.Sprintf("%s.%s", imdsAksName, imdsRegion)
			updated = true
			logger.Infof("Setting cluster name to %ss", cluster.Name)
		}

		// Get best available SKU if not specified
		if cluster.Provision.SKU == "" {
			sku, tier, err := getBestAvailableSKU(ctx, cluster.Provision.SubscriptionID, cluster.Provision.Region, cred)
			if err != nil {
				return fmt.Errorf("failed to determine SKU: %w", err)
			}
			cluster.Provision.SKU = sku
			cluster.Provision.Tier = tier
			updated = true
		}

		// Look up principal ID if managed identity client ID is specified
		if cluster.Provision.ManagedIdentityClientID != "" && cluster.Provision.ManagedIdentityPrincipalID == "" {
			principalID, err := lookupManagedIdentityPrincipalID(ctx, cred, cluster.Provision.SubscriptionID, cluster.Provision.ResourceGroup, cluster.Provision.ManagedIdentityClientID)
			if err != nil {
				return fmt.Errorf("failed to look up managed identity principal ID: %w", err)
			}
			cluster.Provision.ManagedIdentityPrincipalID = principalID
			updated = true
		}

		if len(cluster.Databases) == 0 {
			// Default to two databases if none specified
			cluster.Databases = []adxmonv1.ADXDatabaseSpec{
				{
					Name:          "Metrics",
					TelemetryType: adxmonv1.DatabaseTelemetryMetrics,
				},
				{
					Name:          "Logs",
					TelemetryType: adxmonv1.DatabaseTelemetryLogs,
				},
			}
			updated = true
		}

		// Persist any changes back to the API server
		if updated {
			if err := r.Update(ctx, operator); err != nil {
				logger.Errorf("Failed to update operator with cluster information: %v", err)
				return fmt.Errorf("failed to update operator spec: %w", err)
			}
		}
	}

	return nil
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

func getAzureCredential(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (azcore.TokenCredential, error) {
	if operator.Spec.AzureAuth == nil {
		// Use default credentials
		cred, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create default credential: %w", err)
		}
		return cred, nil
	}

	switch operator.Spec.AzureAuth.Type {
	case adxmonv1.AzureAuthTypeDefault:
		cred, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create default credential: %w", err)
		}
		return cred, nil

	case adxmonv1.AzureAuthTypeManagedIdentity:
		var opts *azidentity.ManagedIdentityCredentialOptions
		if operator.Spec.AzureAuth.ManagedIdentityClientID != "" {
			opts = &azidentity.ManagedIdentityCredentialOptions{
				ID: azidentity.ClientID(operator.Spec.AzureAuth.ManagedIdentityClientID),
			}
		}
		cred, err := azidentity.NewManagedIdentityCredential(opts)
		if err != nil {
			return nil, fmt.Errorf("failed to create managed identity credential: %w", err)
		}
		return cred, nil

	case adxmonv1.AzureAuthTypeServicePrincipal:
		if operator.Spec.AzureAuth.ServicePrincipal == nil {
			return nil, fmt.Errorf("service principal auth configured but no credentials provided")
		}

		// Get client ID from secret
		var clientIDSecret corev1.Secret
		if err := r.Get(ctx, types.NamespacedName{
			Name:      operator.Spec.AzureAuth.ServicePrincipal.ClientID.Name,
			Namespace: operator.Namespace,
		}, &clientIDSecret); err != nil {
			return nil, fmt.Errorf("failed to get client ID secret: %w", err)
		}
		clientID := string(clientIDSecret.Data[operator.Spec.AzureAuth.ServicePrincipal.ClientID.Key])

		// Get client secret from secret
		var clientSecretSecret corev1.Secret
		if err := r.Get(ctx, types.NamespacedName{
			Name:      operator.Spec.AzureAuth.ServicePrincipal.ClientSecret.Name,
			Namespace: operator.Namespace,
		}, &clientSecretSecret); err != nil {
			return nil, fmt.Errorf("failed to get client secret: %w", err)
		}
		clientSecret := string(clientSecretSecret.Data[operator.Spec.AzureAuth.ServicePrincipal.ClientSecret.Key])

		cred, err := azidentity.NewClientSecretCredential(
			operator.Spec.AzureAuth.ServicePrincipal.TenantID,
			clientID,
			clientSecret,
			nil,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create service principal credential: %w", err)
		}
		return cred, nil

	default:
		return nil, fmt.Errorf("unknown auth type: %s", operator.Spec.AzureAuth.Type)
	}
}
