# Testing LoadBalancer Controller in Kind

This guide provides step-by-step instructions to verify the LoadBalancer controller functionality in a local Kind cluster.

## Prerequisites

- Docker
- Kind (Kubernetes in Docker)
- kubectl
- make

**Important**: Meridio-2 uses the API group `meridio-2.nordix.org/v1alpha1`, not `meridio.nordix.org`.

## Test Environment Setup

### 1. Create Kind Cluster

```bash
./test/e2e/scripts/setup-kind-cluster.sh
```

This creates a Kind cluster with:
- Gateway API CRDs installed
- Multus CNI (for secondary networks)
- Whereabouts IPAM

### 2. Build and Load Images

```bash
# Build the controller-manager image with 'test' tag
make controller-manager BUILD_STEPS=build VERSION=test

# Build the stateless-load-balancer image with 'test' tag
make stateless-load-balancer BUILD_STEPS=build VERSION=test

# Load images into Kind cluster
kind load docker-image controller-manager:test --name meridio-test
kind load docker-image stateless-load-balancer:test --name meridio-test
```

### 3. Deploy Meridio-2 Operator

```bash
# Install CRDs
make install

# Deploy controller manager
make deploy IMG=controller:test
```

## Test Scenarios

### Scenario 1: Basic NFQLB Instance Creation

**Objective**: Verify that a DistributionGroup creates an NFQLB shared-memory instance.

```bash
# Apply test resources
kubectl apply -f test/e2e/testdata/scenario-1-basic/

# Expected resources:
# - GatewayClass
# - Gateway
# - DistributionGroup
# - L34Route (linking Gateway → DistributionGroup)
```

**Verification**:

```bash
# 1. Check LoadBalancer pod is running
kubectl get pods -n meridio-system -l app=stateless-load-balancer

# 2. Check NFQLB process is running
kubectl exec -n meridio-system <lb-pod> -c stateless-load-balancer -- \
  ps aux | grep nfqlb

# 3. Verify NFQLB instance exists
kubectl exec -n meridio-system <lb-pod> -c stateless-load-balancer -- \
  nfqlb show --shm=tshm-<distgroup-name>

# Expected output:
# Maglev: M=3200, N=32
# Targets: 0
```

**Success Criteria**:
- ✅ LoadBalancer pod running
- ✅ NFQLB process in `flowlb` mode
- ✅ Shared-memory instance created with correct M/N parameters

### Scenario 2: Target Activation

**Objective**: Verify that EndpointSlices with Maglev identifiers activate targets in NFQLB.

**Note**: EndpointSlices are created by the DistributionGroup controller with Zone field format `"maglev:N"` where N is the Maglev identifier (0-31 by default).

```bash
# Apply test resources
kubectl apply -f test/e2e/testdata/scenario-2-targets/

# Expected resources:
# - DistributionGroup
# - EndpointSlice with 3 endpoints (Zone field = "maglev:N")
```

**Verification**:

```bash
# 1. Check EndpointSlice has Maglev identifiers
kubectl get endpointslice <name> -o jsonpath='{.endpoints[*].zone}'
# Expected: maglev:0 maglev:1 maglev:2

# 2. Verify targets are activated in NFQLB
kubectl exec -n meridio-system <lb-pod> -c stateless-load-balancer -- \
  nfqlb show --shm=tshm-<distgroup-name>

# Expected output:
# Targets: 3
# 0: 10.244.1.10
# 1: 10.244.1.11
# 2: 10.244.1.12
```

**Success Criteria**:
- ✅ 3 targets activated in NFQLB
- ✅ Identifiers (0, 1, 2) extracted from Zone values ("maglev:0", "maglev:1", "maglev:2")
- ✅ IP addresses match EndpointSlice addresses

### Scenario 3: Target Deactivation

**Objective**: Verify that removing endpoints deactivates targets.

```bash
# Remove one endpoint from EndpointSlice
kubectl patch endpointslice <name> --type=json \
  -p='[{"op": "remove", "path": "/endpoints/2"}]'

# Wait for reconciliation
sleep 5
```

**Verification**:

```bash
# Verify target count decreased
kubectl exec -n meridio-system <lb-pod> -c stateless-load-balancer -- \
  nfqlb show --shm=tshm-<distgroup-name>

# Expected output:
# Targets: 2
```

**Success Criteria**:
- ✅ Target count reduced to 2
- ✅ Removed target no longer in NFQLB

### Scenario 4: Multiple DistributionGroups

**Objective**: Verify multiple DistributionGroups create separate NFQLB instances.

```bash
# Apply test resources
kubectl apply -f test/e2e/testdata/scenario-4-multiple/

# Expected resources:
# - 2 DistributionGroups (web-backends, api-backends)
# - 2 L34Routes
```

**Verification**:

```bash
# List all NFQLB instances
kubectl exec -n meridio-system <lb-pod> -c stateless-load-balancer -- \
  ls -la /dev/shm/

# Expected output:
# web-backends
# api-backends

# Verify each instance
for dg in web-backends api-backends; do
  echo "=== $dg ==="
  kubectl exec -n meridio-system <lb-pod> -c stateless-load-balancer -- \
    nfqlb show --shm=tshm-$dg
done
```

