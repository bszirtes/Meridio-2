#!/usr/bin/env bash
# Setup Kind cluster with networking prerequisites for Meridio-2 e2e tests.
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-meridio-e2e}"
K8S_VERSION="${K8S_VERSION:-v1.32.2}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"

echo "=== Setting up Kind cluster for Meridio-2 e2e tests ==="

for cmd in kind kubectl docker; do
    command -v "$cmd" >/dev/null 2>&1 || { echo "❌ $cmd not found"; exit 1; }
done

# Delete existing cluster
if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
    echo "⚠️  Deleting existing cluster '${CLUSTER_NAME}'..."
    kind delete cluster --name "${CLUSTER_NAME}"
fi

echo "📦 Creating Kind cluster '${CLUSTER_NAME}'..."
cat <<EOF | kind create cluster --name "${CLUSTER_NAME}" --image "kindest/node:${K8S_VERSION}" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
- role: worker
- role: worker
networking:
  disableDefaultCNI: false
  podSubnet: "10.244.0.0/16"
  serviceSubnet: "10.96.0.0/12"
EOF

echo "⏳ Waiting for nodes..."
kubectl wait --for=condition=Ready nodes --all --timeout=120s

echo "📦 Installing Gateway API CRDs..."
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.1/standard-install.yaml

echo "📦 Installing Multus CNI..."
kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/master/deployments/multus-daemonset.yml

echo "📦 Installing Whereabouts IPAM..."
kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/whereabouts/master/doc/crds/daemonset-install.yaml
kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/whereabouts/master/doc/crds/whereabouts.cni.cncf.io_ippools.yaml
kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/whereabouts/master/doc/crds/whereabouts.cni.cncf.io_overlappingrangeipreservations.yaml

echo "⏳ Waiting for system pods..."
kubectl wait --for=condition=Ready pods --all -n kube-system --timeout=180s

echo "📦 Starting VPN gateway (external BGP peer)..."
cd "${PROJECT_ROOT}/hack/vpn-gateway"
docker compose build
docker compose up -d
cd "${PROJECT_ROOT}"

echo ""
echo "✅ Kind cluster '${CLUSTER_NAME}' is ready!"
echo ""
echo "Next: ./test/e2e/scripts/build-and-deploy.sh"
