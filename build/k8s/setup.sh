#!/bin/bash
set -euo pipefail
SCRIPT_DIR="$(dirname "${BASH_SOURCE[0]}")"

# Inputs
AKS_CLUSTER=""
AKS_SUB=""
AKS_RG=""
KUSTO_CLUSTER=""
KUSTO_SUB=""
KUSTO_RG=""
GRAFANA=""
GRAFANA_SUB=""
GRAFANA_RG=""

# Other variables
AKS_REGION=""
KUSTO_REGION=""
KUSTO_FQDN=""
SETUP_GRAFANA="false"
GRAFANA_ENDPOINT=""
INTERACTIVE="true"

# Help usage
usage() {
  echo "Usage: $0 \
-c '<aks-cluster-name> <aks-cluster-sub-id> <aks-cluster-resource-group>' \
-k '<kusto-cluster-name> <kusto-cluster-sub-id> <kusto-cluster-resource-group>' \
[-g '<managed-grafana-name> <grafana-sub-id> <grafana-resource-group>']" 1>&2;
  exit 1;
}

# If any arguments are passed to the script, run in non-interactive mode
if [[ $# -gt 0 ]]; then
  INTERACTIVE="false"
  # Read command line arguments
  while getopts ":c:k:g:" o; do
      case "${o}" in
          c)
              A=${OPTARG} # AKS Cluster Name
              read -ra array <<< "$A"
              AKS_CLUSTER=${array[0]}
              AKS_SUB=${array[1]}
              AKS_RG=${array[2]}
              ;;
          k)
              K=${OPTARG} # Kusto Cluster Name
              read -ra array <<< "$K"
              KUSTO_CLUSTER=${array[0]}
              KUSTO_SUB=${array[1]}
              KUSTO_RG=${array[2]}
              ;;
          g)
              SETUP_GRAFANA="true"
              G=${OPTARG} # Grafana instance name
              read -ra array <<< "$G"
              GRAFANA=${array[0]}
              GRAFANA_SUB=${array[1]}
              GRAFANA_RG=${array[2]}
              ;;
          *)
              usage
              ;;
      esac
  done
  shift $((OPTIND-1))
fi

interactive() {
  "$INTERACTIVE" == "true"
}

for cmd in az jq envsubst kubectl; do
    if ! command -v $cmd &>/dev/null; then
        echo "$cmd command could not be found. Please install it before continuing."
        exit 1
    fi
done
echo "All dependencies are installed."

# Make sure user is logged in to Azure CLI and that the token is not expired
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

# Install required extensions;
# - resource-graph: Required to run queries against Azure resources
# - kusto: Required to work with kusto clusters & databases
for EXT in resource-graph kusto; do
    INSTALL_EXT=""
    if ! az extension show --name $EXT &> /dev/null; then
        if interactive; then
        read -p "The '$EXT' extension is not installed. Do you want to install it now? (y/n) " INSTALL_EXT
        else
            INSTALL_EXT="y"
        fi
        if [[ "$INSTALL_EXT" == "y" ]]; then
            az extension add --name "$EXT"
        else
            echo "The '$EXT' extension is required. Exiting."
            exit 1
        fi
    fi
done

if interactive; then
  # Ask for the name of the aks cluster and read it as input. With that name, run a graph query to find its information
  read -p "Please enter the name of the AKS cluster where ADX-Mon components should be deployed: " AKS_CLUSTER
  while [[ -z "${AKS_CLUSTER// }" ]]; do
      echo "Cluster cannot be empty. Please enter the name of the AKS cluster:"
      read AKS_CLUSTER
  done

  # Run a graph query to find the cluster's resource group and subscription id
  CLUSTER_INFO=$(az graph query -q "Resources | where type =~ 'Microsoft.ContainerService/managedClusters' and name =~ '$AKS_CLUSTER' | project resourceGroup, subscriptionId, location")
  if [[ $(echo $CLUSTER_INFO | jq '.data | length') -eq 0 ]]; then
      echo "No AKS cluster could be found for the cluster name '$AKS_CLUSTER'. Exiting."
      exit 1
  fi

  AKS_RG=$(echo $CLUSTER_INFO | jq -r '.data[0].resourceGroup')
  AKS_SUB=$(echo $CLUSTER_INFO | jq -r '.data[0].subscriptionId')
  AKS_REGION=$(echo $CLUSTER_INFO | jq -r '.data[0].location')