**Success Criteria**:
- ✅ 2 separate shared-memory instances
- ✅ Each instance has correct M/N parameters
- ✅ Instances are independent

### Scenario 5: DistributionGroup Deletion

**Objective**: Verify that deleting a DistributionGroup cleans up NFQLB instance.

```bash
# Delete DistributionGroup
kubectl delete distributiongroup web-backends

# Wait for reconciliation
sleep 5
```

**Verification**:

```bash
# Verify instance is removed
kubectl exec -n meridio-system <lb-pod> -c stateless-load-balancer -- \
  ls -la /dev/shm/ | grep web-backends

# Expected: No output (instance deleted)

# Verify other instance still exists
kubectl exec -n meridio-system <lb-pod> -c stateless-load-balancer -- \
  nfqlb show --shm=tshm-api-backends

# Expected: Still shows api-backends instance
```

**Success Criteria**:
- ✅ Deleted DistributionGroup's instance removed
- ✅ Other instances unaffected

### Scenario 6: Gateway Filtering

**Objective**: Verify that LoadBalancer controller only manages DistributionGroups for its Gateway.

```bash
# Apply test resources
kubectl apply -f test/e2e/testdata/scenario-6-filtering/

# Expected resources:
# - 2 Gateways (gateway-a, gateway-b)
# - 2 DistributionGroups (dg-a, dg-b)
# - 2 L34Routes (linking gateway-a→dg-a, gateway-b→dg-b)
```

**Verification**:

```bash
# Check gateway-a's LoadBalancer pod
kubectl exec -n meridio-system gateway-a-lb-xxx -c stateless-load-balancer -- \
  ls /dev/shm/

# Expected: Only dg-a

# Check gateway-b's LoadBalancer pod
kubectl exec -n meridio-system gateway-b-lb-xxx -c stateless-load-balancer -- \
  ls /dev/shm/

# Expected: Only dg-b
```

**Success Criteria**:
- ✅ Each LoadBalancer pod only manages its Gateway's DistributionGroups
- ✅ No cross-Gateway interference

## Debugging

### Check Controller Logs

```bash
# LoadBalancer controller logs
kubectl logs -n meridio-system <lb-pod> -c stateless-load-balancer -f

# Look for:
# - "Created NFQLB instance"
# - "Activated target"
# - "Deactivated target"
# - "Deleting NFQLB instance"
```

### Check NFQLB Process

```bash
# Verify NFQLB process is running
kubectl exec -n meridio-system <lb-pod> -c stateless-load-balancer -- \
  ps aux | grep nfqlb

# Expected:
# nfqlb flowlb --queue=0:3 --promiscuous_ping
```

### Check Shared Memory

```bash
# List all shared memory instances
kubectl exec -n meridio-system <lb-pod> -c stateless-load-balancer -- \
  ls -lh /dev/shm/

# Check instance details
kubectl exec -n meridio-system <lb-pod> -c stateless-load-balancer -- \
  nfqlb show --shm=tshm-<distgroup-name>
```

### Check nftables Rules (TODO)

```bash
# List nftables rules
kubectl exec -n meridio-system <lb-pod> -c stateless-load-balancer -- \
  nft list table inet meridio
```

## Cleanup

```bash
# Delete test resources
kubectl delete -f test/e2e/testdata/scenario-*/

# Delete cluster
kind delete cluster --name meridio-test
```

## Known Limitations (Current Implementation)

- ✅ NFQLB instance creation/deletion
- ✅ Target activation/deactivation
- ✅ Gateway filtering
- ⏳ Flow configuration (TODO)
- ⏳ nftables VIP rules (TODO)
- ⏳ ICMP rules (TODO)
- ⏳ Readiness files (TODO)

## Troubleshooting

### Issue: "no matches for kind DistributionGroup in version meridio.nordix.org/v1alpha1"

**Symptoms**: Error when applying test manifests

**Cause**: Using wrong API group. Meridio-2 uses `meridio-2.nordix.org`, not `meridio.nordix.org`

**Fix**: Update manifests to use correct API group:
```yaml
apiVersion: meridio-2.nordix.org/v1alpha1
kind: DistributionGroup
```

### Issue: NFQLB process not starting

**Symptoms**: LoadBalancer pod crashes or restarts

**Check**:
```bash
kubectl logs -n meridio-system <lb-pod> -c stateless-load-balancer --previous
```

**Common causes**:
- Missing NET_ADMIN capability
- nfqueue kernel module not loaded
- Conflicting nfqueue usage

### Issue: Targets not activating

**Symptoms**: `nfqlb show` shows 0 targets

**Check**:
1. EndpointSlice has `Zone` field set
2. EndpointSlice has `Conditions.Ready = true`
3. EndpointSlice labeled with `meridio-2.nordix.org/distributiongroup: <distgroup-name>`

**Debug**:
```bash
kubectl get endpointslice <name> -o yaml
```

### Issue: DistributionGroup not reconciled

**Symptoms**: No NFQLB instance created

**Check**:
1. L34Route exists linking Gateway → DistributionGroup
2. L34Route `parentRefs` matches Gateway name
3. L34Route `backendRefs` matches DistributionGroup name

**Debug**:
```bash
kubectl get l34route <name> -o yaml
kubectl logs -n meridio-system <lb-pod> -c stateless-load-balancer | grep "does not belong"
```
