# LoadBalancer Controller

## Overview

The LoadBalancer controller manages NFQLB (nfqueue-loadbalancer) instances for traffic distribution within a Gateway. It watches DistributionGroup resources and creates corresponding NFQLB shared-memory instances, configures policy routing, nftables rules, and readiness signaling.

## Architecture

### Deployment Model

**One SLLB Pod per Gateway**:
- Pod contains 2 containers: `stateless-load-balancer` + `router`
- The `stateless-load-balancer` container runs the LoadBalancer controller
- The `router` container runs Bird3 for BGP/routing protocol advertisement

### NFQLB Architecture

**One NFQLB process per container**:
- Started in `flowlb` mode: `nfqlb flowlb --queue=0:3`
- This single process manages **multiple shared-memory LB instances**

**One shared-memory instance per DistributionGroup**:
- Created via: `nfqlb init --shm=tshm-<distgroup-name> --M=<m> --N=<n>`
- Each instance is a **shared memory region** (not a separate process)
- All instances are managed by the single `nfqlb flowlb` process

### Detailed Flow

```
                          ┌──────────────────────────────────────────────────────────┐
                          │ SLLB Pod (per Gateway)                                   │
                          │                                                          │
                          │  ┌────────────────────────────────────────────────────┐  │
  Incoming VIP traffic    │  │ stateless-load-balancer container                  │  │
  ──────────────────────► │  │                                                     │  │
                          │  │  nftables (prerouting)                             │  │
                          │  │  ├─ Match VIP → queue to nfqueue 0-3              │  │
                          │  │  │                                                 │  │
                          │  │  ▼                                                 │  │
                          │  │  nfqlb flowlb (single process)                    │  │
                          │  │  ├─ Reads shared memory instances                  │  │
                          │  │  ├─ Maglev hash → selects target                  │  │
                          │  │  └─ Sets fwmark on packet                         │  │
                          │  │                                                     │  │
                          │  │  Policy routing (ip rule / ip route)               │  │
                          │  │  ├─ fwmark 5000 → table 5000 → via target-1       │  │
                          │  │  ├─ fwmark 5001 → table 5001 → via target-2  ─────┼──┼──► net1 (macvlan)
                          │  │  └─ fwmark 5002 → table 5002 → via target-3       │  │    to targets
                          │  │                                                     │  │
                          │  │  LoadBalancer Controller                           │  │
                          │  │  ├─ Watches DistributionGroups, L34Routes,         │  │
                          │  │  │  EndpointSlices, Gateways                       │  │
                          │  │  ├─ Creates/deletes shared-memory instances        │  │
                          │  │  ├─ Configures nftables VIP rules                 │  │
                          │  │  ├─ Configures policy routing per target           │  │
                          │  │  └─ Writes readiness files                        │  │
                          │  └────────────────────────────────────────────────────┘  │
                          │                                                          │
                          │  ┌────────────────────────────────────────────────────┐  │
                          │  │ router container (Bird3)                           │  │
                          │  │  ├─ Reads readiness files from shared volume       │  │
                          │  │  └─ Advertises VIPs via BGP when ready             │  │
                          │  └────────────────────────────────────────────────────┘  │
                          └──────────────────────────────────────────────────────────┘
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
  - **M** (table size): Nearest prime to `MaxEndpoints × 100`
  - **N** (max endpoints): From `DistributionGroup.Spec.Maglev.MaxEndpoints` (default: 32)
- Tracks instances in memory map
- Deletes instances when DistributionGroup is removed

### 2. Target Management

For each DistributionGroup:
- Watches EndpointSlices labeled with `meridio-2.nordix.org/distribution-group: <distgroup-name>`
- EndpointSlices must have an **OwnerReference** to the DistributionGroup (set by the DistributionGroup controller) to trigger reconciliation on updates
- Extracts target identifiers from `endpoint.Zone` field (format: `maglev:<N>`)
- Filters by `endpoint.Conditions.Ready == true`
- Activates new targets: `instance.Activate(identifier, fwmark)`
- Deactivates removed targets: `instance.Deactivate(identifier)`

### 3. Policy Routing

For each activated target:
- Configures routing **before** activating the target to prevent traffic loss
- Creates `ip rule`: fwmark → routing table
- Creates `ip route`: default via target IP in routing table
- Kernel determines the outgoing interface based on target IP subnet
- Cleans up routes on target deactivation
- Cleans stale ARP/NDP entries on route changes

### 4. Flow Configuration

- Configures NFQLB flows from L34Routes
- Maps VIPs, protocols, ports, and priorities
- Flows define traffic classification rules for load balancing
- Flows are only configured when there are ready endpoints

### 5. nftables Rules

- Creates nftables table with VIP sets (IPv4 and IPv6)
- Prerouting chain: queues VIP-destined traffic to nfqueue for NFQLB processing
- Output chain: queues locally-originated ICMP to VIPs (for ping responses)
- VIPs extracted from L34Route `destinationCIDRs`

### 6. Readiness Signaling

- Writes readiness files: `/var/run/meridio/lb-ready-<distgroup>`
- Created only when DistributionGroup has ready endpoints
- Removed when DistributionGroup is deleted or has no ready endpoints
- Router container (Bird3) reads these to decide VIP advertisement via BGP

## References

- [ADR-001: DistributionGroup as Primary Resource](../../docs/architecture/adr-001-distributiongroup-primary-resource.md)
- [NFQLB Documentation](https://github.com/Nordix/nfqueue-loadbalancer)
