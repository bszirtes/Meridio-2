# Gateway Controller

## Overview

The Gateway controller manages the lifecycle of Gateway API Gateway resources, creating and managing stateless load balancer (LB) Deployments for L3/L4 traffic handling on secondary networks.

## Architecture

### Core Concepts

**Gateway**: A Gateway API resource representing an L3/L4 load balancer instance. The controller creates a dedicated LB Deployment for each accepted Gateway.

**GatewayClass**: Determines which controller manages a Gateway. Only Gateways with a GatewayClass matching `spec.controllerName` are managed by this controller.

**GatewayConfiguration**: Implementation-specific parameters (replicas, resources, network attachments) referenced via `Gateway.spec.infrastructure.parametersRef`.

**LB Deployment**: A Kubernetes Deployment running load balancer and router containers. Named `sllb-<gateway-name>` and owned by the Gateway via ownerReference.

### Design Principles

- **No finalizers**: Uses ownerReferences only for automatic garbage collection
  - LB Deployments are pure Kubernetes resources (no external cleanup needed)
  - OwnerReferences ensure cleanup even if controller is unavailable
  - Simpler operational model: no manual intervention needed
- **Mixed filtering strategy**: 
  - GatewayClass-based filtering in mappers (avoid missing events)
  - Status condition checks in reconciliation (authoritative decision)
- **Idempotent reconciliation**: Safe to run multiple times
- **Template-based deployment**: Load LB Deployment from YAML template, customize per Gateway

### Resource Relationships

```
GatewayClass
└── spec.controllerName → matches controller

Gateway
├── spec.gatewayClassName → GatewayClass
├── spec.infrastructure.parametersRef → GatewayConfiguration
└── (owns) → LB Deployment (via ownerReference)

GatewayConfiguration
├── spec.networkAttachments → NAD references
├── spec.networkSubnets → secondary network CIDRs
├── spec.horizontalScaling → replicas, enforceReplicas
└── spec.verticalScaling → container resources

L34Route
├── spec.parentRefs → Gateway
└── spec.destinationCIDRs → VIP addresses

LB Deployment (owned by Gateway)
├── metadata.ownerReferences → Gateway (controller=true)
├── metadata.labels[gateway.networking.k8s.io/gateway-name] → Gateway name
└── spec.template.spec.serviceAccountName → stateless-load-balancer (injected by Kustomize)
```

**Ownership model:**
- Gateway owns LB Deployment via `metadata.ownerReferences` (controller=true)
- Enables automatic garbage collection when Gateway is deleted
- No finalizers needed (Kubernetes handles cleanup)

## Reconciliation Flow

### 1. Fetch Gateway
- Return early if not found (deleted)
- Skip reconciliation if being deleted (ownerReferences handle cleanup)

### 2. Check GatewayClass
- Fetch GatewayClass referenced by `spec.gatewayClassName`
- Compare `spec.controllerName` with controller's configured name
- Skip if not managed by this controller

### 3. Validate GatewayConfiguration
**TODO: Not yet implemented**
- Fetch GatewayConfiguration from `spec.infrastructure.parametersRef`
- Validate required fields (networkSubnets, etc.)
- Set `Accepted=False` if missing/invalid with reason `InvalidParameters`
- Only proceed if validation passes

### 4. Set Accepted Status
- Set `Accepted=True` with reason `Accepted`
- Message includes controller name for identification
- ObservedGeneration tracks which spec version was accepted

**Why check GatewayClass before setting Accepted:**
- Gateway API convention: `Accepted=True` means controller will manage this Gateway
- Prevents multiple controllers from accepting the same Gateway
- Allows users to transfer Gateway ownership by changing `gatewayClassName`

### 5. Update Addresses from L34Routes
- List all L34Routes in namespace (or cluster-wide if `--namespace` is empty)
- Filter routes referencing this Gateway in `spec.parentRefs`
- Extract unique destination CIDRs (VIP addresses)
- Update `status.addresses` with sorted IP addresses

**Why aggregate addresses from routes:**
- Gateway API pattern: Gateway status reflects actual VIPs served
- Enables clients to discover available VIPs without listing routes
- Sorted for deterministic output

### 6. Reconcile LB Deployment
- Load template from `<template-path>/lb-deployment.yaml`
- Customize for this Gateway:
  - Name: `sllb-<gateway-name>`
  - Namespace: same as Gateway
  - Labels: `app=sllb-<gateway-name>`, `gateway.networking.k8s.io/gateway-name=<gateway-name>`
  - ServiceAccount: from `--lb-service-account` flag (injected by Kustomize)
  - Selector: `app=sllb-<gateway-name>` (immutable)
  - Anti-affinity: updated to match deployment-specific labels
