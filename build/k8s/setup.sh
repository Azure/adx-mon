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
    read -p "The '$ext' extension is not installed. Do you want to install it now? (y/n) " INSTALL_EXT
    if [[ "$INSTALL_EXT" == "y" ]]; then
        az extension add --name "$EXT"
    else
        echo "The '$EXT' extension is required. Exiting."
        exit 1
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
echo -e "Found AKS cluster info:"
echo -e "  AKS Cluster Name: \e[32m$CLUSTER\e[0m"
echo -e "  Resource Group: \e[32m$RESOURCE_GROUP\e[0m"
echo -e "  Subscription ID:\e[32m $SUBSCRIPTION_ID\e[0m"
echo -e "  Region: \e[32m$REGION\e[0m"
echo -e "  Managed Identity Client ID: \e[32m$NODE_POOL_IDENTITY\e[0m"
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
KUSTO_REGION=$(echo $CLUSTER_INFO | jq -r '.data[0].location')

if [[ $CLUSTER_COUNT -eq 0 ]]; then
    echo "No Kusto cluster could be found for the database name '$CLUSTER_NAME'. Exiting."
    exit 1
else
    CLUSTER_NAME=$(echo $CLUSTER_INFO | jq -r '.data[0].name')
    SUBSCRIPTION_ID=$(echo $CLUSTER_INFO | jq -r '.data[0].subscriptionId')
    RESOURCE_GROUP=$(echo $CLUSTER_INFO | jq -r '.data[0].resourceGroup')
    ADX_FQDN=$(echo $CLUSTER_INFO | jq -r '.data[0].properties_uri')
fi
echo
echo "Found ADX cluster info:"
echo -e "  Cluster Name: \e[32m$CLUSTER_NAME\e[0m"
echo -e "  Subscription ID: \e[32m$SUBSCRIPTION_ID\e[0m"
echo -e "  Resource Group: \e[32m$RESOURCE_GROUP\e[0m"
echo -e "  ADX FQDN: \e[32m$ADX_FQDN\e[0m"
echo -e "  Region: \e[32m$KUSTO_REGION\e[0m"
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

echo
echo -e "\e[97mSuccessfully deployed ADX-Mon components to AKS cluster $CLUSTER.\e[0m"
echo
echo "Collected telemetry can be found the $DATABASE_NAME database at $ADX_FQDN."
