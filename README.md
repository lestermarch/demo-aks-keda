# Event-Driven Autoscaling (KEDA) with Azure Kubernetes Service

## Summary

[Kubernetes Event-Drive Autoscaling]((https://keda.sh/)) (KEDA) is a component and extension of [Horizonal Pod Autoscaler](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/) (HPA) which can be added to any Kubernetes cluster, including [Azure Kubernetes Service](https://learn.microsoft.com/en-us/azure/aks/) (AKS), to reactively scale workloads based on various types of event.

## Lab

This repo guides you though the setup of a lab environment to demonstrate the utility of KEDA with AKS. The lab is comprised of the following resources:

- **[Azure Storage Account and Queue](https://learn.microsoft.com/en-us/azure/storage/queues/)** - The _KEDA target_ which will be monitored by the AKS KEDA operator to determine scaling requirements.
- **[Azure Container Registry](https://learn.microsoft.com/en-us/azure/container-registry/)** - The registry for building and storing the container images used in this demonstration.
- **[Azure Kubernetes Service](https://learn.microsoft.com/en-us/azure/aks/)** - The compute environment used to demonstrate workload scaling with KEDA.
- **[Azure Managed Identity](https://learn.microsoft.com/en-us/entra/identity/managed-identities-azure-resources/overview)** - The identity used by the AKS workloads and KEDA operator to authenticate to Azure resources.
- (_Optional_) **[Azure Log Analytics Workspace](https://learn.microsoft.com/en-us/azure/azure-monitor/logs/log-analytics-workspace-overview)** - The log store used by AKS to enable Container Insights and view real-time workload metrics, including autoscaling behaviour.

### Setup

> [!Note]
> The variables set during setup steps may be referenced by later steps. Take care to ensure these are not lost of overwritten during deployment.

### 1. Environment

This section 

#### 1.1. Variables

Set some variables:

```bash
# Modify as preferred:
RESOURCE_GROUP_NAME="rg-aks-keda-demo"
LOCATION="uksouth"
```

#### 1.2. Resource Group

Create an Azure resource group for the lab resources:

```bash
az group create \
  --name $RESOURCE_GROUP_NAME \
  --location $LOCATION
```

### 2. Storage

This section

#### 2.1. Variables

Set some variables:

```bash
# Modify as preferred:
STORAGE_ACCOUNT_PREFIX="stakskedademo"
STORAGE_QUEUE_NAME="demo"

# Do not modify:
RESOURCE_GROUP_ID=$(az group show --name $RESOURCE_GROUP_NAME --query id -o tsv)
ENTROPY=$(echo $RESOURCE_GROUP_ID | sha256sum | cut -c1-8)
STORAGE_ACCOUNT_NAME="$STORAGE_ACCOUNT_PREFIX$ENTROPY"
```

#### 2.2. Account

Create an Azure storage account:

```bash
az storage account create \
  --name $STORAGE_ACCOUNT_NAME \
  --resource-group $RESOURCE_GROUP_NAME \
  --location $LOCATION \
  --sku "Standard_LRS" \
  --min-tls-version "TLS1_2"
```

#### 2.3. Queue

Create an Azure storage queue:

```bash
az storage queue create \
  --name $STORAGE_QUEUE_NAME \
  --account-name $STORAGE_ACCOUNT_NAME \
  --auth-mode "login"
```

### 3. Container Registry

This section

#### 3.1. Variables

Set some variables:

```bash
# Modify as preferred:
ACR_PREFIX="acrakskedademo"

# Do not modify:
ACR_NAME="$ACR_PREFIX$ENTROPY
```

#### 3.2. Registry

Create an Azure container registry:

```bash
az acr create \
  --name $ACR_NAME \
  --location $LOCATION \
  --resource-group $RESOURCE_GROUP_NAME \
  --sku "Basic"
```

### 4. Azure Kubernetes Service

This section

#### 4.1. Variables

Set some variables:

```bash
# Modify as preferred:
AKS_CLUSTER_NAME="aks-keda-demo"

# Do not modify:
ACR_ID=$(az acr show --name $ACR_NAME --resource-group $RESOURCE_GROUP_NAME --query id -o tsv)
USER_ID=$(az ad signed-in-user show --query id -o tsv)
```

#### 4.2. Cluster

Create an AKS cluster:

```bash
az aks create \
  --name $AKS_CLUSTER_NAME \
  --resource-group $RESOURCE_GROUP_NAME \
  --location $LOCATION \
  --attach-acr $ACR_ID \
  --disable-local-accounts \
  --enable-aad \
  --enable-azure-rbac \
  --enable-cluster-autoscaler \
  --enable-keda \
  --enable-managed-identity \
  --enable-oidc-issuer \
  --enable-workload-identity \
  --generate-ssh-keys \
  --min-count 2 \
  --max-count 6 \
  --network-dataplane "cilium" \
  --network-plugin "azure" \
  --network-plugin-mode "overlay" \
  --network-policy "cilium" \
  --node-count 2 \
  --os-sku "AzureLinux" \
  --pod-cidr "172.100.0.0/16" \
  --tier "standard" \
  --zones 1 2 3
```

#### 4.3. Permissions

Grant yourself the `Azure Kubernetes RBAC Cluster Admin` role on the AKS cluster:

```bash
AKS_CLUSTER_ID=$(az aks show --name $AKS_CLUSTER_NAME --resource-group $RESOURCE_GROUP_NAME --query id -o tsv)

az role assignment create \
  --assignee-object-id $USER_ID \
  --assignee-principal-type "User" \
  --role "Azure Kubernetes Service RBAC Cluster Admin" \
  --scope $AKS_CLUSTER_ID
```

### (Optional) 5. Monitoring

This section

#### 5.1. Variables

Set some variables:

```bash
# Modify as preferred:
LOG_WORKSPACE_NAME="log-keda-demo"
```

#### 5.2. Workspace

Create an Azure log analytics workspace:

```bash
az monitor log-analytics workspace create \
  --name $LOG_WORKSPACE_NAME \
  --location $LOCATION \
  --resource-group $RESOURCE_GROUP_NAME
```

#### 5.3. Enable AKS Container Insights

```bash
LOG_WORKSPACE_ID=$(az monitor log-analytics workspace show --name $LOG_WORKSPACE_NAME --resource-group $RESOURCE_GROUP_NAME --query id -o tsv)

az aks enable-addons \
  --name $AKS_CLUSTER_NAME \
  --resource-group $RESOURCE_GROUP_NAME \
  --addon "monitoring" \
  --workspace-resource-id $LOG_WORKSPACE_ID
```

### 6. Workload Identity

This section

#### 6.1. Variables

Set some variables:

```bash
# Modify as preferred:
NAMESPACE="keda-demo"
WORKLOAD_IDENTITY_NAME="uid-aks-keda-demo"

# Do not modify:
AKS_OIDC_ISSUER=$(az aks show --name $AKS_CLUSTER_NAME --resource-group $RESOURCE_GROUP_NAME --query "oidcIssuerProfile.issuerUrl" -o tsv)
```

#### 6.2. Authenticate

Get your AKS credentials for authentication:

```bash
az aks get-credentials \
  --name $AKS_CLUSTER_NAME \
  --resource-group $RESOURCE_GROUP_NAME
```

#### 6.3. AKS Namespace

Create an AKS namespace:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Namespace
metadata:
  name: "${NAMESPACE}"
EOF
```

#### 6.4. Managed Identity

Create an Azure managed identity:

```bash
az identity create \
  --name $WORKLOAD_IDENTITY_NAME \
  --resource-group $RESOURCE_GROUP_NAME \
  --location $LOCATION
```

#### 6.5. AKS Service Account

```bash
WORKLOAD_IDENTITY_CLIENT_ID=$(az identity show --name $WORKLOAD_IDENTITY_NAME --resource-group $RESOURCE_GROUP_NAME --query clientId -o tsv)
WORKLOAD_IDENTITY_TENANT_ID=$(az identity show --name $WORKLOAD_IDENTITY_NAME --resource-group $RESOURCE_GROUP_NAME --query tenantId -o tsv)

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  annotations:
    azure.workload.identity/client-id: "${WORKLOAD_IDENTITY_CLIENT_ID}"
    azure.workload.identity/tenant-id: "${WORKLOAD_IDENTITY_TENANT_ID}"
  name: "${WORKLOAD_IDENTITY_NAME}"
  namespace: "${NAMESPACE}"
EOF
```

#### 6.6. Identity Federation

Federate the Azure managed identity and AKS service account:

```bash
az identity federated-credential create \
  --name "aks-sa-$WORKLOAD_IDENTITY_NAME" \
  --resource-group $RESOURCE_GROUP_NAME \
  --identity-name $WORKLOAD_IDENTITY_NAME \
  --issuer $AKS_OIDC_ISSUER \
  --subject system:serviceaccount:$NAMESPACE:$WORKLOAD_IDENTITY_NAME \
  --audiences api://AzureADTokenExchange
```

Federate the Azure managed identity and KEDA operator service account:

```bash
az identity federated-credential create \
  --name "aks-sa-keda-operator" \
  --resource-group $RESOURCE_GROUP_NAME \
  --identity-name $WORKLOAD_IDENTITY_NAME \
  --issuer $AKS_OIDC_ISSUER \
  --subject system:serviceaccount:kube-system:keda-operator \
  --audiences api://AzureADTokenExchange
```

### 7. Permissions

This section

#### 7.1. Variables

Set some variables:

```bash
# Do not modify:
STORAGE_ACCOUNT_ID=$(az storage account show --name $STORAGE_ACCOUNT_NAME --resource-group $RESOURCE_GROUP_NAME --query id -o tsv)
WORKLOAD_IDENTITY_PRINCIPAL_ID=$(az identity show --name $WORKLOAD_IDENTITY_NAME --resource-group $RESOURCE_GROUP_NAME --query principalId -o tsv)
```

#### 7.2. Storage Account RBAC

Grant yourself the `Storage Queue Data Contributor` role on the storage account:

```bash
az role assignment create \
  --assignee-object-id $USER_ID \
  --assignee-principal-type "User" \
  --role "Storage Queue Data Contributor" \
  --scope $STORAGE_ACCOUNT_ID
```

Grant the Azure managed identity the `Storage Queue Data Contributor` role on the storage account:

```bash
az role assignment create \
  --assignee-object-id $WORKLOAD_IDENTITY_PRINCIPAL_ID \
  --assignee-principal-type "ServicePrincipal" \
  --role "Storage Queue Data Contributor" \
  --scope $STORAGE_ACCOUNT_ID
```

#### 7.3. Container Registry RBAC

Grant yourself the `AcrPush` role on the container registry:

```bash
az role assignment create \
  --assignee-object-id $USER_ID \
  --assignee-principal-type "User" \
  --role "AcrPush" \
  --scope $ACR_ID
```

### 8. Build Applications

This section

#### 8.1. Variables

Set some variables:

```bash
# Modify as preferred:
MESSAGE_GENERATOR_IMAGE_NAME="az-message-generator"
MESSAGE_PROCESSOR_IMAGE_NAME="az-message-processor"
```

#### 8.2. Image Build

Build the Azure storage message generator image:

```bash
az acr build \
  --registry $ACR_NAME \
  --image $MESSAGE_GENERATOR_IMAGE_NAME:{{.Run.ID}} \
  apps/az-message-generator
```

Build the Azure storage message processor image:

```bash
az acr build \
  --registry $ACR_NAME \
  --image $MESSAGE_PROCESSOR_IMAGE_NAME:{{.Run.ID}} \
  apps/az-message-processor
```

### 9. Deployment

This section

#### 9.1. Variables

Set some variables:

```bash
# Modify as preferred:
AUTH_TRIGGER_NAME="azure-queue-auth"
DEPLOYMENT_NAME="azure-queue-processor"
MESSAGE_PROCESSING_SECONDS="3"
SCALED_OBJECT_NAME="azure-queue-scaler"
SCALING_QUEUE_LENGTH="10"

# Do not modify:
ACR_LOGIN_SERVER=$(az acr show --name $ACR_NAME --resource-group $RESOURCE_GROUP_NAME --query loginServer -o tsv)
MESSAGE_PROCESSOR_IMAGE_TAG=$(az acr repository show-tags --name $ACR_NAME --repository $MESSAGE_PROCESSOR_IMAGE_NAME --orderby time_desc --top 1 --query '[0]' -o tsv)
```

#### 9.2. Deployment

Create an Azure storage message processor deployment:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: $DEPLOYMENT_NAME
  namespace: $NAMESPACE
spec:
  selector:
    matchLabels:
      app: $DEPLOYMENT_NAME
  template:
    metadata:
      labels
        app: $DEPLOYMENT_NAME
        azure.workload.identity/use: "true"
    spec:
      serviceAccountName: $WORKLOAD_IDENTITY_NAME
      containers:
      - name: $MESSAGE_PROCESSOR_IMAGE_NAME
        image: $ACR_LOGIN_SERVER/$MESSAGE_PROCESSOR_IMAGE_NAME:$MESSAGE_PROCESSOR_IMAGE_TAG
        env:
        - name: AZURE_CLIENT_ID
          value: $WORKLOAD_IDENTITY_CLIENT_ID
        - name: MESSAGE_PROCESSING_SECONDS
          value: $MESSAGE_PROCESSING_SECONDS
        - name: STORAGE_ACCOUNT_NAME
          value: $STORAGE_ACCOUNT_NAME
        - name: STORAGE_QUEUE_NAME
          value: $STORAGE_QUEUE_NAME
EOF
```

#### 9.3. Enable KEDA

Create a KEDA trigger authentication:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: keda.sh/v1alpha1
kind: TriggerAuthentication
metadata:
  name: $AUTH_TRIGGER_NAME
  namespace: $NAMESPACE
spec:
  podIdentity:
    identityId: $WORKLOAD_IDENTITY_CLIENT_ID
    provider: azure-workload
EOF
```

Create a KEDA scaling object:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: $SCALED_OBJECT_NAME
  namespace: $NAMESPACE
spec:
  scaleTargetRef:
    name: $DEPLOYMENT_NAME
  pollingInterval: 10
  cooldownPeriod: 60
  minReplicaCount: 1
  maxReplicaCount: 120
  advanced:
    restoreToOriginalReplicaCount: true
    horizontalPodAutoscalerConfig:
      name: $SCALED_OBJECT_NAME-hpa"
      behavior:
        scaleDown:
          stabilizationWindowSeconds: 30
          policies:
          - type: Percent
            value: 20
            periodSeconds: 10
  triggers:
  - type: azure-queue
    metadata:
      accountName: $STORAGE_ACCOUNT_NAME
      queueName: $STORAGE_QUEUE_NAME
      queueLength: $SCALING_QUEUE_LENGTH
    authenticationRef:
      name: $AUTH_TRIGGER_NAME
EOF
```

### 10. Load Testing

This section

#### 10.1. Variables

Set some variables:

```bash
# Modify as preferred:
MESSAGE_COUNT_PER_MINUTE_MAX="256"
MESSAGE_COUNT_PER_MINUTE_MIN="32"

# Do not modify:
MESSAGE_GENERATOR_IMAGE_TAG=$(az acr repository show-tags --name $ACR_NAME --repository $MESSAGE_GENERATOR_IMAGE_NAME --orderby time_desc --top 1 --query '[0]' -o tsv)
```

#### 10.2. Testing

Create an Azure storage message generator pod:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  labels:
    app: azure-storage-queue-message-generator
    azure.workload.identity/use: "true"
  name: azure-storage-queue-message-generator
  namespace: $NAMESPACE
spec:
  serviceAccountName: $WORKLOAD_IDENTITY_NAME
  containers:
  - name: $MESSAGE_GENERATOR_IMAGE_NAME
    image: $ACR_LOGIN_SERVER/$MESSAGE_GENERATOR_IMAGE_NAME:$MESSAGE_GENERATOR_IMAGE_TAG
    env:
    - name: AZURE_CLIENT_ID
      value: $WORKLOAD_IDENTITY_CLIENT_ID
    - name: MESSAGE_COUNT_PER_MINUTE_MAX
      value: $MESSAGE_COUNT_PER_MINUTE_MAX
    - name: MESSAGE_COUNT_PER_MINUTE_MIN
      value: $MESSAGE_COUNT_PER_MINUTE_MIN
    - name: STORAGE_ACCOUNT_NAME
      value: $STORAGE_ACCOUNT_NAME
    - name: STORAGE_QUEUE_NAME
      value: $STORAGE_QUEUE_NAME
EOF
```
