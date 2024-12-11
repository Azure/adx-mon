#!/bin/bash
set -euo pipefail
SCRIPT_DIR="$(dirname "${BASH_SOURCE[0]}")"


if ! command -v az &> /dev/null
then
    echo "The 'az' command could not be found. Please install Azure CLI before continuing."
    exit
fi

if ! az account show &> /dev/null; then
    echo "You are not logged in to Azure CLI. Please log in."
    az login
fi

TOKEN_EXPIRY=$(az account get-access-token --query expires_on -o tsv)
CURRENT_DATE=$(date -u +%s)

if [[ "$CURRENT_DATE" > "$TOKEN_EXPIRY" ]]; then
    echo "Your Azure CLI token has expired. Please log in again."
    az login
fi

for EXT in resource-graph kusto; do
    if ! az extension show --name $EXT &> /dev/null; then
        read -p "The '$EXT' extension is not installed. Do you want to install it now? (y/n) " INSTALL_EXT
        if [[ "$INSTALL_EXT" == "y" ]]; then
            az extension add --name "$EXT"
        else
            echo "The '$EXT' extension is required. Exiting."
            exit 1
        fi
    fi
done

# Ask for the name of the aks cluster and read it as input.  With that name, run a graph query to find
read -p "Please enter the name of the AKS cluster where ADX-Mon components should be deployed: " CLUSTER
while [[ -z "${CLUSTER// }" ]]; do
    echo "Cluster cannot be empty. Please enter the name of the AKS cluster:"
    read CLUSTER
done

# Run a graph query to find the cluster's resource group and subscription id
CLUSTER_INFO=$(az graph query -q "Resources | where type =~ 'Microsoft.ContainerService/managedClusters' and name =~ '$CLUSTER' | project resourceGroup, subscriptionId, location")
if [[ $(echo $CLUSTER_INFO | jq '.data | length') -eq 0 ]]; then
    echo "No AKS cluster could be found for the cluster name '$CLUSTER'. Exiting."
    exit 1
fi

RESOURCE_GROUP=$(echo $CLUSTER_INFO | jq -r '.data[0].resourceGroup')
SUBSCRIPTION_ID=$(echo $CLUSTER_INFO | jq -r '.data[0].subscriptionId')
REGION=$(echo $CLUSTER_INFO | jq -r '.data[0].location')

# Find the managed identity client ID attached to the AKS node pools
NODE_POOL_IDENTITY=$(az aks show --resource-group $RESOURCE_GROUP --name $CLUSTER --query identityProfile.kubeletidentity.clientId -o json | jq . -r)

echo
printf "Found AKS cluster info:\n"
printf "  AKS Cluster Name: \e[32m$CLUSTER\e[0m\n"
printf "  Resource Group: \e[32m$RESOURCE_GROUP\e[0m\n"
printf "  Subscription ID:\e[32m $SUBSCRIPTION_ID\e[0m\n"
printf "  Region: \e[32m$REGION\e[0m\n"
printf "  Managed Identity Client ID: \e[32m$NODE_POOL_IDENTITY\e[0m\n"
echo
read -p "Is this information correct? (y/n) " CONFIRM
if [[ "$CONFIRM" != "y" ]]; then
    echo "Exiting as the information is not correct."
    exit 1
fi

az aks get-credentials --subscription $SUBSCRIPTION_ID --resource-group $RESOURCE_GROUP --name $CLUSTER

echo
read -p "Please enter the Azure Data Explorer cluster name where ADX-Mon will store telemetry: " CLUSTER_NAME
while [[ -z "${CLUSTER_NAME// }" ]]; do
    echo "ADX cluster name cannot be empty. Please enter the database name:"
    read CLUSTER_NAME
done

CLUSTER_INFO=$(az graph query -q "Resources | where type =~ 'Microsoft.Kusto/clusters' and name =~ '$CLUSTER_NAME' | project name, resourceGroup, subscriptionId, location, properties.uri")
CLUSTER_COUNT=$(echo $CLUSTER_INFO | jq '.data | length')
KUSTO_REGION=$REGION

