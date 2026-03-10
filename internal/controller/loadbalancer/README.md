# LoadBalancer Controller

## Overview

The LoadBalancer controller manages NFQLB (nfqueue-loadbalancer) instances for traffic distribution within a Gateway. It watches DistributionGroup resources and creates corresponding NFQLB shared-memory instances.

## Architecture

### Deployment Model

**One SLLB Pod per Gateway**:
- Pod contains 2 containers: `stateless-load-balancer` + `router`
- The `stateless-load-balancer` container runs the LoadBalancer controller

### NFQLB Architecture

**One NFQLB process per container**:
- Started in `flowlb` mode: `nfqlb flowlb --queue=0:3`
- This single process manages **multiple shared-memory LB instances**

**One shared-memory instance per DistributionGroup**:
- Created via: `nfqlb init --shm=<distgroup-name> --M=<m> --N=<n>`
- Each instance is a **shared memory region** (not a separate process)
- All instances are managed by the single `nfqlb flowlb` process

### Detailed Flow

```
┌─────────────────────────────────────────────────────────────┐
│ SLLB Pod (per Gateway)                                      │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ stateless-load-balancer container                      │ │
│  │                                                         │ │
│  │  ONE nfqlb process (flowlb mode)                       │ │
│  │  ├─ Listens on nfqueues 0-3                           │ │
│  │  └─ Manages multiple shared-memory instances:         │ │
│  │                                                         │ │
│  │     DistributionGroup "web-backends"                   │ │
│  │     ├─ Shared memory: /dev/shm/web-backends           │ │
│  │     ├─ Maglev table: M=3200, N=32                     │ │
│  │     └─ Targets: [10.0.1.1, 10.0.1.2, ...]            │ │
│  │                                                         │ │
│  │     DistributionGroup "api-backends"                   │ │
│  │     ├─ Shared memory: /dev/shm/api-backends           │ │
│  │     ├─ Maglev table: M=6400, N=64                     │ │
│  │     └─ Targets: [10.0.2.1, 10.0.2.2, ...]            │ │
│  │                                                         │ │
│  │  LoadBalancer Controller                               │ │
│  │  └─ Watches DistributionGroups                        │ │
│  │     └─ Creates/deletes shared-memory instances        │ │
│  └────────────────────────────────────────────────────────┘ │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ router container (Bird2/FRR)                           │ │
│  └────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

## Key Points

1. **One NFQLB process** per SLLB container (started at container startup)
2. **Multiple shared-memory instances** within that process (one per DistributionGroup)
3. **1:1 mapping**: Each DistributionGroup gets its own shared-memory LB instance
4. **Lifecycle**: 
   - Process: Lives for entire container lifetime
   - Instances: Created/deleted as DistributionGroups are added/removed

## Design Decision: DistributionGroup as Primary Resource

The controller watches **DistributionGroup** as the primary resource, mirroring the Kubernetes Service/kube-proxy architectural pattern.

### Architectural Consistency

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

### Gateway Filtering

The controller filters DistributionGroups to only reconcile those belonging to its Gateway by checking if any L34Route references both:
1. This Gateway (via `parentRefs`)
2. This DistributionGroup (via `backendRefs`)

This ensures:
- Each LoadBalancer controller only manages its Gateway's DistributionGroups
- Multiple Gateways can coexist without interference
- Clear ownership boundaries

## Reconciliation Logic

### 1. NFQLB Instance Management

For each DistributionGroup:
- Creates shared-memory instance with Maglev parameters:
  - **M** (table size): `MaxEndpoints × 100`
  - **N** (max endpoints): From `DistributionGroup.Spec.Maglev.MaxEndpoints` (default: 32)
- Tracks instances in memory map
- Deletes instances when DistributionGroup is removed

### 2. Target Management

For each DistributionGroup:
- Watches EndpointSlices (labeled with `kubernetes.io/service-name: <distgroup-name>`)
- Extracts target identifiers from `endpoint.Zone` field
- Filters by `endpoint.Conditions.Ready == true`
- Activates new targets: `instance.Activate(identifier, identifier)`
- Deactivates removed targets: `instance.Deactivate(identifier)`

### 3. Flow Configuration

- Configure NFQLB flows from L34Routes
- Map VIPs, protocols, ports, and priorities
- Flows define traffic classification rules for load balancing

### 4. nftables Rules

- Single shared nftables table (`meridio-lb`) for all DistributionGroups
- VIPs extracted from Gateway.status.addresses
- Queue traffic matching VIPs to nfqueue for NFQLB processing
- Prevents packet re-injection with overlapping VIPs across DGs

### 5. Readiness Signaling

- Write readiness files: `/var/run/meridio/lb-ready-<distgroup>`
- Created only when DistributionGroup has ready endpoints
- Router container reads these to decide VIP advertisement
- Cleanup on DistributionGroup deletion or endpoint unavailability

## References

- [ADR-001: DistributionGroup as Primary Resource](../../docs/architecture/adr-001-distributiongroup-primary-resource.md)
- [NFQLB Documentation](https://github.com/Nordix/nfqueue-loadbalancer)
