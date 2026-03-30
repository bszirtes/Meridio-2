# E2E Integration Demo — Multi-Gateway Topology

Verified on Kind cluster `meridio-e2e` with 2 workers, 4 gateways across 2 namespaces.

> **IPv4-only scope:** The e2e tests cover IPv4 only. IPv6 works in isolation
> (single address family), but dual-stack is not supported until the LB controller
> race condition is fixed: the loadbalancer controller processes EndpointSlices
> sequentially, so the second address family's routes overwrite the first.
> See [Known Issues](#known-issues).

## Topology

```
VPN Gateway (Docker, BIRD 3.x)
├── vlan1 (VLAN 100) 169.254.100.150 → gw-a1 (ASN 8103)
├── vlan2 (VLAN 200) 169.254.200.150 → gw-a2 (ASN 8104)
├── vlan3 (VLAN 300) 169.254.101.150 → gw-b1 (ASN 8105)
└── vlan4 (VLAN 400) 169.254.201.150 → gw-b2 (ASN 8106)

e2e-separate-app-nets (separate app-nets per gateway):
  gw-a1 → VIP 20.0.0.1  (app-net-a1, 169.111.100.0/24)
  gw-a2 → VIP 20.0.0.2  (app-net-a2, 169.111.200.0/24)
  2 target pods, each connected to both app-nets

e2e-shared-app-net (shared app-net across gateways):
  gw-b1 → VIP 30.0.0.1  (app-net-b, 169.111.100.0/24)
  gw-b2 → VIP 30.0.0.2  (app-net-b, 169.111.100.0/24)
  2 target pods, each connected to shared app-net
```

## Deployment

### Prerequisites

- Docker
- Kind cluster with Multus, Whereabouts, Gateway API CRDs, cert-manager
- VPN gateway container running on the Kind network
- Controller-manager deployed per namespace

### Apply Test Resources

```bash
kubectl apply -f test/e2e/testdata/common/
kubectl apply -f test/e2e/testdata/separate-app-nets/
kubectl apply -f test/e2e/testdata/shared-app-net/
```

### Run Tests

```bash
go test ./test/e2e/ -tags e2e -v -timeout 20m
```

Each test (`separate-app-nets`, `shared-app-net`) deploys its own namespace
resources in `BeforeAll` and cleans up in `AfterAll`.

---

## Verification Steps

## 1. Verify Pods Running

```bash
kubectl get pods -n e2e-separate-app-nets -o wide
kubectl get pods -n e2e-shared-app-net -o wide
```

Expected: 8 SLLBR pods (2/2 Running, 2 per gateway), 4 target pods (2/2 Running).

## 2. Verify BGP Sessions (8 sessions, 2 per gateway)

```bash
docker exec vpn-gateway birdc show protocols | grep Established
```

Expected: 8 BGP sessions Established (4 gateways × 2 replicas).

## 3. Verify BFD Sessions (8 sessions)

```bash
docker exec vpn-gateway birdc show bfd sessions
```

Expected: 8 BFD sessions Up (2 per gateway VLAN).

## 4. Verify VIP Routes Learned

```bash
docker exec vpn-gateway ip route
```

Expected:
```
20.0.0.1 proto bird metric 32
    nexthop via 169.254.100.1 dev vlan1 weight 1
    nexthop via 169.254.100.2 dev vlan1 weight 1
20.0.0.2 proto bird metric 32
    nexthop via 169.254.200.1 dev vlan2 weight 1
    nexthop via 169.254.200.2 dev vlan2 weight 1
30.0.0.1 proto bird metric 32
    nexthop via 169.254.101.1 dev vlan3 weight 1
    nexthop via 169.254.101.2 dev vlan3 weight 1
30.0.0.2 proto bird metric 32
    nexthop via 169.254.201.1 dev vlan4 weight 1
    nexthop via 169.254.201.2 dev vlan4 weight 1
```

## 5. Verify ENCs

```bash
kubectl get enc -n e2e-separate-app-nets -o jsonpath='{range .items[*]}ENC: {.metadata.name}{"\n"}{range .spec.gateways[*]}  gw: {.name}{"\n"}{end}{"\n"}{end}'
kubectl get enc -n e2e-shared-app-net -o jsonpath='{range .items[*]}ENC: {.metadata.name}{"\n"}{range .spec.gateways[*]}  gw: {.name}{"\n"}{end}{"\n"}{end}'
```

Expected: 2 ENCs per namespace, each with 2 gateway entries.

## 6. Traffic Tests

```bash
for vip in 20.0.0.1 20.0.0.2 30.0.0.1 30.0.0.2; do
  loss=$(docker exec vpn-gateway ping -c 3 -W 2 $vip 2>&1 | grep -oP '\d+(?=% packet loss)')
  echo "$vip: ${loss}% loss"
done
```

```bash
for spec in "gw-a1,20.0.0.1" "gw-a2,20.0.0.2" "gw-b1,30.0.0.1" "gw-b2,30.0.0.2"; do
  IFS=, read gw vip <<< "$spec"
  out=$(docker exec vpn-gateway /opt/ctraffic -address $vip:5000 -nconn 100 -timeout 10s -stats all)
  nhosts=$(echo "$out" | jq '[.ConnStats[].Host // empty] | unique | length')
  lost=$(echo "$out" | jq '.FailedConnects')
  echo "$gw TCP: $nhosts hosts, $lost lost"
done
```

---

## Verified Results

```
separate-app-nets:
  [PASS] gw-a1 ICMP: 0% loss
  [PASS] gw-a2 ICMP: 0% loss
  [PASS] gw-a1 TCP:  2 hosts, 0 lost
  [PASS] gw-a2 TCP:  2 hosts, 0 lost
  [PASS] gw-a1 UDP:  2 hosts, 0 lost
  [PASS] gw-a2 UDP:  2 hosts, 0 lost

shared-app-net:
  [PASS] gw-b1 ICMP: 0% loss
  [PASS] gw-b2 ICMP: 0% loss
  [PASS] gw-b1 TCP:  2 hosts, 0 lost
  [PASS] gw-b2 TCP:  2 hosts, 0 lost
  [PASS] gw-b1 UDP:  2 hosts, 0 lost
  [PASS] gw-b2 UDP:  2 hosts, 0 lost

Results: 12 passed, 0 failed
```

---

## Known Issues

### No Dual-Stack Support (LB Controller Race)

IPv6 works in isolation (single address family per gateway), but dual-stack
(IPv4 + IPv6 simultaneously) is not supported. The loadbalancer controller
processes EndpointSlices sequentially — since each slice covers one address
family, the second family's routes overwrite the first. The result is that
only one address family gets fwmark-based routing rules at any given time.

The fix requires the LB controller to merge routes across both address families
per target identifier rather than replacing them. Until then, the e2e topology
is IPv4-only.