if [[ $CLUSTER_COUNT -eq 0 ]]; then
    echo "No Kusto cluster could be found for the database name '$CLUSTER_NAME'. Creating cluster."
    
    # Process available SKUs in the region, sorted by recommended
    az extension add --name kusto
    recommended_skus=("Standard_L8as_v3" "Standard_L16as_v3" "Standard_L32as_v3")
    skus=$(az kusto cluster list-sku --subscription "$SUBSCRIPTION_ID" --query "[?contains(locations, '$KUSTO_REGION') && tier == 'Standard'].name" --output tsv)
    available_rec=()
    available_nonrec=()
    while read -r word; do
        if [[ " ${recommended_skus[@]} " =~ " $word " ]]; then
            available_rec+=("$word (recommended)")
        else
            available_nonrec+=("$word")
        fi
    done <<< "$skus"
    available_skus=("${available_rec[@]}" "${available_nonrec[@]}")

    # Prompt user for SKU selection
    PS3="Select a SKU value from the listed options: "
    select choice in "${available_skus[@]}"; do
        if [[ -n $choice ]]; then
            CLUSTER_SKU="${choice// (recommended)/}"
            echo "SKU selected: $CLUSTER_SKU"
            break
        else
            echo "Invalid selection. Please choose a valid option."
        fi
    done

    echo "Creating Kusto cluster with database name '$CLUSTER_NAME' in resource group '$RESOURCE_GROUP' with SKU '$CLUSTER_SKU'"
    az kusto cluster create --name "$CLUSTER_NAME" --resource-group "$RESOURCE_GROUP" --sku name="$CLUSTER_SKU" tier="Standard"
    ADX_FQDN=$(az kusto cluster show --resource-group "$RESOURCE_GROUP" --name "$CLUSTER_NAME" --query "uri" -o tsv)
else
    CLUSTER_NAME=$(echo $CLUSTER_INFO | jq -r '.data[0].name')
    SUBSCRIPTION_ID=$(echo $CLUSTER_INFO | jq -r '.data[0].subscriptionId')
    RESOURCE_GROUP=$(echo $CLUSTER_INFO | jq -r '.data[0].resourceGroup')
    KUSTO_REGION=$(echo $CLUSTER_INFO | jq -r '.data[0].location')
    ADX_FQDN=$(echo $CLUSTER_INFO | jq -r '.data[0].properties_uri')
fi
echo
printf "Found ADX cluster info:\n"
printf "  Cluster Name: \e[32m$CLUSTER_NAME\e[0m\n"
printf "  Subscription ID: \e[32m$SUBSCRIPTION_ID\e[0m\n"
printf "  Resource Group: \e[32m$RESOURCE_GROUP\e[0m\n"
printf "  ADX FQDN: \e[32m$ADX_FQDN\e[0m\n"
printf "  Region: \e[32m$KUSTO_REGION\e[0m\n"
echo
read -p "Is this the correct ADX cluster info? (y/n) " CONFIRM
if [[ "$CONFIRM" != "y" ]]; then
    echo "Exiting as the ADX cluster info is not correct."
    exit 1
fi

for DATABASE_NAME in Metrics Logs; do
    # Check if the $DATABASE_NAME database exists
    DATABASE_EXISTS=$(az kusto database show --cluster-name $CLUSTER_NAME --resource-group $RESOURCE_GROUP --database-name $DATABASE_NAME --query "name" -o tsv 2>/dev/null || echo "")
    if [[ -z "$DATABASE_EXISTS" ]]; then
        echo "The $DATABASE_NAME database does not exist. Creating it now."
        az kusto database create --cluster-name $CLUSTER_NAME --resource-group $RESOURCE_GROUP --database-name $DATABASE_NAME --read-write-database  soft-delete-period=P30D hot-cache-period=P7D location=$KUSTO_REGION
    else
        echo "The $DATABASE_NAME database already exists."
    fi

    # Check if the NODE_POOL_IDENTITY is an admin on the $DATABASE_NAME database
    ADMIN_CHECK=$(az kusto database list-principal --cluster-name $CLUSTER_NAME --resource-group $RESOURCE_GROUP --database-name $DATABASE_NAME --query "[?type=='App' && appId=='$NODE_POOL_IDENTITY' && role=='Admin']" -o tsv)
    if [[ -z "$ADMIN_CHECK" ]]; then
        echo "The Managed Identity Client ID is not configured to use database $DATABASE_NAME. Adding it as an admin."
        az kusto database add-principal --cluster-name $CLUSTER_NAME --resource-group $RESOURCE_GROUP --database-name $DATABASE_NAME --value role=Admin name=ADXMon type=app app-id=$NODE_POOL_IDENTITY
    else
        echo "The Managed Identity Client ID is already configured to use database $DATABASE_NAME."
    fi
done

export CLUSTER=$CLUSTER
export REGION=$REGION
export CLIENT_ID=$NODE_POOL_IDENTITY
export ADX_URL=$ADX_FQDN
envsubst < $SCRIPT_DIR/ingestor.yaml | kubectl apply -f -
envsubst < $SCRIPT_DIR/collector.yaml | kubectl apply -f -
kubectl apply -f $SCRIPT_DIR/ksm.yaml

# Restart all workloads for schema changes to be reflected if this is an update
kubectl rollout restart sts ingestor -n adx-mon
kubectl rollout restart ds collector -n adx-mon
kubectl rollout restart deploy collector-singleton -n adx-mon

