# Meridio-2 MVP - TODO Breakdown

**MVP Definition**: First drop that can be tried out and handles main use cases for early feedback.

**Reference**: 
- [Gist Plan](https://gist.github.com/zolug/84852f94d448987658607a06686d1163)
- [Issue #6](https://github.com/Nordix/Meridio-2/issues/6)
- [POC Multi-Tenancy](https://github.com/LionelJouin/meridio-2.x/tree/main/docs#multi-tenancy)

**Current State**: CRDs defined (DistributionGroup, EndpointNetworkConfiguration, GatewayConfig, GatewayRouter, L34Route). L34Route webhook implemented. No controllers yet.

**Key Implementation Notes from Gist**:
- Controller Manager initially **without leader election**
- Gateway controller **MUST watch GatewayClass** but **MUST NOT update GatewayClass.status**
- Router container uses **readiness gates** for external connectivity status
- EndpointNetworkConfiguration controller **MUST filter next-hops by Router readiness gates**
- LB readiness signaling: start with **dummy file**, later integrate with `WatchesRawSource`
- Sidecar cleanup: can use **nftables sets** to store old VIP state (advanced, post-MVP)

---

## Architecture Overview

### CRDs (Implemented)
- **Gateway** (Gateway API): Load balancer infrastructure
- **GatewayRouter**: BGP/BFD configuration for external connectivity
- **GatewayConfig**: Deployment configuration (replicas, resources, network attachments)
- **L34Route**: Layer 3/4 traffic classification rules
- **DistributionGroup**: Backend endpoint grouping with Maglev hashing
- **EndpointNetworkConfiguration**: Per-pod network configuration (VIPs, next-hops)

### Components to Build
1. **Gateway Controller**: Reconciles Gateway → creates LB Deployment
2. **DistributionGroup Controller**: Creates EndpointSlices from Pod secondary IPs
3. **EndpointNetworkConfiguration Controller**: Creates per-pod network config CRs
4. **LoadBalancer Container** (in LB Pod): Manages NFQLB instances and nftables
5. **Router Container** (in LB Pod): Manages BGP/BFD via Bird2/FRR
6. **Application Sidecar**: Applies VIPs and source-based routing

---

## 1. Gateway Controller

**Purpose**: Reconcile Gateway resources to create/manage LB Deployments.

**Important**: Controller Manager should initially be implemented **without leader election** for simplicity.

### 1.1 Basic Reconciliation
- [ ] Watch Gateway resources with `gatewayClassName: meridio.nordix.org/stateless-load-balancer`
- [ ] **Watch GatewayClass objects** to determine if controller should manage a Gateway
  - Only reconcile Gateways whose GatewayClass.spec.controllerName matches this controller
  - **MUST NOT update GatewayClass.status** (read-only for this controller)
- [ ] Extract GatewayConfig reference from Gateway annotations/labels
- [ ] Create Deployment with 2 containers (LoadBalancer + Router)
- [ ] Set owner reference (Gateway → Deployment)
- [ ] Handle Gateway deletion (finalizer for cleanup)

### 1.2 Deployment Configuration
- [ ] Apply GatewayConfig.spec.horizontalScaling.replicas to Deployment
- [ ] Apply GatewayConfig.spec.verticalScaling.containers resources
- [ ] Apply GatewayConfig.spec.networkAttachments as Multus annotations
- [ ] Generate unique Deployment name (e.g., `<gateway-name>-lb`)
- [ ] Add labels for discovery (e.g., `gateway.meridio.nordix.org/name: <gateway-name>`)
- [ ] **LB template management**: Refer to Meridio's approach for managing Deployment templates
  - Annotations and labels must be adjusted upon changes
  - Template should be configurable (e.g., via ConfigMap or embedded in controller)

### 1.3 Container Specs
- [ ] **LoadBalancer container**:
  - Image: `<registry>/stateless-load-balancer:<version>`
  - SecurityContext: `NET_ADMIN`, `SYS_ADMIN` capabilities
  - Env vars: `GATEWAY_NAME`, `GATEWAY_NAMESPACE`
  - Volume mounts: shared `/var/run/meridio` for readiness signaling
- [ ] **Router container**:
  - Image: `<registry>/router:<version>`
  - SecurityContext: `NET_ADMIN` capability
  - Env vars: `GATEWAY_NAME`, `GATEWAY_NAMESPACE`
  - Volume mounts: shared `/var/run/meridio`, Bird3 config
  - **Readiness gates**: Add custom readiness gate for external connectivity
    - Gate name: `meridio.nordix.org/external-connectivity`
    - Router container will update this based on BGP/BFD status

### 1.4 Status Updates
- [ ] Update Gateway.status.conditions:
  - `Accepted`: Gateway configuration valid (GatewayClass matches, GatewayConfig valid)
  - `Programmed`: LB Deployment ready
  - `Ready`: All LB Pods ready (including Router readiness gates)
- [ ] **Update Gateway.status.addresses**:
  - **Based on associated L34Routes** (VIPs from destinationCIDRs)
  - Include LB Pod secondary IPs for reference
  - Follow Gateway API address type conventions

### 1.5 Edge Cases
- [ ] Handle GatewayConfig not found (set condition `Accepted=False`)
- [ ] Handle invalid network attachments (validate NAD exists)
- [ ] Handle Deployment creation failure (retry with backoff)
- [ ] Handle scaling changes (update Deployment replicas)

---

## 2. DistributionGroup Controller

**Purpose**: Create EndpointSlices with secondary network IPs for DistributionGroup backends.

**Reference**: See [Issue #6 comment](https://github.com/Nordix/Meridio-2/issues/6#issuecomment-3723685373) for additional context.

### 2.1 Pod Discovery
- [ ] Watch DistributionGroup resources
- [ ] List Pods matching `spec.selector` (LabelSelector)
- [ ] Filter Pods by readiness (`Pod.Status.Conditions[Ready] == True`)
- [ ] Extract parentRef Gateway to determine network context

### 2.2 Secondary IP Extraction
- [ ] Parse Pod annotation `k8s.v1.cni.cncf.io/network-status` (Multus)
- [ ] Match network name from GatewayConfig.spec.networkAttachments
- [ ] Extract IP address from matched network interface
- [ ] Handle IPv4/IPv6 dual-stack (create separate EndpointSlices)

### 2.3 EndpointSlice Creation
- [ ] Create EndpointSlice owned by DistributionGroup
- [ ] Set `addressType`: `IPv4` or `IPv6`
- [ ] Populate `endpoints[]`:
  - `addresses`: secondary network IPs
  - `conditions.ready`: based on Pod readiness
  - `targetRef`: reference to Pod
- [ ] Set labels:
  - `kubernetes.io/service-name`: DistributionGroup name
  - `endpointslice.kubernetes.io/managed-by`: `meridio-controller`

### 2.4 EndpointSlice Updates
- [ ] Watch Pod changes (IP change, readiness change, deletion)
- [ ] Update EndpointSlice when Pods added/removed
- [ ] Handle DistributionGroup selector changes (rebuild EndpointSlice)
- [ ] Batch updates (debounce rapid changes)

### 2.5 Edge Cases
- [ ] Handle Pod without secondary network annotation (skip with warning event)
- [ ] Handle multiple network attachments (use first matching GatewayConfig network)
- [ ] Handle DistributionGroup with no matching Pods (create empty EndpointSlice)
- [ ] Handle mixed IPv4/IPv6 Pods (create 2 EndpointSlices)

---

## 3. EndpointNetworkConfiguration Controller

**Purpose**: Create per-pod network configuration CRs for application sidecars.

### 3.1 Pod Discovery
- [ ] Watch Pods with label `meridio.nordix.org/inject-config: "true"`
- [ ] Check if Pod is part of a DistributionGroup (match labels)
- [ ] Handle Pod create/update/delete events

### 3.2 Gateway Discovery
- [ ] Find L34Routes with `backendRefs` pointing to Pod's DistributionGroup
- [ ] Extract `parentRefs` (Gateway references) from L34Routes
- [ ] Resolve Gateway names to Gateway objects
- [ ] Handle multiple Gateways (Pod may be behind multiple LBs)

### 3.3 VIP Collection
- [ ] For each Gateway, collect VIPs from L34Routes:
  - Filter L34Routes: `parentRef == Gateway` AND `backendRef == DistributionGroup`
  - Extract `destinationCIDRs` (VIPs)
- [ ] Deduplicate VIPs across multiple L34Routes
- [ ] Group VIPs by Gateway and IP family

### 3.4 Next-Hop Collection (LB Pod IPs)
- [ ] For each Gateway, find LB Deployment (via Gateway.status or labels)
- [ ] List Pods in LB Deployment
- [ ] Extract secondary network IPs from LB Pods (parse Multus annotation)
- [ ] **Filter by LB Pod readiness**:
  - Pod.Status.Conditions[Ready] == True
  - **Router container readiness gate** (`meridio.nordix.org/external-connectivity`) == True
  - Only include LB Pods with established external connectivity (BGP/BFD up)
- [ ] Match network interface (same network as application Pod)

### 3.5 Network Interface Detection
- [ ] Parse Pod's Multus `network-status` annotation
- [ ] Match network name from GatewayConfig
- [ ] Determine interface name (e.g., `net1`)
- [ ] Extract subnet from network-status for validation

### 3.6 EndpointNetworkConfiguration CR Management
- [ ] Create CR with name matching Pod name (e.g., `<pod-name>`)
- [ ] Set owner reference to Pod (automatic cleanup on Pod deletion)
- [ ] Populate spec:
  ```yaml
  spec:
    gateways:
    - name: gateway-a
      domains:
      - name: gateway-a-ipv4
        ipFamily: IPv4
        network:
          subnet: 169.254.100.0/24
          interfaceHint: net1
        vips:
        - 20.0.0.1/32
        - 40.0.0.1/32
        nextHops:
        - 169.254.100.10  # LB Pod 1
        - 169.254.100.11  # LB Pod 2
  ```
- [ ] Update CR when configuration changes (VIPs, next-hops, Gateways)
- [ ] Handle CR already exists (update, don't fail)

### 3.7 Edge Cases
- [ ] Handle Pod without secondary network (skip, emit warning event)
- [ ] Handle Pod in multiple DistributionGroups (multiple gateway configs)
- [ ] Handle Gateway with no ready LB Pods (empty next-hops, sidecar waits)
- [ ] Handle L34Route deletion (remove VIPs from CR)
- [ ] Handle Gateway deletion (remove gateway from CR)
- [ ] Handle network interface name mismatch (Pod vs LB Pod on different networks)

---

## 4. LoadBalancer Container Logic

**Purpose**: Manage NFQLB instances and nftables rules for traffic distribution.

### 4.1 Initialization
- [ ] Determine Gateway identity (env vars: `GATEWAY_NAME`, `GATEWAY_NAMESPACE`)
- [ ] Set up Kubernetes client (in-cluster config)
- [ ] Initialize nftables (create `inet meridio` table)
- [ ] Create shared directory `/var/run/meridio` for readiness signaling

### 4.2 Resource Watching
- [ ] Watch L34Routes with `parentRefs` matching this Gateway
- [ ] Watch DistributionGroups referenced by L34Routes
- [ ] Watch EndpointSlices for DistributionGroups
- [ ] Handle watch reconnections and errors

### 4.3 NFQLB Instance Management
- [ ] Create NFQLB instance per DistributionGroup:
  - Shared memory name: `/meridio-<gateway>-<distgroup>`
  - Maglev M parameter: from DistributionGroup.spec.maglev (default 997)
  - Maglev N parameter: DistributionGroup.spec.maglev.maxEndpoints (default 32)
  - nfqueue number: allocate dynamically (e.g., 100 + index)
- [ ] Start NFQLB process: `nfqlb lb <instance> --queue <num> --M <M> --N <N>`
- [ ] Track NFQLB instances (map: DistributionGroup → instance)
- [ ] Delete NFQLB instance when DistributionGroup no longer referenced
- [ ] Monitor NFQLB process health (restart on crash)

### 4.4 Target Management (NFQLB Backends)
- [ ] Watch EndpointSlices for each DistributionGroup
- [ ] Extract target IPs from `EndpointSlice.endpoints[].addresses`
- [ ] Filter by `endpoints[].conditions.ready == true`
- [ ] Add targets: `nfqlb target-add <instance> <ip>`
- [ ] Remove targets: `nfqlb target-delete <instance> <ip>`
- [ ] Handle target updates (IP change, readiness change)

### 4.5 Flow Management (Traffic Classification)
- [ ] Watch L34Routes for this Gateway
- [ ] Map L34Route to NFQLB instance (via `backendRefs` → DistributionGroup)
- [ ] Extract flow parameters:
  - VIPs: `destinationCIDRs`
  - Protocols: `protocols` (TCP/UDP/SCTP)
  - Source/destination ports: `sourcePorts`, `destinationPorts`
  - Priority: `priority`
- [ ] Add flow: `nfqlb flow-add <instance> --vip <vip> --proto <proto> --dport <port> --prio <prio>`
- [ ] Update flow when L34Route changes
- [ ] Delete flow when L34Route removed

### 4.6 nftables Rules (Traffic Steering)
- [ ] Create nftables chain:
  ```
  nft add chain inet meridio prerouting { type filter hook prerouting priority 0; }
  ```
- [ ] Add rules per VIP to queue traffic:
  ```
  nft add rule inet meridio prerouting ip daddr <vip> queue num <nfqueue-id>
  ```
- [ ] **Add ICMP handling rules** (echo-request/reply for VIPs):
  ```
  nft add rule inet meridio prerouting ip daddr <vip> icmp type echo-request accept
  nft add rule inet meridio prerouting ip daddr <vip> icmp type echo-reply accept
  ```
  - **Note**: ICMP handling is important for VIP reachability testing
- [ ] Update rules when VIPs added/removed
- [ ] Clean up rules on shutdown (delete table)

### 4.7 Readiness Signaling
- [ ] Determine readiness per DistributionGroup:
  - NFQLB instance running
  - At least one target available
  - Flows configured
- [ ] Write readiness to file: `/var/run/meridio/lb-ready-<distgroup>`
- [ ] Update readiness when targets change
- [ ] **Router container reads these files** to decide VIP advertisement
- [ ] **Optional (post-MVP)**: Expose NFQLB distribution readiness via more sophisticated mechanism
  - Could use shared memory for faster updates
  - Could expose metrics endpoint

### 4.8 Edge Cases
- [ ] Handle L34Route with non-existent DistributionGroup (log error, skip)
- [ ] Handle DistributionGroup with no endpoints (mark not ready, don't advertise VIP)
- [ ] Handle overlapping flows (NFQLB handles by priority)
- [ ] Handle NFQLB shared memory full (log error, reject new targets)
- [ ] Handle nftables rule conflicts (use unique chain names)

---

## 5. Router Container Logic

**Purpose**: Manage BGP/BFD via Bird3 for VIP advertisement.

### 5.1 Initialization
- [ ] Determine Gateway identity (env vars: `GATEWAY_NAME`, `GATEWAY_NAMESPACE`)
- [ ] Set up Kubernetes client (in-cluster config)
- [ ] Initialize Bird3 configuration directory

### 5.2 GatewayRouter Configuration
- [ ] Watch GatewayRouter resources for this Gateway
- [ ] Extract BGP/BFD parameters:
  - Local/remote AS numbers
  - Neighbor address
  - Interface
  - BFD timers (min-rx, min-tx, multiplier)
- [ ] Generate Bird3 configuration file
- [ ] Reload Bird3 on configuration change

### 5.3 VIP Advertisement
- [ ] Watch L34Routes for this Gateway
- [ ] Extract VIPs from `destinationCIDRs`
- [ ] Check readiness from LoadBalancer container (`/var/run/meridio/lb-ready-<distgroup>`)
  - **First iteration**: Use dummy file, assume LB is always ready
  - **Later**: Integrate with reconcile loop using `WatchesRawSource` builder option
- [ ] Add VIP to loopback interface: `ip addr add <vip> dev lo`
- [ ] Advertise VIP via BGP (Bird3 exports loopback routes)
- [ ] Withdraw VIP when not ready (remove from loopback)

### 5.4 BGP/BFD Monitoring & External Connectivity
- [ ] Monitor BGP session status (via Bird3 CLI: `birdc show protocols`)
- [ ] Monitor BFD session status
- [ ] **Determine external connectivity status**:
  - BGP session established AND BFD session up = connected
  - Either down = disconnected
- [ ] **Expose external connectivity via Pod readiness gate**:
  - Update Pod condition `meridio.nordix.org/external-connectivity`
  - Requires Kubernetes API access from Router container
  - Use in-cluster client to patch Pod status
- [ ] Emit Kubernetes events on session up/down
- [ ] Update Gateway.status with BGP/BFD state (if possible)
- [ ] **Refer to Meridio** for Bird3 status monitoring implementation

### 5.5 Edge Cases
- [ ] Handle GatewayRouter not found (log error, don't start BGP)
- [ ] Handle BGP session down (keep advertising VIPs, rely on BFD)
- [ ] Handle BFD session down (withdraw VIPs immediately)
- [ ] Handle Bird3 crash (restart, restore configuration)

---

## 6. Application Sidecar Logic

**Purpose**: Apply VIPs and source-based routing to application pod network namespace.

**Note**: The sidecar is the chosen alternative for Application Network Configuration Injection (instead of POC's Network Daemon approach). The sidecar consumes the EndpointNetworkConfiguration CR produced by the controller (Section 3).

### 6.1 Initialization
- [ ] Determine Pod identity (env vars: `POD_NAME`, `POD_NAMESPACE`)
- [ ] Set up Kubernetes client (in-cluster config)
- [ ] Watch EndpointNetworkConfiguration with name matching Pod name

### 6.2 Network Interface Discovery
- [ ] For each `spec.gateways[].domains[]`:
  - Parse `network.subnet` to determine IP family
  - Use `network.interfaceHint` to find interface (e.g., `net1`)
  - Validate interface IP matches subnet
  - If no hint, iterate interfaces to find matching subnet

### 6.3 VIP Configuration
- [ ] For each domain, add VIPs to interface:
  ```bash
  ip addr add <vip> dev <interface>
  ```
- [ ] Handle IPv6 VIPs (add `nodad` flag)
- [ ] Remove VIPs when domain removed from spec

### 6.4 Source-Based Routing
- [ ] For each domain, create routing table:
  - Allocate table ID (e.g., hash of domain name)
  - Add to `/etc/iproute2/rt_tables`: `<table-id> <domain-name>`
- [ ] Add default route via next-hops:
  ```bash
  ip route add default table <table-id> \
    nexthop via <nexthop1> dev <interface> weight 1 \
    nexthop via <nexthop2> dev <interface> weight 1
  ```
- [ ] Add source-based routing rule:
  ```bash
  ip rule add from <vip> table <table-id> priority 100
  ```
- [ ] Update routes when next-hops change
- [ ] Remove routes/rules when domain removed

### 6.5 Status Updates
- [ ] Update EndpointNetworkConfiguration.status.conditions:
  - `Ready=True`: All VIPs and routes configured
  - `Ready=False`: Configuration failed
- [ ] Set `observedGeneration` to track which spec version is applied
- [ ] Emit events for configuration errors

### 6.6 Edge Cases
- [ ] Handle interface not found (retry with backoff, emit event)
- [ ] Handle subnet mismatch (log error, skip domain)
- [ ] Handle no next-hops (skip routing, only configure VIPs)
- [ ] Handle next-hop unreachable (log warning, keep route)
- [ ] Handle sidecar restart (restore configuration from CR)
- [ ] **Handle cleanup when sidecar was unavailable**:
  - Sidecar crashed and missed Gateway removal from EndpointNetworkConfiguration
  - **Solution**: Use nftables sets to store old VIP state
    - Sidecar has NET_ADMIN capability (needed for IP/route management anyway)
    - On startup, read nftables sets to discover previously configured VIPs
    - Compare with current CR spec and remove stale VIPs
    - Example: `nft add set inet meridio vips { type ipv4_addr; }`
  - **Note**: This is an advanced edge case, may be post-MVP

---

## 7. Cross-Cutting MVP Requirements

These items span multiple components and are essential for a usable MVP.

### 7.1 Testing (Must Have)
- [ ] Basic unit tests for critical paths:
  - Gateway controller reconciliation logic
  - DistributionGroup EndpointSlice creation
  - Sidecar VIP/routing logic
- [ ] Simple E2E test:
  - Deploy Gateway, GatewayRouter, L34Route, DistributionGroup, application
  - Verify external traffic reaches application
  - Verify BGP advertises VIP
- [ ] E2E test for 2 Gateways in one namespace:
  - Verify isolation (separate VIPs, separate LB Deployments)
  - Verify no cross-gateway interference

### 7.2 Validation (Must Have)
- [ ] Webhook validation for GatewayRouter:
  - Validate BGP AS numbers (valid range)
  - Validate BFD timers (min-rx, min-tx > 0)
  - Validate neighbor address (valid IP)
  - Validate interface name (non-empty)
- [ ] Webhook validation for GatewayConfig:
  - Validate network attachments (NAD exists)
  - Validate replicas > 0
  - Validate resource requests/limits
- [ ] Validate DistributionGroup references in L34Route (controller-side)

### 7.3 Error Handling (Must Have)
- [ ] Basic error handling in controllers:
  - Retry on transient failures (API server unavailable)
  - Set status conditions on errors
  - Emit events for user-visible errors
- [ ] Finalizers for cleanup:
  - Gateway: delete LB Deployment, remove from status
  - DistributionGroup: delete EndpointSlices
  - EndpointNetworkConfiguration: cleanup handled by owner reference
- [ ] Graceful degradation:
  - LB pods unavailable: controller logs warning, doesn't crash
  - Sidecar crash recovery: restore VIPs/routes from CR on restart

### 7.4 Observability (Must Have)
- [ ] Controller logs:
  - Structured logging (JSON format)
  - ERROR level for failures
  - INFO level for state changes (Gateway created, LB ready)
  - DEBUG level for reconciliation details
- [ ] Health checks:
  - Controller manager: liveness (process alive), readiness (leader elected)
  - Sidecar: liveness (process alive), readiness (VIPs configured)
  - LB container: liveness (process alive), readiness (NFQLB running)
  - Router container: liveness (process alive), readiness (BGP session up)
- [ ] Kubernetes Events:
  - Gateway: Accepted, Programmed, Ready conditions
  - DistributionGroup: EndpointSlice created/updated
  - EndpointNetworkConfiguration: Configuration applied/failed
  - BGP session: up/down events
  - BFD session: up/down events

### 7.5 Security (Must Have)
- [ ] RBAC for controller manager:
  - Read: Gateway, GatewayConfig, GatewayRouter, L34Route, DistributionGroup, Pod, EndpointSlice
  - Write: Deployment, EndpointSlice, EndpointNetworkConfiguration
  - Update status: Gateway, DistributionGroup, EndpointNetworkConfiguration
  - Create events: all namespaces
- [ ] Sidecar security context:
  - Capabilities: NET_ADMIN only
  - Run as non-root (if possible, may need root for netns operations)
  - Read-only root filesystem (except /var/run)
  - Drop all capabilities except NET_ADMIN

### 7.6 Documentation (Must Have)
- [ ] Quick start guide:
  - Prerequisites (cert-manager, Multus, CNI plugins, Whereabouts)
  - Install operator (kubectl apply or Helm)
  - Create Gateway with GatewayRouter
  - Deploy test application with sidecar
  - Verify traffic flow
- [ ] Example manifests:
  - Gateway + GatewayConfig + GatewayRouter (complete setup)
  - L34Route (TCP, UDP, SCTP examples)
  - DistributionGroup (with selector)
  - Application Pod with sidecar (manual injection)
  - NetworkAttachmentDefinition (Multus)
- [ ] Basic troubleshooting:
  - Check controller logs: `kubectl logs -n meridio-system deployment/meridio-controller-manager`
  - Check LB pod logs: `kubectl logs <lb-pod> -c loadbalancer` / `-c router`
  - Check sidecar logs: `kubectl logs <app-pod> -c meridio-sidecar`
  - Verify BGP session: `kubectl exec <lb-pod> -c router -- birdc show protocols`
  - Verify BFD session: `kubectl exec <lb-pod> -c router -- birdc show bfd sessions`
  - Debug traffic: `kubectl exec <lb-pod> -c loadbalancer -- nfqlb flow-list`
- [ ] Architecture diagram:
  - Control plane: Controller Manager → Gateway/L34Route/DistributionGroup
  - Data plane: External traffic → LB Pod (Router + LoadBalancer) → Application Pod (Sidecar)
- [ ] API reference:
  - CRD field descriptions (generated from kubebuilder markers)
  - Example values for each field

### 7.7 Should Have for MVP (Important but not blocking)

#### Testing
- [ ] Integration tests for controller reconciliation logic (envtest)
- [ ] Test DistributionGroup with no matching Pods
- [ ] Test L34Route with invalid DistributionGroup reference

#### Observability
- [ ] Key Prometheus metrics:
  - `meridio_bgp_session_up{gateway}` (gauge: 0/1)
  - `meridio_bfd_session_up{gateway}` (gauge: 0/1)
  - `meridio_vip_count{gateway}` (gauge)
  - `meridio_sidecar_config_status{pod}` (gauge: 0=failed, 1=ready)
  - `meridio_controller_reconcile_duration_seconds{controller}` (histogram)
- [ ] Controller reconciliation latency metric

#### Error Handling
- [ ] Retry logic with exponential backoff (controller-runtime default)
- [ ] Handle partial failures (some VIPs configured, others failed)

### 7.8 Nice to Have (Post-MVP)

#### Testing
- [ ] Performance/load testing for NFQLB (max flows, max targets)
- [ ] Chaos testing (pod failures, network partitions, API server unavailable)
- [ ] Multi-gateway scenarios (cross-namespace, different networks)

#### Observability
- [ ] NFQLB flow statistics (packets, connections per VIP)
- [ ] Detailed controller metrics (queue depth, reconciliation errors, requeue count)

#### Documentation
- [ ] Migration guide from Meridio v1 to v2
- [ ] Performance tuning guide (NFQLB parameters, BGP timers, BFD timers)

### 7.9 Out of Scope for MVP
- [ ] Network policies for controller-to-API-server
- [ ] BGP authentication (mentioned as "without authentication" in plan)
- [ ] Cross-resource validation (complex dependency checks across CRDs)
- [ ] Advanced scaling strategies (HPA, VPA integration)

---

## Implementation Order (Recommended)

### Phase 1: Basic Data Plane (Week 1-2)
1. **Gateway Controller** (1.1-1.3): Create LB Deployments
2. **DistributionGroup Controller** (2.1-2.3): Create EndpointSlices
3. **LoadBalancer Container** (4.1-4.6): NFQLB + nftables
4. **Router Container** (5.1-5.3): BGP/BFD + VIP advertisement

**Milestone**: External traffic reaches LB, gets distributed to backends.

### Phase 2: Application Integration (Week 3)
5. **EndpointNetworkConfiguration Controller** (3.1-3.6): Create per-pod configs
6. **Application Sidecar** (6.1-6.4): Apply VIPs and routing

**Milestone**: Application pods receive traffic on VIPs, return traffic routed correctly.

### Phase 3: Observability & Robustness (Week 4)
7. Status updates (1.4, 3.7, 6.5)
8. Readiness signaling (4.7, 5.3)
9. Edge case handling (all .5/.6/.7/.8 sections)
10. Cross-cutting requirements (Section 7.1-7.6):
    - Basic unit tests
    - Webhook validation (GatewayRouter, GatewayConfig)
    - Error handling and finalizers
    - Observability (logs, health checks, events)
    - Security (RBAC, security contexts)
    - Documentation (quick start, examples, troubleshooting)

**Milestone**: MVP ready for early feedback.

---

## Key Architectural Decisions

### Decided

1. **Routing Suite**: **Bird3** ✅
   - Latest Meridio works well with Bird3
   - Proven stability and performance

2. **Readiness Communication (LB → Router)**: **File-based** ✅
   - Simple, no dependencies
   - Files in `/var/run/meridio/lb-ready-<distgroup>`

3. **Sidecar Injection**: **Manual in Pod spec** ✅
   - Simple, user responsibility
   - No webhook complexity for MVP

### To Be Decided

4. **NFQLB nfqueue numbering**:
   - Static per Gateway (e.g., 100-199 for gateway-a)?
   - Dynamic allocation (track in controller)?

5. **EndpointSlice addressType**:
   - Use standard `IPv4`/`IPv6` (recommended)
   - Custom type like `SecondaryIPv4` (requires custom controller)

6. **Next-hop selection**:
   - All LB Pods (full mesh, recommended for MVP)
   - Subset based on locality/zone (complex, post-MVP)

---

## MVP Success Criteria

**Can demonstrate:**
1. Deploy Gateway with GatewayRouter (BGP to external gateway)
2. Create L34Route pointing to DistributionGroup
3. Application pod receives VIP and routes via sidecar
4. External traffic reaches application through LB
5. BGP advertises VIP, BFD detects failures
6. Clean removal (delete Gateway → everything cleaned up)
7. Two Gateways in same namespace work independently

**Early feedback focus:**
- Does the API design make sense?
- Is the sidecar approach acceptable?
- Are the main use cases covered?
- What's missing for production use?
