package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"text/template"
	"time"

	adxmonv1 "github.com/Azure/adx-mon/api/v1"
	"github.com/Azure/adx-mon/pkg/logger"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	kusto "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/kusto/armkusto"
	armresources "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// AdxClusterCreator is a function type that creates an ADX cluster.
type AdxClusterCreator = func(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (ctrl.Result, error)

// AdxClusterDeleter is a function type that checks if an ADX cluster is ready.
type AdxClusterReady = func(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (ctrl.Result, error)

func handleAdxEvent(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (ctrl.Result, error) {
	if clustersAreDone(operator) {
		c := metav1.Condition{
			Type:               adxmonv1.ADXClusterConditionOwner,
			Status:             metav1.ConditionTrue,
			Reason:             string(adxmonv1.OperatorServiceReasonInstalled),
			Message:            "All Kusto clusters are ready",
			LastTransitionTime: metav1.NewTime(time.Now()),
		}
		if meta.SetStatusCondition(&operator.Status.Conditions, c) {
			err := r.Status().Update(ctx, operator)
			return ctrl.Result{Requeue: true}, err
		}

		return ctrl.Result{}, nil
	}

	// TODO Ensure ASO Kusto CRD is created, check its status
	// or we might just bootstrap ADX ourselves.

	c := metav1.Condition{
		Type:               adxmonv1.ADXClusterConditionOwner,
		Status:             metav1.ConditionUnknown,
		Reason:             string(adxmonv1.OperatorServiceReasonInstalling),
		Message:            "Ensuring Kusto cluster and database via ASO",
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
	if meta.SetStatusCondition(&operator.Status.Conditions, c) {
		if err := r.Status().Update(ctx, operator); err != nil {
			logger.Errorf("Failed to update status: %v", err)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func clustersAreDone(operator *adxmonv1.Operator) bool {
	condition := meta.FindStatusCondition(operator.Status.Conditions, adxmonv1.ADXClusterConditionOwner)
	if condition != nil &&
		condition.Status == metav1.ConditionTrue &&
		condition.ObservedGeneration == operator.GetGeneration() {
		// If the condition is true, we can assume the cluster is done
		return true
	}

	// If a Kusto cluster already has an endpoint, we denote the cluster as "existing".
	var allExisting bool
	if operator.Spec.ADX != nil {
		for _, cluster := range operator.Spec.ADX.Clusters {
			if cluster.Endpoint == "" {
				return false
			}
			allExisting = true
		}
	}
	if allExisting {
		return true
	}

	// TODO: Check the ASO owned Kusto CRD status

	// If the cluster is not existing, we check if the owner condition is true.
	if condition == nil {
		return false
	}
	if condition.Status != metav1.ConditionTrue {
		return false
	}
	return true
}

// getIMDSMetadata queries Azure IMDS for metadata about the current environment
func getIMDSMetadata(ctx context.Context) (region, subscriptionId, resourceGroup, aksClusterName string, ok bool) {
	const imdsURL = "http://169.254.169.254/metadata/instance?api-version=2021-02-01"
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
func getBestAvailableSKU(ctx context.Context, subscriptionId string, region string, cred *azidentity.DefaultAzureCredential) (sku, tier string, err error) {
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

// ArmAdxCluster creates a Kusto cluster using an ARM template
func ArmAdxCluster(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (ctrl.Result, error) {
	if operator.Spec.ADX == nil {
		operator.Spec.ADX = &adxmonv1.ADXConfig{}
	}

	// Get IMDS metadata for defaults
	imdsRegion, imdsSub, imdsRG, imdsAksName, imdsOK := getIMDSMetadata(ctx)

	// Authenticate early since we need it for SKU selection
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get Azure credential: %w", err)
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
				return ctrl.Result{}, fmt.Errorf("failed to determine SKU: %w", err)
			}
			cluster.Provision.SKU = sku
			cluster.Provision.Tier = tier
			updated = true
		}

		// Persist any changes back to the API server
		if updated {
			if err := r.Update(ctx, operator); err != nil {
				logger.Errorf("Failed to update operator with cluster information: %v", err)
				return ctrl.Result{}, fmt.Errorf("failed to update operator spec: %w", err)
			}
		}

		// Load ARM template from manifestsFS
		tmplBytes, err := manifestsFS.ReadFile("manifests/kusto-cluster-arm.json")
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to read ARM template: %w", err)
		}
		tmpl, err := template.New("arm").Parse(string(tmplBytes))
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to parse ARM template: %w", err)
		}

		// Render template
		var buf bytes.Buffer
		err = tmpl.Execute(&buf, map[string]any{
			"Name":      cluster.Name,
			"Provision": cluster.Provision,
		})
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to render ARM template: %w", err)
		}

		// Unmarshal rendered template to map
		var templateObj map[string]any
		if err := json.Unmarshal(buf.Bytes(), &templateObj); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to unmarshal rendered ARM template: %w", err)
		}

		// Deploy ARM template
		deploymentsClient, err := armresources.NewDeploymentsClient(cluster.Provision.SubscriptionID, cred, nil)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to create deployments client: %w", err)
		}

		deploymentName := fmt.Sprintf("adxmon-%s-%d", cluster.Name, time.Now().Unix())
		deployment := armresources.Deployment{
			Properties: &armresources.DeploymentProperties{
				Mode:       to.Ptr(armresources.DeploymentModeIncremental),
				Template:   templateObj,
				Parameters: map[string]any{}, // parameters are inlined by template rendering
			},
		}

		// Start deployment
		_, err = deploymentsClient.BeginCreateOrUpdate(ctx, cluster.Provision.ResourceGroup, deploymentName, deployment, nil)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to start ARM deployment: %w", err)
		}
	}

	// Update status condition
	c := metav1.Condition{
		Type:               adxmonv1.ADXClusterConditionOwner,
		Status:             metav1.ConditionUnknown,
		Reason:             string(adxmonv1.OperatorServiceReasonInstalling),
		Message:            "Provisioning Kusto clusters",
		LastTransitionTime: metav1.NewTime(time.Now()),
	}
	meta.SetStatusCondition(&operator.Status.Conditions, c)
	if err := r.Status().Update(ctx, operator); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue to check deployment status
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// ArmAdxReady checks if the ADX cluster is ready and updates the operator with connection information
func ArmAdxReady(ctx context.Context, r *Reconciler, operator *adxmonv1.Operator) (ctrl.Result, error) {
	if operator.Spec.ADX == nil {
		return ctrl.Result{}, nil
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
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
		if *resp.Properties.State != "Running" {
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
			Message:            "Waiting for Kusto clusters to be ready",
			LastTransitionTime: metav1.NewTime(time.Now()),
		}
		meta.SetStatusCondition(&operator.Status.Conditions, c)
		if err := r.Status().Update(ctx, operator); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	// All clusters are ready, update status
	c := metav1.Condition{
		Type:               adxmonv1.ADXClusterConditionOwner,
		Status:             metav1.ConditionTrue,
		Reason:             string(adxmonv1.OperatorServiceReasonInstalled),
		Message:            "All Kusto clusters are ready",
		LastTransitionTime: metav1.NewTime(time.Now()),
		ObservedGeneration: operator.GetGeneration(),
	}
	meta.SetStatusCondition(&operator.Status.Conditions, c)
	if err := r.Status().Update(ctx, operator); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{Requeue: true}, nil
}
