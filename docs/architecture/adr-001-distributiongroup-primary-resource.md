# Architectural Decision: DistributionGroup as Primary Resource

## Context

The LoadBalancer controller needs to manage NFQLB instances for load balancing traffic. The question is: what should be the primary resource that triggers reconciliation?

**Options**:
1. Watch **Gateway** as primary (initial implementation)
2. Watch **DistributionGroup** as primary (current implementation)

## Decision

**We watch DistributionGroup as the primary resource**, mirroring the Kubernetes Service/kube-proxy architectural pattern.

## Rationale

### Architectural Consistency with Kubernetes

The design mirrors how kube-proxy implements Service load balancing:

```
┌─────────────────────────────────┬──────────────────────────────────────┐
│ Kubernetes                      │ Meridio-2                            │
├─────────────────────────────────┼──────────────────────────────────────┤
│ Service (abstract LB)           │ DistributionGroup (abstract LB)      │
│ EndpointSlice (backends)        │ EndpointSlice (backends)             │
│ kube-proxy (per-node agent)     │ LB controller (per-Gateway agent)    │
│ Watches: Service (primary)      │ Watches: DistributionGroup (primary) │
│ Implements: iptables/ipvs       │ Implements: NFQLB (Maglev)           │
└─────────────────────────────────┴──────────────────────────────────────┘
```

**Key insight**: kube-proxy watches **Service** (not Node), even though it runs per-node. Similarly, our LoadBalancer controller watches **DistributionGroup** (not Gateway), even though it runs per-Gateway.

### Benefits

1. **Direct Mapping**: DistributionGroup → NFQLB instance (1:1 relationship)
   - Clear ownership and lifecycle management
   - NFQLB instance lifecycle directly tied to DistributionGroup

2. **Semantic Clarity**: DistributionGroup represents the load balancer
   - Like Service represents a load balancer in Kubernetes
   - Gateway represents infrastructure (like Node in Kubernetes)

3. **Focused Reconciliation**: Each DistributionGroup reconciles independently
   - Changes to one DistributionGroup don't trigger reconciliation of others
   - Better separation of concerns

4. **Architectural Consistency**: Follows established Kubernetes patterns
   - Easier to understand for Kubernetes developers
   - Predictable behavior

### Gateway Filtering

The controller filters DistributionGroups to only reconcile those belonging to its Gateway:

```go
func (c *Controller) belongsToGateway(distGroup) bool {
    // Check if any L34Route references both:
    // 1. This Gateway (via parentRefs)
    // 2. This DistributionGroup (via backendRefs)
    return found
}
```

This ensures:
- Each LoadBalancer controller only manages its Gateway's DistributionGroups
- Multiple Gateways can coexist without interference
- Clear ownership boundaries

## Consequences

### Positive

- **Architectural consistency**: Matches Kubernetes Service/kube-proxy model
- **Clear semantics**: DistributionGroup = load balancer definition
- **Direct lifecycle**: NFQLB instance lifecycle tied to DistributionGroup
- **Independent reconciliation**: Each DistributionGroup reconciles separately

### Negative

- **Multiple reconciliations**: N DistributionGroups = N reconcile loops (vs 1 Gateway reconcile)
- **Filtering overhead**: Must check L34Routes to determine Gateway ownership
- **Slightly more complex**: Requires `belongsToGateway()` filtering logic

### Mitigations

- **Performance**: Reconciliation is lightweight (NFQLB operations are fast)
- **Filtering**: Cached in controller-runtime, minimal overhead
- **Complexity**: Well-documented pattern (kube-proxy does the same)

## Alternatives Considered

### Alternative 1: Watch Gateway as Primary

**Pros**:
- Single reconciliation point per Gateway
- Simpler filtering (Gateway name from env var)
- Matches deployment model (1 pod per Gateway)

**Cons**:
- Indirect relationship (Gateway → L34Route → DistributionGroup)
- Not analogous to Service/kube-proxy model
- DistributionGroup changes trigger Gateway reconcile (extra hop)
- Less clear ownership (NFQLB lifecycle not directly tied to DistributionGroup)

**Rejected because**: Doesn't match Kubernetes architectural patterns

### Alternative 2: Watch Both Gateway and DistributionGroup

**Pros**:
- Could optimize for both patterns

**Cons**:
- Complexity: Two reconciliation paths
- Confusion: Which resource is "primary"?
- Maintenance burden: Two code paths to maintain

**Rejected because**: Unnecessary complexity

## References

- Kubernetes Service/kube-proxy architecture
- [Controller-runtime Best Practices](https://book.kubebuilder.io/reference/good-practices.html)
- POC implementation (watches Gateway, but different architecture)

## Status

**Accepted** - Implemented in `internal/controller/loadbalancer/controller.go`

## Date

2026-02-26
