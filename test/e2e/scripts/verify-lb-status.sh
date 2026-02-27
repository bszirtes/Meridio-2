#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${NAMESPACE:-default}"
LB_POD=""

echo "==> Finding LoadBalancer pod"
LB_POD=$(kubectl get pods -n "${NAMESPACE}" -l app=stateless-load-balancer -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")

if [ -z "$LB_POD" ]; then
  echo "ERROR: No LoadBalancer pod found in namespace ${NAMESPACE}"
  echo "Make sure the LoadBalancer controller is deployed"
  exit 1
fi

echo "Found LoadBalancer pod: ${LB_POD}"
echo ""

echo "==> Checking NFQLB process"
kubectl exec -n "${NAMESPACE}" "${LB_POD}" -- \
  ps aux | grep -E "nfqlb|PID" | grep -v grep || echo "NFQLB process not found!"
echo ""

echo "==> Listing shared memory instances"
kubectl exec -n "${NAMESPACE}" "${LB_POD}" -- \
  ls -lh /dev/shm/ 2>/dev/null || echo "No shared memory instances found"
echo ""

echo "==> Recent controller logs (last 20 lines)"
kubectl logs -n "${NAMESPACE}" "${LB_POD}" --tail=20
