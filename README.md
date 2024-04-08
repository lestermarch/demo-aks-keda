# Event-Driven Autoscaling with Azure Kubernetes Service

Repo to demonstrate Kubernetes Event-Driven Autoscaling (KEDA) with Azure Kubernetes Service.

## Lab Setup

1. Create a resource group:

```azurecli
RESOURCE_GROUP_NAME="rg-aks-keda-demo"
LOCATION="uksouth"

az group create \
  --name $RESOURCE_GROUP_NAME \
  --location $LOCATION
```

2. Create a storage account and queue:

```azurecli
RESOURCE_GROUP_ID=$(az group show --name $RESOURCE_GROUP_NAME --query id -o tsv)
ENTROPY=$(echo $RESOURCE_GROUP_ID | sha256sum | cut -c1-8)
STORAGE_ACCOUNT_NAME="stakskedademo$ENTROPY"
STORAGE_QUEUE_NAME="demo"

az storage account create \
  --name $STORAGE_ACCOUNT_NAME \
  --resource-group $RESOURCE_GROUP_NAME \
  --location $LOCATION \
  --sku "Standard_LRS" \
  --min-tls-version "TLS1_2"

az storage queue create \
  --name $STORAGE_QUEUE_NAME \
  --account-name $STORAGE_ACCOUNT_NAME \
  --auth-mode "login"
```

3. Create an Azure Container Registry:

```azurecli
ACR_NAME="acrkedademo$ENTROPY"
az acr create \
  --name $ACR_NAME \
  --location $LOCATION \
  --resource-group $RESOURCE_GROUP_NAME \
  --sku "Basic"
```

4. Assign ACR permissions:

```azurecli
ACR_ID=$(az acr show --name $ACR_NAME --resource-group $RESOURCE_GROUP_NAME --query id -o tsv)
USER_ID=$(az ad signed-in-user show --query id -o tsv)

az role assignment create \
  --assignee-object-id $USER_ID \
  --assignee-principal-type "User" \
  --role "AcrPush" \
  --scope $ACR_ID
```

5. Build and push the message generator app to ACR

```azurecli
MESSAGE_GENERATOR_IMAGE_NAME="az-message-generator"

az acr build \
  --registry $ACR_NAME \
  --image $MESSAGE_GENERATOR_IMAGE_NAME:{{.Run.ID}} \
  apps/az-message-generator
```

4. Create an AKS cluster with KEDA enabled:

```azurecli
AKS_CLUSTER_NAME="aks-keda-demo"

az aks create \
  --name $AKS_CLUSTER_NAME \
  --resource-group $RESOURCE_GROUP_NAME \
  --location $LOCATION \
  --attach-acr $ACR_ID \
  --disable-local-accounts \
  --enable-aad \
  --enable-addons "azure-keyvault-secrets-provider" \
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

5. Assign AKS cluster admin permissions:

```azurecli
AKS_CLUSTER_ID=$(az aks show --name $AKS_CLUSTER_NAME --resource-group $RESOURCE_GROUP_NAME --query id -o tsv)

az role assignment create \
  --assignee-object-id $USER_ID \
  --assignee-principal-type "User" \
  --role "Azure Kubernetes Service RBAC Cluster Admin" \
  --scope $AKS_CLUSTER_ID
```

6. Get AKS cluster credentials:

```azurecli
az aks get-credentials \
  --name $AKS_CLUSTER_NAME \
  --resource-group $RESOURCE_GROUP_NAME
```

7. Create a namespace:

```azurecli
NAMESPACE="keda-demo"

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Namespace
metadata:
  name: "${NAMESPACE}"
EOF
```

8. Create a managed workload identity:

```azurecli
AKS_OIDC_ISSUER=$(az aks show --name $AKS_CLUSTER_NAME --resource-group $RESOURCE_GROUP_NAME --query "oidcIssuerProfile.issuerUrl" -o tsv)
WORKLOAD_IDENTITY_NAME="uid-aks-keda-demo"

az identity create \
  --name $WORKLOAD_IDENTITY_NAME \
  --resource-group $RESOURCE_GROUP_NAME \
  --location $LOCATION

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

az identity federated-credential create \
  --name "aks-sa-$WORKLOAD_IDENTITY_NAME" \
  --resource-group $RESOURCE_GROUP_NAME \
  --identity-name $WORKLOAD_IDENTITY_NAME \
  --issuer $AKS_OIDC_ISSUER \
  --subject system:serviceaccount:$NAMESPACE:$WORKLOAD_IDENTITY_NAME \
  --audiences api://AzureADTokenExchange

az identity federated-credential create \
  --name "aks-sa-keda-operator" \
  --resource-group $RESOURCE_GROUP_NAME \
  --identity-name $WORKLOAD_IDENTITY_NAME \
  --issuer $AKS_OIDC_ISSUER \
  --subject system:serviceaccount:kube-system:keda-operator \
  --audiences api://AzureADTokenExchange
```

9. Assign storage queue permissions:

```azurecli
STORAGE_ACCOUNT_ID=$(az storage account show --name $STORAGE_ACCOUNT_NAME --resource-group $RESOURCE_GROUP_NAME --query id -o tsv)
WORKLOAD_IDENTITY_PRINCIPAL_ID=$(az identity show --name $WORKLOAD_IDENTITY_NAME --resource-group $RESOURCE_GROUP_NAME --query principalId -o tsv)