else
  CLUSTER_INFO=$(az graph query -q "Resources | where type =~ 'Microsoft.ContainerService/managedClusters' and name =~ '$AKS_CLUSTER' and subscriptionId =~ '$AKS_SUB' and resourceGroup =~ '$AKS_RG' | project resourceGroup, subscriptionId, location")
  AKS_REGION=$(echo $CLUSTER_INFO | jq -r '.data[0].location')
fi

# Find the managed identity client ID attached to the AKS node pools
NODE_POOL_IDENTITY=$(az aks show --resource-group $AKS_RG --name $AKS_CLUSTER --query identityProfile.kubeletidentity.clientId -o json | jq . -r)

if interactive; then
  echo
  printf "Found AKS cluster info:\n"
  printf "  AKS Cluster Name: \e[32m$AKS_CLUSTER\e[0m\n"
  printf "  Resource Group: \e[32m$AKS_RG\e[0m\n"
  printf "  Subscription ID:\e[32m $AKS_SUB\e[0m\n"
  printf "  Region: \e[32m$AKS_REGION\e[0m\n"
  printf "  Managed Identity Client ID: \e[32m$NODE_POOL_IDENTITY\e[0m\n"
  echo
  read -p "Is this information correct? (y/n) " CONFIRM
  if [[ "$CONFIRM" != "y" ]]; then
      echo "Exiting as the information is not correct."
      exit 1
  fi
fi

# Get credentials for the AKS cluster
az aks get-credentials --subscription $AKS_SUB --resource-group $AKS_RG --name $AKS_CLUSTER

if interactive; then
  echo
  read -p "Please enter the Azure Data Explorer cluster name where ADX-Mon will store telemetry: " KUSTO_CLUSTER
  while [[ -z "${KUSTO_CLUSTER// }" ]]; do
      echo "ADX cluster name cannot be empty. Please enter the database name:"
      read KUSTO_CLUSTER
  done
fi

CLUSTER_INFO=$(az graph query -q "Resources | where type =~ 'Microsoft.Kusto/clusters' and name =~ '$KUSTO_CLUSTER' | project name, resourceGroup, subscriptionId, location, properties.uri")
KUSTO_CLUSTER_COUNT=$(echo $CLUSTER_INFO | jq '.data | length')

if [[ $KUSTO_CLUSTER_COUNT -eq 0 ]]; then
  if interactive; then
    echo "No Kusto cluster could be found for the database name '$KUSTO_CLUSTER'. Creating cluster."
    # We'll create the Kusto cluster in the same sub as the AKS cluster
    KUSTO_SUB=$AKS_SUB
    KUSTO_REGION=$AKS_REGION
    # Process available SKUs in the region, sorted by recommended
    recommended_skus=("Standard_L8as_v3" "Standard_L16as_v3" "Standard_L32as_v3")
    skus=$(az kusto cluster list-sku --subscription "$KUSTO_SUB" --query "[?contains(locations, '$KUSTO_REGION') && tier == 'Standard'].name" --output tsv)
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
            KUSTO_CLUSTER_SKU="${choice// (recommended)/}"
            echo "SKU selected: $KUSTO_CLUSTER_SKU"
            break
        else
            echo "Invalid selection. Please choose a valid option."
        fi
    done

    KUSTO_RG=$AKS_RG
    echo "Creating Kusto cluster with database name '$KUSTO_CLUSTER' in resource group '$KUSTO_RG' with SKU '$KUSTO_CLUSTER_SKU'"
    az kusto cluster create --name "$KUSTO_CLUSTER" --resource-group "$KUSTO_RG" --sku name="$KUSTO_CLUSTER_SKU" tier="Standard"
    KUSTO_FQDN=$(az kusto cluster show --resource-group "$KUSTO_RG" --name "$KUSTO_CLUSTER" --query "uri" -o tsv)
  else
    # Non-interactive mode assumes that one will have already have a kusto cluster created
    echo "No Kusto cluster '$KUSTO_CLUSTER' could be found. Exiting."
    exit 1
  fi