GRAFANA_ENDPOINT=""
echo
read -p "Do you want to setup an Azure Managed Grafana instance to visualize the AKS telemetry? (y/n) " CONFIRM
if [[ "$CONFIRM" == "y" ]]; then
    if ! az extension show --name amg &> /dev/null; then
        read -p "The 'amg' extension is not installed. Do you want to install it now? (y/n) " INSTALL_EXT
        if [[ "$INSTALL_EXT" == "y" ]]; then
            az extension add --name "amg"
        else
            echo "The 'amg' extension is required to setup grafana. Exiting."
            exit 1
        fi
    fi

    read -p "Please enter name of Azure Managed Grafana instance (new or existing): " GRAFANA
    while [[ -z "${GRAFANA// }" ]]; do
        echo "Instance name cannot be empty. Please enter the name:"
        read GRAFANA
    done

    GRAFANA_INFO=$(az graph query -q "Resources | where type =~ 'Microsoft.Dashboard/grafana' and name =~ '$GRAFANA' | project name, resourceGroup, subscriptionId, location, properties.endpoint")
    GRAFANA_COUNT=$(echo $GRAFANA_INFO | jq '.data | length')
    if [[ GRAFANA_COUNT -eq 0 ]]; then
        # Create Azure Managed Grafana
        echo "The $GRAFANA instance does not exist. Creating it in $RESOURCE_GROUP resource group."
        az grafana create --name "$GRAFANA" --resource-group "$RESOURCE_GROUP"
        GRAFANA_INFO=$(az grafana show --name "$GRAFANA" --resource-group "$RESOURCE_GROUP" -o json)
        GRAFANA_RG=$RESOURCE_GROUP
        GRAFANA_ENDPOINT=$(echo $GRAFANA_INFO | jq -r '.properties.endpoint')
    else
        GRAFANA_SUB=$(echo $GRAFANA_INFO | jq -r '.data[0].subscriptionId')
        GRAFANA_RG=$(echo $GRAFANA_INFO | jq -r '.data[0].resourceGroup')
        GRAFANA_ENDPOINT=$(echo $GRAFANA_INFO | jq -r '.data[0].properties_endpoint')
        GRAFANA_REGION=$(echo $GRAFANA_INFO | jq -r '.data[0].location')

        echo
        printf "Found Grafana instance:\n"
        printf "  Name: \e[32m$GRAFANA\e[0m\n"
        printf "  Subscription ID: \e[32m$GRAFANA_SUB\e[0m\n"
        printf "  Resource Group: \e[32m$GRAFANA_RG\e[0m\n"
        printf "  Endpoint: \e[32m$GRAFANA_ENDPOINT\e[0m\n"
        printf "  Region: \e[32m$GRAFANA_REGION\e[0m\n"
        echo
        read -p "Is this the correct info? (y/n) " CONFIRM
        if [[ "$CONFIRM" != "y" ]]; then
            echo "Exiting as the Grafana info is not correct."
            exit 1
        fi
    fi

    # Grant the grafana MSI as a reader/viewer on the kusto cluster
    GRAFANA_IDENTITY=$(az grafana show -n "$GRAFANA" -g "$GRAFANA_RG" --query identity.principalId -o json | jq -r .)
    az kusto database add-principal --cluster-name $CLUSTER_NAME --resource-group $RESOURCE_GROUP --database-name Metrics --value role=Viewer name=AzureManagedGrafana type=app app-id=$GRAFANA_IDENTITY

    # Ensure data source already doesn't exist
    DATASOURCE_EXISTS=$(az grafana data-source show -n "$GRAFANA" -g "$GRAFANA_RG" --data-source $CLUSTER_NAME --query "name" -o json 2>/dev/null || echo "")
     if [[ -z "$DATASOURCE_EXISTS" ]]; then
        # Add Kusto cluster as datasource
        echo "Adding Kusto $CLUSTER_NAME as data source to grafana"
          # Add the kusto cluster as a datasource in grafana
          az grafana data-source create -n "$GRAFANA" -g "$GRAFANA_RG" --definition '{"name": "'$CLUSTER_NAME'","type": "grafana-azure-data-explorer-datasource","access": "proxy","jsonData": {"clusterUrl": "'$ADX_URL'"}}'
    else
        echo "The $GRAFANA instance already has a data-source $CLUSTER_NAME"
    fi

    # Import Grafana dashboards if the user wants
    read -p "Do you want to import pre-built dashboards in this Grafana instance? (y/n) " IMPORT_DASHBOARDS
    if [[ "$IMPORT_DASHBOARDS" == "y" ]]; then
      for DASHBOARD in api-server cluster-info metrics-stats namespaces pods; do
         az grafana dashboard create -n "$GRAFANA" --resource-group "$GRAFANA_RG" --definition @"$SCRIPT_DIR/dashboards/$DASHBOARD.json" --overwrite
      done
    else
        echo "No dashboards will be imported."
    fi
fi

echo
printf "\e[97mSuccessfully deployed ADX-Mon components to AKS cluster $CLUSTER.\e[0m"
echo
echo "Collected telemetry can be found the $DATABASE_NAME database at $ADX_FQDN."
if [ ! -z "$GRAFANA_ENDPOINT" ]; then
    echo
    echo "Azure Managed Grafana instance can be accessed at $GRAFANA_ENDPOINT."
fi
