# Manual Testing Summary - 2026-02-27

## What Was Tested

Successfully deployed and tested the stateless-load-balancer component in a kind cluster.

## Key Findings

### ✅ Working Components

1. **Image Build with Capabilities**
   - Fixed Dockerfile to include `setcap` for required capabilities:
     - `cap_net_admin+ep` for stateless-load-balancer binary
     - `cap_net_admin,cap_ipc_lock,cap_ipc_owner+ep` for nfqlb binary
     - `cap_net_admin+ep` for nft binary
   - These capabilities eliminate the need for `privileged: true` in pod security context
   - Without these capabilities, nftables operations fail with "operation not permitted"

2. **LoadBalancer Pod Deployment**
   - Pod runs successfully without `privileged: true` (capabilities set via setcap)
   - Init container sets required sysctls (requires privileged mode for sysctl)
   - NFQLB process starts correctly
   - nftables table and chain created successfully

3. **Controller Reconciliation**
   - Watches DistributionGroup and EndpointSlice resources
   - Detects and reconciles targets from EndpointSlice.Zone field
   - Logs show proper reconciliation: "Reconciled targets count: 3"

### ❌ Known Issues

1. **Gateway Controller Not Implemented**
   - Gateway controller exists but is a stub (TODO comment)
   - LoadBalancer deployment must be created manually
   - No automatic LB pod creation when Gateway is deployed

2. **NFQLB Shared Memory Creation Fails**
   - Controller logs "Created NFQLB instance" but shared memory file not created
   - Expected file: `/dev/shm/tshm-web-backends`
   - Actual files: only `ftshm` and `nfqlb-trace`
   - Target activation fails: "FAILED mapSharedData: tshm-web-backends"
   - Root cause: NFQLB instance creation code issue (needs investigation)

3. **Controller-Manager Image Pull**
   - `make deploy` doesn't properly set imagePullPolicy for kind
   - Requires manual patch: `kubectl patch deployment ... imagePullPolicy=IfNotPresent`

## Test Environment

- **Cluster**: kind v1.32.2 (3 nodes: 1 control-plane, 2 workers)
- **Images**: 
  - `controller-manager:test`
  - `stateless-load-balancer:test`
- **Namespace**: default
- **Resources**:
  - Gateway: test-gateway
  - DistributionGroup: web-backends (Maglev M=3200, N=32)
  - L34Route: web-route (VIP 20.0.0.1/32, port 80, TCP)
  - EndpointSlice: 3 endpoints with Zone identifiers 5000, 5001, 5002

## Files Updated

1. **build/stateless-load-balancer/Dockerfile**
   - Added `libcap` package
   - Added `setcap` commands for capabilities

2. **test/e2e/QUICKSTART.md**
   - Updated with correct build commands
   - Added imagePullPolicy patch step
   - Documented known issues

3. **test/e2e/scripts/setup-kind-cluster.sh**
   - Updated next steps with correct commands

4. **test/e2e/scripts/verify-lb-status.sh**
   - Fixed default namespace (default instead of meridio-system)
   - Removed container name (single container pod)

5. **test/e2e/testdata/scenario-1-basic/loadbalancer.yaml** (new)
   - Manual LoadBalancer deployment
   - ServiceAccount + RBAC
   - Deployment with sysctl init container
   - Privileged security context

6. **test/e2e/testdata/scenario-1-basic/resources.yaml**
   - Fixed GatewayClass controllerName to match controller config

## Next Steps

1. **Implement Gateway Controller**
   - Create LoadBalancer deployment when Gateway is created
   - Set owner references for garbage collection
   - Update Gateway status conditions

2. **Fix NFQLB Shared Memory Creation**
   - Debug why `tshm-<name>` files aren't created
   - Check NFQLB library integration
   - Verify shared memory permissions

3. **Improve Deployment**
   - Fix imagePullPolicy in kustomize config
   - Add proper health checks
   - Add readiness file writing

## Testing Instructions

The test framework is ready to use. Follow [test/e2e/QUICKSTART.md](test/e2e/QUICKSTART.md) for step-by-step instructions.

**Quick verification:**
```bash
# After setup
./test/e2e/scripts/verify-lb-status.sh
```

Expected output shows:
- NFQLB process running
- nftables initialized
- Controller logs (may show shared memory errors)