else
  if interactive; then
    KUSTO_SUB=$(echo $CLUSTER_INFO | jq -r '.data[0].subscriptionId')
    KUSTO_RG=$(echo $CLUSTER_INFO | jq -r '.data[0].resourceGroup')
    KUSTO_REGION=$(echo $CLUSTER_INFO | jq -r '.data[0].location')
    KUSTO_FQDN=$(echo $CLUSTER_INFO | jq -r '.data[0].properties_uri')

    echo
    printf "Found ADX cluster info:\n"
    printf "  Cluster Name: \e[32m$KUSTO_CLUSTER\e[0m\n"
    printf "  Subscription ID: \e[32m$KUSTO_SUB\e[0m\n"
    printf "  Resource Group: \e[32m$KUSTO_RG\e[0m\n"
    printf "  ADX FQDN: \e[32m$KUSTO_FQDN\e[0m\n"
    printf "  Region: \e[32m$KUSTO_REGION\e[0m\n"
    echo
    read -p "Is this the correct ADX cluster info? (y/n) " CONFIRM
    if [[ "$CONFIRM" != "y" ]]; then
        echo "Exiting as the ADX cluster info is not correct."
        exit 1
    fi
  else
    CLUSTER_INFO=$(az graph query -q "Resources | where type =~ 'Microsoft.Kusto/clusters' and name =~ '$KUSTO_CLUSTER' and subscriptionId =~ '$KUSTO_SUB' and resourceGroup =~ '$KUSTO_RG' | project location, properties.uri")
    KUSTO_REGION=$(echo $CLUSTER_INFO | jq -r '.data[0].location')
    KUSTO_FQDN=$(echo $CLUSTER_INFO | jq -r '.data[0].properties_uri')
  fi
fi

for DATABASE_NAME in Metrics Logs; do
    # Check if the $DATABASE_NAME database exists
    DATABASE_EXISTS=$(az kusto database show --cluster-name $KUSTO_CLUSTER --subscription $KUSTO_SUB --resource-group $KUSTO_RG --database-name $DATABASE_NAME --query "name" -o tsv 2>/dev/null || echo "")
    if [[ -z "$DATABASE_EXISTS" ]]; then
        echo "The $DATABASE_NAME database does not exist. Creating it now."
        az kusto database create --cluster-name $KUSTO_CLUSTER --subscription $KUSTO_SUB --resource-group $KUSTO_RG --database-name $DATABASE_NAME --read-write-database  soft-delete-period=P30D hot-cache-period=P7D location=$KUSTO_REGION
    else
        echo "The $DATABASE_NAME database already exists."
    fi

    # Check if the NODE_POOL_IDENTITY is an admin on the $DATABASE_NAME database
    ADMIN_CHECK=$(az kusto database list-principal --subscription $KUSTO_SUB --cluster-name $KUSTO_CLUSTER --resource-group $KUSTO_RG --database-name $DATABASE_NAME --query "[?type=='App' && appId=='$NODE_POOL_IDENTITY' && role=='Admin']" -o tsv)
    if [[ -z "$ADMIN_CHECK" ]]; then
        echo "The Managed Identity Client ID is not configured to use database $DATABASE_NAME. Adding it as an admin."
        az kusto database add-principal --subscription $KUSTO_SUB --cluster-name $KUSTO_CLUSTER --resource-group $KUSTO_RG --database-name $DATABASE_NAME --value role=Admin name=ADXMon type=app app-id=$NODE_POOL_IDENTITY
    else
        echo "The Managed Identity Client ID is already configured to use database $DATABASE_NAME."
    fi
done

export CLUSTER=$AKS_CLUSTER
export REGION=$AKS_REGION
export CLIENT_ID=$NODE_POOL_IDENTITY
export ADX_URL=$KUSTO_FQDN
envsubst < $SCRIPT_DIR/ingestor.yaml | kubectl apply -f -
envsubst < $SCRIPT_DIR/collector.yaml | kubectl apply -f -
kubectl apply -f $SCRIPT_DIR/ksm.yaml

