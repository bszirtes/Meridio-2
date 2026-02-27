#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-meridio-test}"
GATEWAY_API_VERSION="${GATEWAY_API_VERSION:-v1.4.1}"

echo "==> Creating Kind cluster: ${CLUSTER_NAME}"
cat <<EOF | kind create cluster --name "${CLUSTER_NAME}" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
- role: worker
- role: worker
networking:
  disableDefaultCNI: false
  podSubnet: "10.244.0.0/16"
EOF

echo "==> Installing Gateway API CRDs"
kubectl apply -f "https://github.com/kubernetes-sigs/gateway-api/releases/download/${GATEWAY_API_VERSION}/standard-install.yaml"

echo "==> Waiting for Gateway API CRDs to be established"
kubectl wait --for condition=established --timeout=60s \
  crd/gatewayclasses.gateway.networking.k8s.io \
  crd/gateways.gateway.networking.k8s.io

echo "==> Cluster ready!"
echo ""
echo "Next steps:"
echo "  1. Build images:"
echo "       make controller-manager BUILD_STEPS=build VERSION=test"
echo "       make stateless-load-balancer BUILD_STEPS=build VERSION=test"
echo "  2. Load into Kind:"
echo "       kind load docker-image controller-manager:test --name ${CLUSTER_NAME}"
echo "       kind load docker-image stateless-load-balancer:test --name ${CLUSTER_NAME}"
echo "  3. Install and deploy:"
echo "       make install"
echo "       make deploy IMG=controller-manager:test"
echo "       kubectl patch deployment meridio-2-controller-manager -n meridio-2 \\"
echo "         --type='json' -p='[{\"op\": \"replace\", \"path\": \"/spec/template/spec/containers/0/imagePullPolicy\", \"value\":\"IfNotPresent\"}]'"
echo "  4. Run tests: see test/e2e/QUICKSTART.md"
