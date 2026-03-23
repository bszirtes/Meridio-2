# EndpointNetworkConfiguration Controller

The ENC controller resolves the network configuration that each application Pod needs from the Meridio-2 data plane and writes it into an EndpointNetworkConfiguration CR for the sidecar to consume. It watches target Pods and resolves the full resource chain to produce an ENC with VIPs, source-based routing next-hops, and network identity.

## Resolution Chain

For each running Pod that matches a DistributionGroup selector, the controller resolves:

```
Pod
 └─ DistributionGroup (label selector match)
     └─ Gateway (via DG.parentRefs or L34Route.parentRefs → backendRefs)
         ├─ GatewayConfiguration (via Gateway.infrastructure.parametersRef)
         │   ├─ Network subnets + interface hints
         │   └─ NAD attachments
         ├─ VIPs (from Gateway.status.addresses)
         └─ SLLBR Pods (labeled gateway.networking.k8s.io/gateway-name)
             └─ NextHops (IPs extracted from Pod status matching subnet CIDRs)
```

The output is an ENC with one `GatewayConnection` per Gateway, each containing dual-stack domains with:
- **VIPs**: plain IPs (not CIDRs) from Gateway status addresses
- **NextHops**: SLLBR Pod IPs on the application network subnet
- **Domain naming**: `<gateway>-<ipFamily>` (e.g. `gw-a-IPv4`, `gw-a-IPv6`)
- **Network context**: subnet CIDR and interface hint from GatewayConfiguration

## Watches and Triggers

The controller is triggered by changes to any resource in the chain:

| Resource | Trigger | Mapper |
|----------|---------|--------|
| Pod (target) | Primary — reconcile on create/update/delete | Direct (For) |
| ENC | Owned — reconcile owner Pod on changes | Owner reference (Owns) |
| DistributionGroup | Selector or parentRefs change | `mapDGToPods` |
| Gateway | Status/spec change | `mapGatewayToPods` |
| L34Route | parentRefs/backendRefs change | `mapL34RouteToPods` |
| GatewayConfiguration | Network config change | `mapGatewayConfigToPods` |
| Pod (SLLBR) | IP change on SLLBR Pods | `mapSLLBRPodToPods` (label predicate) |

SLLBR Pods are filtered by the `gateway.networking.k8s.io/gateway-name` label to avoid reconciling on unrelated Pod changes.

## Reconcile Logic

1. Fetch the Pod. If not found → no-op. If not Running → delete stale ENC if exists.
2. Resolve all `GatewayConnection`s via the chain above.
3. If no connections → delete ENC if exists (Pod no longer targeted).
4. If connections exist → create or update the ENC (skip update if spec unchanged).

The ENC is owned by the Pod (via `controllerutil.SetControllerReference`), so it is garbage-collected when the Pod is deleted.

## Files

| File | Lines | Purpose |
|------|-------|---------|
| `controller.go` | 165 | Reconciler, setup, ENC create/update/delete |
| `resolve.go` | 454 | Full resolution chain: DG → Gateway → GatewayConfig → SLLBR NextHops → ENC spec |
| `mappers.go` | 267 | Watch mappers: resource change → affected Pod reconcile requests |
| `*_test.go` | 1050 | 39 unit tests (77% coverage) |
