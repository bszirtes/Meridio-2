#!/usr/bin/env bash
# Build all images and deploy Meridio-2 into Kind cluster.
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-meridio-e2e}"
VERSION="${VERSION:-latest}"
REGISTRY="${REGISTRY:-localhost:5001}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"

cd "${PROJECT_ROOT}"

echo "=== Building and deploying Meridio-2 ==="

kubectl config use-context "kind-${CLUSTER_NAME}"

echo "📦 Building images..."
for img in controller-manager stateless-load-balancer router network-sidecar; do
    echo "  Building ${img}..."
    make "${img}" BUILD_STEPS="build tag" VERSION="${VERSION}" REGISTRY="${REGISTRY}" 2>&1 | tail -1
done

echo "📤 Loading images into Kind..."
for img in controller-manager stateless-load-balancer router network-sidecar; do
    kind load docker-image "${REGISTRY}/${img}:${VERSION}" --name "${CLUSTER_NAME}"
done

echo "📦 Installing CRDs..."
make install

echo "📦 Installing cert-manager..."
make cert-manager

echo "📦 Deploying controller-manager..."
make deploy REGISTRY="${REGISTRY}" VERSION_CONTROLLER_MANAGER="${VERSION}"

echo "🔧 Patching imagePullPolicy for Kind..."
kubectl patch deployment meridio-2-controller-manager \
  -n meridio-2 \
  --type=json \
  -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/imagePullPolicy", "value": "IfNotPresent"}]'

echo "⏳ Waiting for controller-manager..."
kubectl wait --for=condition=Available --timeout=180s \
  -n meridio-2 deployment/meridio-2-controller-manager

echo "📦 Applying common cluster-scoped resources..."
kubectl apply -f test/e2e/testdata/common/

echo ""
echo "✅ Meridio-2 deployed!"
echo ""
echo "Next: go test ./test/e2e/ -tags e2e -v"