- Merge `spec.infrastructure.labels` and `spec.infrastructure.annotations`
- Set ownerReference to Gateway
- Create if not exists, update if changed (semantic equality check implemented)

**Implementation details:**
- Follows Meridio v1 "existing-as-base" pattern
- Single code path via `reconcileDeploymentSpec(base, template, ...)` where `base == nil` for create
- Uses `maps.Equal()` and `maps.Copy()` for map operations
- Preserves external labels/annotations while enforcing controller-managed fields
- Semantic equality check via `deploymentNeedsUpdate()` before calling `r.Update()`
- `SetControllerReference()` only called for new Deployments (prevents spurious updates)

**Conflict handling:**
- Transient errors (Conflict, AlreadyExists) → Requeue without setting `Programmed=False`
- Permanent errors (name collision, create failure) → Set `Programmed=False` + return error
- Follows Meridio v1 pattern: helper functions return wrapped errors, Reconcile decides requeue strategy

**Why template-based approach:**
- Flexibility: Users can customize LB container images, resources, etc.
- Separation of concerns: Controller logic vs deployment configuration
- Kustomize integration: Templates live in `config/templates/`, packaged as ConfigMap

**ServiceAccount name handling:**
- Template uses placeholder ServiceAccount name (`stateless-load-balancer`)
- Kustomize replacement injects actual name after `namePrefix` is applied
- Controller reads from `--lb-service-account` flag (bound to `MERIDIO_LB_SERVICE_ACCOUNT` env var)
- Works with any Kustomize configuration (no hardcoded prefixed names)

### 7. Set Programmed Status
- Set `Programmed=True` with reason `Programmed` after successful deployment reconciliation
- Set `Programmed=False` for permanent errors that prevent data plane configuration:
  - Name collision (Deployment owned by different Gateway)
  - Deployment creation failure
- Transient errors (API conflicts, network issues) don't alter `Programmed` status

### 8. Handle Ownership Transfer
- If Gateway's `gatewayClassName` changes to a different controller:
  - Reset `Accepted` to `Unknown` with reason `Pending` (best effort)
  - LB Deployment remains (new controller can delete/replace via ownerReference)
- Gateway API discourages changing `gatewayClassName` (edge case)

## Watch Triggers

The controller reconciles when:

| Resource | Trigger | Mapper Function | Filtering Strategy |
|----------|---------|-----------------|-------------------|
| Gateway | Create/Update/Delete | Direct (`.For()`) | None (all events) |
| Deployment | Create/Update/Delete | Owned (`.Owns()`) | ownerReference |
| GatewayClass | Create/Update/Delete | Matches controllerName | GatewayClass-based |
| L34Route | Create/Update/Delete | References Gateway in parentRefs | Status condition-based |
| GatewayConfiguration | Create/Update/Delete | Referenced by Gateway | GatewayClass-based |

### Filtering Strategy Rationale

**GatewayClass mapper:**
- Uses GatewayClass-based filtering (checks `spec.controllerName`)
- Avoids missing events when GatewayClass changes
- Reconciles all Gateways using the affected GatewayClass

**L34Route mapper:**
- Uses status condition-based filtering (checks `Accepted=True`)
- Avoids unnecessary reconciliation for unmanaged Gateways
- Routes only affect accepted Gateways (address aggregation)

**GatewayConfiguration mapper:**
- Uses GatewayClass-based filtering (checks `shouldManageGateway`)
- Avoids missing events when configuration changes
- Configuration affects acceptance decision (validation)

**Trade-off:**
- GatewayClass filtering: More reconciliations, but no missed events
- Status condition filtering: Fewer reconciliations, but requires Gateway to be accepted first
- Mixed approach balances correctness and efficiency

## Status Conditions

### Accepted Condition

**Type:** `Accepted`

**Status:** `True` | `False` | `Unknown`

**Reasons:**
- `Accepted`: Gateway is valid and will be managed by this controller
- `InvalidParameters`: GatewayConfiguration is missing or invalid (TODO)
- `Pending`: Waiting for controller (default for Unknown status)

**Lifecycle:**
1. Gateway created → `Unknown` (Pending) - waiting for controller
2. Controller validates → `True` (Accepted) - will manage this Gateway
3. GatewayConfiguration invalid → `False` (InvalidParameters) - won't create LB Deployment (TODO)
4. Gateway deleted → ownerReferences clean up LB Deployment

**ObservedGeneration:** Tracks which Gateway.spec version was evaluated

### Programmed Condition

**Type:** `Programmed`

**Status:** `True` | `False` | `Unknown`

