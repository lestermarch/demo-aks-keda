# Event-Driven Autoscaling with Azure Kubernetes Service

Repo to demonstrate Kubernetes Event-Driven Autoscaling (KEDA) with Azure Kubernetes Service.

## Setup

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

3. Create an AKS cluster with KEDA enabled:

```azurecli
AKS_CLUSTER_NAME="aks-keda-demo"

az aks create \
  --name $AKS_CLUSTER_NAME \
  --resource-group $RESOURCE_GROUP_NAME \
  --location $LOCATION \
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

4. Assign AKS cluster admin permissions:

```azurecli
AKS_CLUSTER_ID=$(az aks show --name $AKS_CLUSTER_NAME --resource-group $RESOURCE_GROUP_NAME --query id -o tsv)
USER_ID=$(az ad signed-in-user show --query id -o tsv)

az role assignment create \
  --assignee-object-id $USER_ID \
  --assignee-principal-type "User" \
  --role "Azure Kubernetes Service RBAC Cluster Admin" \
  --scope $AKS_CLUSTER_ID
```

5. Get AKS cluster credentials:

```azurecli
az aks get-credentials \
  --name $AKS_CLUSTER_NAME \
  --resource-group $RESOURCE_GROUP_NAME
```

6. Create a namespace:

```azurecli
NAMESPACE="keda-demo"

cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Namespace
metadata:
  name: "${NAMESPACE}"
EOF
```

7. Create a managed workload identity:

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

8. Assign storage queue permissions:

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

9. Create a deployment to scale using KEDA:

```azurecli
DEPLOYMENT_NAME="azure-queue-processor"

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
      serviceAccountName: "${WORKLOAD_IDENTITY_NAME}"
      containers:
      - name: httpd
        image: httpd
EOF
```

10. Create a KEDA trigger authentication and scaled object for Azure storage queues:

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