az role assignment create \
  --assignee-object-id $WORKLOAD_IDENTITY_PRINCIPAL_ID \
  --assignee-principal-type "ServicePrincipal" \
  --role "Storage Queue Data Contributor" \
  --scope $STORAGE_ACCOUNT_ID

az role assignment create \
  --assignee-object-id $USER_ID \
  --assignee-principal-type "User" \
  --role "Storage Queue Data Contributor" \
  --scope $STORAGE_ACCOUNT_ID
```

10. Create a deployment to scale using KEDA:

```azurecli
DEPLOYMENT_NAME="azure-queue-processor"
STORAGE_ACCOUNT_KEY=$(az storage account keys list --account-name $STORAGE_ACCOUNT_NAME --resource-group $RESOURCE_GROUP_NAME --query [0].value -o tsv)

cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: "${DEPLOYMENT_NAME}"
  namespace: "${NAMESPACE}"
spec:
  selector:
    matchLabels:
      app: "${DEPLOYMENT_NAME}"
  template:
    metadata:
      labels:
        app: "${DEPLOYMENT_NAME}"
        azure.workload.identity/use: "true"
    spec:
      containers:
      - name: azure-cli
        image: mcr.microsoft.com/azure-cli
        command: ["/bin/bash", "-c"]
        args:
        - |
          while true; do
            az storage message get \
              --account-name "${STORAGE_ACCOUNT_NAME}" \
              --account-key "${STORAGE_ACCOUNT_KEY}" \
              --queue-name "${STORAGE_QUEUE_NAME}" \
              --auth-mode key \
              --query id \
              --output tsv \
              --only-show-errors
          done
EOF
```

11. Create a KEDA trigger authentication and scaled object for Azure storage queues:

```azurecli
AUTH_TRIGGER_NAME="azure-queue-auth"
SCALED_OBJECT_NAME="azure-queue-scaler"
SCALING_QUEUE_LENGTH="10"

cat <<EOF | kubectl apply -f -
apiVersion: keda.sh/v1alpha1
kind: TriggerAuthentication
metadata:
  name: "${AUTH_TRIGGER_NAME}"
  namespace: "${NAMESPACE}"
spec:
  podIdentity:
    identityId: "${WORKLOAD_IDENTITY_CLIENT_ID}"
    provider: azure-workload
---
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: "${SCALED_OBJECT_NAME}"
  namespace: "${NAMESPACE}"
spec:
  scaleTargetRef:
    name: "${DEPLOYMENT_NAME}"
  pollingInterval: 10
  cooldownPeriod: 60
  minReplicaCount: 1
  maxReplicaCount: 120
  advanced:
    restoreToOriginalReplicaCount: true
    horizontalPodAutoscalerConfig:
      name: "${SCALED_OBJECT_NAME}-hpa"
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
      accountName: "${STORAGE_ACCOUNT_NAME}"
      queueName: "${STORAGE_QUEUE_NAME}"
      queueLength: "${SCALING_QUEUE_LENGTH}"
    authenticationRef:
      name: "${AUTH_TRIGGER_NAME}"
EOF
```

### Testing

Create a pod to automatically generate Azure Storage Queue messages:

```azurecli
ACR_LOGIN_SERVER=$(az acr show --name $ACR_NAME --resource-group $RESOURCE_GROUP_NAME --query loginServer -o tsv)
MESSAGE_GENERATOR_IMAGE_TAG=$(az acr repository show-tags --name $ACR_NAME --repository $MESSAGE_GENERATOR_IMAGE_NAME --orderby time_desc --top 1 --query '[0]' -o tsv)
MESSAGE_COUNT_PER_MINUTE_MAX="256"
MESSAGE_COUNT_PER_MINUTE_MIN="32"

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  labels:
    app: azure-storage-queue-message-generator
    azure.workload.identity/use: "true"
  name: azure-storage-queue-message-generator
  namespace: "${NAMESPACE}"
spec:
  serviceAccountName: "${WORKLOAD_IDENTITY_NAME}"
  containers:
  - name: $MESSAGE_GENERATOR_IMAGE_NAME
    image: $ACR_LOGIN_SERVER/$MESSAGE_GENERATOR_IMAGE_NAME:$MESSAGE_GENERATOR_IMAGE_TAG
    env:
    - name: AZURE_CLIENT_ID
      value: "${WORKLOAD_IDENTITY_CLIENT_ID}"
    - name: MESSAGE_COUNT_PER_MINUTE_MAX
      value: "${MESSAGE_COUNT_PER_MINUTE_MAX}"
    - name: MESSAGE_COUNT_PER_MINUTE_MIN
      value: "${MESSAGE_COUNT_PER_MINUTE_MIN}"
    - name: STORAGE_ACCOUNT_NAME
      value: "${STORAGE_ACCOUNT_NAME}"
    - name: STORAGE_QUEUE_NAME
      value: "${STORAGE_QUEUE_NAME}"
EOF
```