**Reasons:**
- `Programmed` (status=True): LB Deployment reconciled successfully
- `Invalid` (status=False): Deployment creation failure or name collision
- `Pending` (status=Unknown): Waiting for LB Deployment reconciliation (default)

**Lifecycle:**
1. Gateway created → `Unknown` (Pending) - waiting for LB Deployment reconciliation
2. LB Deployment created → `True` (Programmed) - config sent to data plane (pods may still be initializing)
3. Permanent error (name collision, create failure) → `False` (Invalid) - cannot send config to data plane
4. Transient error (API conflict, network issue) → status unchanged - controller will retry

**Note:** `Programmed=True` indicates the LB Deployment resource was successfully reconciled, but does not guarantee the data plane is ready to process traffic. The LB pods run their own controllers (loadbalancer and router) that configure the data plane asynchronously.

**ObservedGeneration:** Tracks which Gateway.spec version was programmed

## Edge Cases

### Gateway Not Managed by This Controller
- Skip reconciliation (no status update)
- Avoids conflicts with other Gateway API implementations

### GatewayClass Not Found
- Skip reconciliation (treat as unmanaged)
- Allows users to delete GatewayClass without breaking Gateways

### GatewayConfiguration Missing
- TODO: Set `Accepted=False` with reason `InvalidParameters`
- Currently: Proceeds without validation (creates LB Deployment with defaults)

### Gateway Being Deleted
- Skip reconciliation (ownerReferences handle cleanup)
- LB Deployment deleted automatically by Kubernetes GC
- No finalizers needed

### Concurrent Reconciles
- Status update conflicts handled gracefully
- Automatic requeue with fresh resourceVersion
- Idiomatic Kubernetes pattern

### LB Deployment Modified Externally
- Detected via `.Owns()` watch
- Overwritten on next reconcile (controller owns the resource)
- Semantic equality check prevents unnecessary updates

### LB Deployment Name Collision
- If Deployment with desired name exists but owned by different Gateway → permanent error
- Error message includes actual owner for debugging
- Gateway status set to `Programmed=False` with reason `Invalid`
- User must resolve conflict (rename/delete conflicting Deployment or Gateway)

**Naming enforcement:**
- Controller uses strict name-based lookup: `sllb-<gateway-name>`
- No label-based fallback (ensures consistent naming for external tools like HPA)
- If Deployment is manually renamed, controller creates new Deployment with correct name
- Old Deployment remains orphaned (manual cleanup required)

### Ownership Transfer (gatewayClassName Changed)
- Old controller resets `Accepted` to `Unknown` (best effort)
- New controller can accept and manage Gateway
- LB Deployment remains (new controller decides whether to delete/replace)
- Gateway API discourages this scenario

## Configuration

### Controller Flags

**`--namespace`**: Limit to single namespace (empty = all namespaces)

**`--controller-name`**: 
- Used to match GatewayClass.spec.controllerName
- Default: `registry.nordix.org/cloud-native/meridio-2/gateway-controller`

**`--template-path`**:
- Path to template directory containing `lb-deployment.yaml`
- Default: `/templates`

**`--lb-service-account`**:
- ServiceAccount name for LB Deployment pods
- Default: `stateless-load-balancer`
- Injected by Kustomize replacement (handles namePrefix)

### Environment Variables
All flags can be set via `MERIDIO_*` environment variables (e.g., `MERIDIO_NAMESPACE`).

## Testing

### Unit Tests
- GatewayClass filtering (`shouldManageGateway`)
- Status condition updates (`updateAcceptedStatus`, `updateProgrammedStatus`)
- L34Route address aggregation (`updateAddressesFromRoutes`)
- Mapper functions (GatewayClass, L34Route, GatewayConfiguration)
- Deployment reconciliation (`deploymentNeedsUpdate`, `mergeMaps`, `reconcileLBDeployment`)
- Deployment customization (labels, anti-affinity, ServiceAccount)

### Manual Testing in Cluster

**Prerequisites:**
- Controller manager deployed
- Gateway API CRDs installed
- Namespace `meridio-2` exists

Deploy test resources:

```bash
cat <<'EOF' | kubectl apply -f -
---
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: meridio-2
spec:
  controllerName: registry.nordix.org/cloud-native/meridio-2/gateway-controller
---
apiVersion: meridio-2.nordix.org/v1alpha1
kind: GatewayConfiguration
metadata:
  name: test-gwconfig
  namespace: meridio-2
spec:
  networkAttachments: []
  networkSubnets:
    - attachmentType: NAD
      cidrs:
        - "192.168.100.0/24"
  horizontalScaling:
    replicas: 2
    enforceReplicas: false
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: test-gateway
  namespace: meridio-2
spec:
  gatewayClassName: meridio-2
  infrastructure:
    parametersRef:
      group: meridio-2.nordix.org
      kind: GatewayConfiguration
      name: test-gwconfig
  listeners:
    - name: default
      protocol: TCP
      port: 80
---
apiVersion: meridio-2.nordix.org/v1alpha1
kind: L34Route
metadata:
  name: test-route
  namespace: meridio-2
spec:
  parentRefs:
    - name: test-gateway
  backendRefs:
    - name: test-backend
      group: meridio-2.nordix.org
      kind: DistributionGroup
  destinationCIDRs:
    - "20.0.0.1/32"
    - "20.0.0.2/32"
  protocols:
    - TCP
  priority: 1
EOF
```

