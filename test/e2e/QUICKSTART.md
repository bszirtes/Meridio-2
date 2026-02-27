# Quick Testing Guide

## Setup (10 minutes)

### 1. Create Kind Cluster
```bash
./test/e2e/scripts/setup-kind-cluster.sh
```

### 2. Build and Load Images

**Important:** The stateless-load-balancer requires specific capabilities (`cap_net_admin`, `cap_ipc_lock`, `cap_ipc_owner`) which are set via `setcap` in the Dockerfile. This eliminates the need for `privileged: true` in the pod security context.

```bash
# Build controller-manager image
make controller-manager BUILD_STEPS=build VERSION=test

# Build stateless-load-balancer image (with capabilities)
make stateless-load-balancer BUILD_STEPS=build VERSION=test

# Load images into Kind cluster
kind load docker-image controller-manager:test --name meridio-test
kind load docker-image stateless-load-balancer:test --name meridio-test
```

### 3. Install CRDs and Deploy Operator
```bash
# Install CRDs
make install

# Deploy controller-manager (note: Gateway controller is not yet implemented)
make deploy IMG=controller-manager:test

# Fix image pull policy for kind
kubectl patch deployment meridio-2-controller-manager -n meridio-2 \
  --type='json' -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/imagePullPolicy", "value":"IfNotPresent"}]'

# Wait for operator ready
kubectl wait --for=condition=Available --timeout=60s -n meridio-2 deployment/meridio-2-controller-manager
```

## Run Basic Test

### Deploy Test Resources
```bash
# Deploy Gateway, DistributionGroup, L34Route
kubectl apply -f test/e2e/testdata/scenario-1-basic/resources.yaml

# Deploy LoadBalancer manually (Gateway controller not implemented yet)
kubectl apply -f test/e2e/testdata/scenario-1-basic/loadbalancer.yaml

# Wait for LoadBalancer pod
kubectl wait --for=condition=Ready pod -l app=stateless-load-balancer --timeout=60s
```

### Verify LoadBalancer Status
```bash
# Check pod is running
kubectl get pods -l app=stateless-load-balancer

# Check NFQLB process
kubectl exec -l app=stateless-load-balancer -- ps aux | grep nfqlb

# Check nftables initialized
kubectl exec -l app=stateless-load-balancer -- nft list tables

# Check logs
kubectl logs -l app=stateless-load-balancer --tail=20
```

## Test with Targets

```bash
# Add EndpointSlice with 3 targets
kubectl apply -f test/e2e/testdata/scenario-2-targets/

# Check reconciliation
kubectl logs -l app=stateless-load-balancer --tail=20 | grep "Reconciled targets"
```

**Expected:** `"count": 3` in logs

## Known Issues

1. **Gateway Controller Not Implemented** - LoadBalancer deployment must be created manually
2. **NFQLB Shared Memory** - Shared memory file creation may fail (under investigation)
3. **Kind Limitations** - Requires capabilities set via `setcap` in Dockerfile

## Cleanup

```bash
kubectl delete -f test/e2e/testdata/scenario-*/
kind delete cluster --name meridio-test
```

## Full Testing Guide

See [TESTING-LOADBALANCER.md](TESTING-LOADBALANCER.md) for detailed test scenarios.