# Restart all workloads for schema changes to be reflected if this is an update
kubectl rollout restart sts ingestor -n adx-mon
kubectl rollout restart ds collector -n adx-mon
kubectl rollout restart deploy collector-singleton -n adx-mon

CONFIRM=""
if interactive; then
  echo
  read -p "Do you want to setup an Azure Managed Grafana instance to visualize the AKS telemetry? (y/n) " CONFIRM
elif [[ "$SETUP_GRAFANA" == "true" ]]; then
  CONFIRM="y"
fi

if [[ "$CONFIRM" == "y" ]]; then
    if ! az extension show --name amg &> /dev/null; then
        if interactive; then
          read -p "The 'amg' extension is not installed. Do you want to install it now? (y/n) " INSTALL_EXT
        else
          INSTALL_EXT="y"
        fi

        if [[ "$INSTALL_EXT" == "y" ]]; then
          az extension add --name "amg"
        else
          echo "The 'amg' extension is required to setup grafana. Exiting."
          exit 1
        fi
    fi

    if interactive; then
      read -p "Please enter name of Azure Managed Grafana instance (new or existing): " GRAFANA
      while [[ -z "${GRAFANA// }" ]]; do
          echo "Instance name cannot be empty. Please enter the name:"
          read GRAFANA
      done
    fi

    GRAFANA_INFO=$(az graph query -q "Resources | where type =~ 'Microsoft.Dashboard/grafana' and name =~ '$GRAFANA' | project name, resourceGroup, subscriptionId, location, properties.endpoint")
    GRAFANA_COUNT=$(echo $GRAFANA_INFO | jq '.data | length')
    if [[ GRAFANA_COUNT -eq 0 ]]; then
        if interactive; then
          # We'll create the Grafana instance in the same sub as the Kusto cluster
          GRAFANA_SUB=$KUSTO_SUB
          GRAFANA_RG=$KUSTO_RG
          # Create Azure Managed Grafana
          echo "The $GRAFANA instance does not exist. Creating it in $GRAFANA_RG resource group."
          az grafana create --name "$GRAFANA" --resource-group "$GRAFANA_RG" --subscription $GRAFANA_SUB
          
          # Wait for Grafana instance to be fully provisioned and ready
          echo "Waiting for Grafana instance to be ready..."
          GRAFANA_READY=false
          WAIT_ATTEMPTS=0
          MAX_WAIT_ATTEMPTS=30  # 15 minutes max wait time
          
          while [[ "$GRAFANA_READY" != "true" && $WAIT_ATTEMPTS -lt $MAX_WAIT_ATTEMPTS ]]; do
            GRAFANA_INFO=$(az grafana show --name "$GRAFANA" --resource-group "$GRAFANA_RG" --subscription $GRAFANA_SUB -o json 2>/dev/null || echo "{}")
            PROVISIONING_STATE=$(echo $GRAFANA_INFO | jq -r '.properties.provisioningState // "Unknown"')
            ENDPOINT=$(echo $GRAFANA_INFO | jq -r '.properties.endpoint // ""')
            
            if [[ "$PROVISIONING_STATE" == "Succeeded" && -n "$ENDPOINT" && "$ENDPOINT" != "null" ]]; then
              GRAFANA_READY=true
              echo "Grafana instance is ready (provisioning state: $PROVISIONING_STATE)"
            else
              echo "Grafana instance not ready yet (provisioning state: $PROVISIONING_STATE). Waiting 30 seconds..."
              sleep 30
              ((WAIT_ATTEMPTS++))
            fi
          done
          
          if [[ "$GRAFANA_READY" != "true" ]]; then
            echo "Warning: Grafana instance may not be fully ready after waiting. Proceeding anyway..."
          fi
          
          GRAFANA_INFO=$(az grafana show --name "$GRAFANA" --resource-group "$GRAFANA_RG" --subscription $GRAFANA_SUB -o json)
          GRAFANA_ENDPOINT=$(echo $GRAFANA_INFO | jq -r '.properties.endpoint')
        else
          # Non-interactive mode assumes that one will have already have a managed grafana created
          echo "No Managed Grafana '$GRAFANA' could be found. Exiting."
          exit 1
        fi
    else
        if interactive; then
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
        else
          GRAFANA_INFO=$(az graph query -q "Resources | where type =~ 'Microsoft.Dashboard/grafana' and name =~ '$GRAFANA' and subscriptionId =~ '$GRAFANA_SUB' and resourceGroup =~ '$GRAFANA_RG' | project location, properties.endpoint")
          GRAFANA_ENDPOINT=$(echo $GRAFANA_INFO | jq -r '.data[0].properties_endpoint')
        fi
    fi

    # Grant the grafana MSI as a reader/viewer on the kusto cluster
    GRAFANA_IDENTITY=$(az grafana show -n "$GRAFANA" -g "$GRAFANA_RG" --query identity.principalId -o json | jq -r .)
    az kusto database add-principal --cluster-name $KUSTO_CLUSTER --resource-group $KUSTO_RG --database-name Metrics --value role=Viewer name=AzureManagedGrafana type=app app-id=$GRAFANA_IDENTITY

    # Ensure data source already doesn't exist
    DATASOURCE_EXISTS=$(az grafana data-source show -n "$GRAFANA" -g "$GRAFANA_RG" --data-source $KUSTO_CLUSTER --query "name" -o json 2>/dev/null || echo "")
     if [[ -z "$DATASOURCE_EXISTS" ]]; then
        # Add Kusto cluster as datasource
        echo "Adding Kusto $KUSTO_CLUSTER as data source to grafana"
          # Add the kusto cluster as a datasource in grafana
          az grafana data-source create -n "$GRAFANA" -g "$GRAFANA_RG" --definition '{"name": "'$KUSTO_CLUSTER'","type": "grafana-azure-data-explorer-datasource","access": "proxy","jsonData": {"clusterUrl": "'$ADX_URL'"}}'
    else
        echo "The $GRAFANA instance already has a data-source $KUSTO_CLUSTER"
    fi

    # Import Grafana dashboards
    if interactive; then
    read -p "Do you want to import pre-built dashboards in this Grafana instance? (y/n) " IMPORT_DASHBOARDS
    else
      IMPORT_DASHBOARDS="y"
    fi

    if [[ "$IMPORT_DASHBOARDS" == "y" ]]; then
      echo "Importing dashboards to Grafana instance..."
      for DASHBOARD in api-server cluster-info metrics-stats namespaces pods; do
        echo "Importing dashboard: $DASHBOARD"
        DASHBOARD_ATTEMPTS=0
        DASHBOARD_SUCCESS=false
        
        while [[ "$DASHBOARD_SUCCESS" != "true" && $DASHBOARD_ATTEMPTS -lt 3 ]]; do
          if az grafana dashboard create -n "$GRAFANA" --resource-group "$GRAFANA_RG" --definition @"$SCRIPT_DIR/dashboards/$DASHBOARD.json" --overwrite 2>/dev/null; then
            DASHBOARD_SUCCESS=true
            echo "Successfully imported dashboard: $DASHBOARD"
          else
            ((DASHBOARD_ATTEMPTS++))
            if [[ $DASHBOARD_ATTEMPTS -lt 3 ]]; then
              echo "Failed to import dashboard $DASHBOARD (attempt $DASHBOARD_ATTEMPTS/3). Retrying in 10 seconds..."
              sleep 10
            else
              echo "Failed to import dashboard $DASHBOARD after 3 attempts. Skipping..."
            fi
          fi
        done
      done
    else
        echo "No dashboards will be imported."
    fi
fi

echo
printf "\e[97mSuccessfully deployed ADX-Mon components to AKS cluster $AKS_CLUSTER.\e[0m"
echo
echo "Collected telemetry can be found the $DATABASE_NAME database at $KUSTO_FQDN."
if [ ! -z "$GRAFANA_ENDPOINT" ]; then
    echo
    echo "Azure Managed Grafana instance can be accessed at $GRAFANA_ENDPOINT."
fi