**Test 1: Verify Gateway Acceptance**

```bash
# Check Accepted condition
kubectl -n meridio-2 get gateway test-gateway -o jsonpath='{.status.conditions[?(@.type=="Accepted")]}'

# Expected: status=True, reason=Accepted, message contains controller name
```

**Test 2: Verify LB Deployment Creation**

```bash
# Check deployment exists
kubectl -n meridio-2 get deployment sllb-test-gateway

# Check Programmed condition
kubectl -n meridio-2 get gateway test-gateway -o jsonpath='{.status.conditions[?(@.type=="Programmed")]}'

# Expected: Programmed=True, reason=Programmed

# Verify ownerReference (controller=true)
kubectl -n meridio-2 get deployment sllb-test-gateway -o jsonpath='{.metadata.ownerReferences[0]}'

# Verify ServiceAccount name
kubectl -n meridio-2 get deployment sllb-test-gateway -o jsonpath='{.spec.template.spec.serviceAccountName}'
```

**Test 3: Verify Status Addresses from L34Routes**

```bash
# Check addresses (should show 20.0.0.1, 20.0.0.2)
kubectl -n meridio-2 get gateway test-gateway -o jsonpath='{.status.addresses[*].value}'

# Expected: 20.0.0.1 20.0.0.2 (sorted)
```

**Test 4: Verify Automatic Cleanup (No Finalizers)**

```bash
# Delete Gateway
kubectl delete gateway test-gateway -n meridio-2

# Verify Gateway deleted immediately (no Terminating state)
kubectl -n meridio-2 get gateway test-gateway 2>&1 | grep "NotFound"

# Wait for GC
sleep 5

# Verify Deployment auto-deleted via ownerReference
kubectl -n meridio-2 get deployment sllb-test-gateway 2>&1 | grep "NotFound"

# Expected: Both resources deleted, no manual cleanup needed
```

**Test 5: Ownership Transfer**

```bash
# Re-create Gateway
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: test-gateway
  namespace: meridio-2
spec:
  gatewayClassName: meridio-2
  infrastructure:
    parametersRef:
      group: meridio-2.nordix.org
      kind: GatewayConfiguration
      name: test-gwconfig
  listeners:
    - name: default
      protocol: TCP
      port: 80
EOF

# Wait for acceptance
sleep 2

# Create another GatewayClass
kubectl apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: other-controller
spec:
  controllerName: example.com/other-controller
EOF

# Change Gateway's gatewayClassName
kubectl -n meridio-2 patch gateway test-gateway --type=merge -p '{"spec":{"gatewayClassName":"other-controller"}}'

# Verify Accepted condition reset to Unknown
kubectl -n meridio-2 get gateway test-gateway -o jsonpath='{.status.conditions[?(@.type=="Accepted")]}'

# Expected: status=Unknown, reason=Pending
```

**Cleanup:**

```bash
kubectl delete gateway test-gateway -n meridio-2
kubectl delete l34route test-route -n meridio-2
kubectl delete gatewayconfiguration test-gwconfig -n meridio-2
kubectl delete gatewayclass meridio-2 other-controller
```

## Future Enhancements

### GatewayConfiguration Validation (HIGH PRIORITY)
- Validate required fields (networkSubnets, etc.)
- Set `Accepted=False` if invalid
- Clear error messages in status

### Apply GatewayConfiguration Values (HIGH PRIORITY)
- Network attachments: NAD configuration
- Horizontal scaling: replicas, enforceReplicas
- Vertical scaling: container resources

### Ready Condition Based on Deployment State (LOW PRIORITY)
- Implement optional `Ready` condition (Extended conformance)
- Set `Ready=True` only when Deployment is fully ready (all replicas available)
- Include replica counts in status message
- Note: `Programmed` already indicates config was sent to data plane

## References

- [Gateway API Specification](https://gateway-api.sigs.k8s.io/)
- [Gateway API GEP-1364: Status Conditions](https://gateway-api.sigs.k8s.io/geps/gep-1364/)
- [Kubernetes OwnerReferences](https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/)
