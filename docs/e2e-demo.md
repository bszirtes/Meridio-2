# E2E Integration Demo — Multi-Gateway Topology

Verified on Kind cluster `meridio-e2e` with 2 workers, 4 gateways across 2 namespaces.

## Topology

```
VPN Gateway (Docker, BIRD 3.x)
├── vlan1 (VLAN 100) 169.254.100.150 fd00:100::150 → gw-a1 (ASN 8103)
├── vlan2 (VLAN 200) 169.254.200.150 fd00:200::150 → gw-a2 (ASN 8104)
├── vlan3 (VLAN 300) 169.254.101.150 fd00:300::150 → gw-b1 (ASN 8105)
└── vlan4 (VLAN 400) 169.254.201.150 fd00:400::150 → gw-b2 (ASN 8106)

e2e-ns-a (separate app-nets per gateway):
  gw-a1 → VIP 20.0.0.1, 2001:db8::1    (app-net-a1, 169.111.100.0/24)
  gw-a2 → VIP 20.0.0.2, 2001:db8::2    (app-net-a2, 169.111.200.0/24)
  2 target pods, each connected to both app-nets

e2e-ns-b (shared app-net across gateways):
  gw-b1 → VIP 30.0.0.1, 2001:db8:1::1  (app-net-b, 169.111.100.0/24)
  gw-b2 → VIP 30.0.0.2, 2001:db8:1::2  (app-net-b, 169.111.100.0/24)
  2 target pods, each connected to shared app-net
```

## Deployment

### Prerequisites

- Docker
- Kind
- kubectl
- jq (for traffic test output parsing)

### Step 0: Create Kind Cluster

```bash
./test/e2e/scripts/setup-kind.sh
```

This creates cluster `meridio-e2e` with 2 workers, installs Multus, Whereabouts,
Gateway API CRDs, and starts the VPN gateway container.

### Step 1: Install Standard CNI Plugins

Kind nodes don't include vlan/macvlan/bridge plugins by default:

```bash
for node in $(kind get nodes --name meridio-e2e); do
  docker exec $node bash -c \
    'curl -sL https://github.com/containernetworking/plugins/releases/download/v1.6.1/cni-plugins-linux-amd64-v1.6.1.tgz | tar -xz -C /opt/cni/bin/'
done
```

### Step 2: Build and Load Images

```bash
export REG=registry.nordix.org/cloud-native/meridio-2
export TAG=e2e-$(git rev-parse --short HEAD)

for img in controller-manager stateless-load-balancer router sidecar; do
  make $img BUILD_STEPS="build tag" REGISTRY=$REG VERSION=$TAG
  kind load docker-image $REG/$img:$TAG --name meridio-e2e
done
```

### Step 3: Install CRDs and cert-manager

```bash
make install
make cert-manager
```

### Step 4: Deploy Controller-Manager Per Namespace

```bash
make deploy NAMESPACE=e2e-ns-a REGISTRY=$REG VERSION_CONTROLLER_MANAGER=$TAG
make deploy NAMESPACE=e2e-ns-b REGISTRY=$REG VERSION_CONTROLLER_MANAGER=$TAG

# Patch ClusterRoleBinding (make deploy overwrites subjects on second run)
kubectl patch clusterrolebinding meridio-2-manager-rolebinding --type='json' \
  -p='[{"op":"add","path":"/subjects/-","value":{"kind":"ServiceAccount","name":"meridio-2-controller-manager","namespace":"e2e-ns-a"}}]'
```

### Step 5: Update Templates ConfigMap with Real Image Refs

The LB deployment template uses generic image names. Substitute with versioned tags:

```bash
for ns in e2e-ns-a e2e-ns-b; do
  sed "s|stateless-load-balancer:latest|$REG/stateless-load-balancer:$TAG|;s|router:latest|$REG/router:$TAG|" \
    config/templates/lb-deployment.yaml > /tmp/lb.yaml
  kubectl create configmap meridio-2-stateless-load-balancer-templates \
    --from-file=lb-deployment.yaml=/tmp/lb.yaml -n $ns --dry-run=client -o yaml | kubectl apply -f -
done
```

### Step 6: Apply Test Resources

```bash
kubectl apply -f test/e2e/testdata/common/
kubectl apply -f test/e2e/testdata/ns-a/
kubectl apply -f test/e2e/testdata/ns-b/
```

### Step 7: Fix Sidecar Image on Targets

Target YAML uses generic `sidecar:latest`. Patch with the real image:

```bash
kubectl set image deployment/target-a -n e2e-ns-a sidecar=$REG/sidecar:$TAG
kubectl set image deployment/target-b -n e2e-ns-b sidecar=$REG/sidecar:$TAG
```

### Step 8: Wait for System Ready

```bash
# Wait for controller-managers
kubectl wait --for=condition=Available --timeout=120s \
  -n e2e-ns-a deployment/meridio-2-controller-manager
kubectl wait --for=condition=Available --timeout=120s \
  -n e2e-ns-b deployment/meridio-2-controller-manager

# Wait for all SLLBR pods
kubectl wait --for=condition=Ready --timeout=120s \
  -n e2e-ns-a pods -l gateway.networking.k8s.io/gateway-name
kubectl wait --for=condition=Ready --timeout=120s \
  -n e2e-ns-b pods -l gateway.networking.k8s.io/gateway-name

# Wait for all target pods
kubectl wait --for=condition=Ready --timeout=120s -n e2e-ns-a pods -l app=target-a
kubectl wait --for=condition=Ready --timeout=120s -n e2e-ns-b pods -l app=target-b
```

---

## Verification Steps

## 1. Verify Pods Running

```bash
kubectl get pods -n e2e-ns-a -o wide
kubectl get pods -n e2e-ns-b -o wide
```

Expected: 4 SLLBR pods (2/2 Running), 4 target pods (2/2 Running).

## 2. Verify BGP Sessions (8 sessions)

```bash
# VPN gateway side
docker exec vpn-gateway birdc show protocols | grep Established

# SLLBR side
for ns in e2e-ns-a e2e-ns-b; do
  for pod in $(kubectl get pods -n $ns -l gateway.networking.k8s.io/gateway-name -o jsonpath='{.items[*].metadata.name}'); do
    echo "--- $pod ---"
    kubectl exec $pod -n $ns -c router -- birdc -s /var/run/bird/bird.ctl show protocols | grep Established
  done
done
```

Expected: 8 BGP sessions Established (4 gateways × IPv4 + IPv6).

## 3. Verify BFD Sessions (8 sessions)

```bash
docker exec vpn-gateway birdc show bfd sessions
```

Expected: 8 BFD sessions Up (4 IPv4 + 4 IPv6).

## 4. Verify VIP Routes Learned

```bash
docker exec vpn-gateway birdc show route
```

Expected output:
```
Table master4:
20.0.0.1/32    via 169.254.100.1 on vlan1   [GW4_A1_1]  (ASN 8103)
20.0.0.2/32    via 169.254.200.1 on vlan2   [GW4_A2_1]  (ASN 8104)
30.0.0.1/32    via 169.254.101.1 on vlan3   [GW4_B1_1]  (ASN 8105)
30.0.0.2/32    via 169.254.201.1 on vlan4   [GW4_B2_1]  (ASN 8106)

Table master6:
2001:db8::1/128    via fd00:100::1 on vlan1  [GW6_A1_1]
2001:db8::2/128    via fd00:200::1 on vlan2  [GW6_A2_1]
2001:db8:1::1/128  via fd00:300::1 on vlan3  [GW6_B1_1]
2001:db8:1::2/128  via fd00:400::1 on vlan4  [GW6_B2_1]
```

## 5. Verify ENCs (Multi-Gateway Content)

```bash
# ns-a: each target should have gw-a1 + gw-a2
kubectl get enc -n e2e-ns-a -o jsonpath='{range .items[*]}ENC: {.metadata.name}{"\n"}{range .spec.gateways[*]}  gw: {.name}{"\n"}{end}{"\n"}{end}'

# ns-b: each target should have gw-b1 + gw-b2
kubectl get enc -n e2e-ns-b -o jsonpath='{range .items[*]}ENC: {.metadata.name}{"\n"}{range .spec.gateways[*]}  gw: {.name}{"\n"}{end}{"\n"}{end}'
```

Expected: 2 ENCs per namespace, each with 2 gateway entries.

## 6. Verify Sidecar Configuration (VIPs + Policy Routing)

```bash
# Pick one target from each namespace
TARGET_A=$(kubectl get pods -n e2e-ns-a -l app=target-a -o jsonpath='{.items[0].metadata.name}')
TARGET_B=$(kubectl get pods -n e2e-ns-b -l app=target-b -o jsonpath='{.items[0].metadata.name}')

# ns-a: VIPs on separate interfaces (net1, net2)
echo "=== ns-a VIPs ===" && kubectl exec $TARGET_A -n e2e-ns-a -c sidecar -- ip addr show | grep -E "/32|/128" | grep -v "::1"
echo "=== ns-a rules ===" && kubectl exec $TARGET_A -n e2e-ns-a -c sidecar -- ip rule show | grep 5000
kubectl exec $TARGET_A -n e2e-ns-a -c sidecar -- ip -6 rule show | grep 5000
echo "=== ns-a tables ===" && kubectl exec $TARGET_A -n e2e-ns-a -c sidecar -- ip route show table 50000
kubectl exec $TARGET_A -n e2e-ns-a -c sidecar -- ip route show table 50001

# ns-b: VIPs on same interface (net1), different next-hops
echo "=== ns-b VIPs ===" && kubectl exec $TARGET_B -n e2e-ns-b -c sidecar -- ip addr show | grep -E "/32|/128" | grep -v "::1"
echo "=== ns-b rules ===" && kubectl exec $TARGET_B -n e2e-ns-b -c sidecar -- ip rule show | grep 5000
kubectl exec $TARGET_B -n e2e-ns-b -c sidecar -- ip -6 rule show | grep 5000
echo "=== ns-b tables ===" && kubectl exec $TARGET_B -n e2e-ns-b -c sidecar -- ip route show table 50000
kubectl exec $TARGET_B -n e2e-ns-b -c sidecar -- ip route show table 50001
```

Expected for ns-a (separate app-nets):
```
VIPs: 20.0.0.1/32 on net1, 20.0.0.2/32 on net2, 2001:db8::1/128, 2001:db8::2/128
Rules: from 20.0.0.1 lookup 50000, from 20.0.0.2 lookup 50001
Table 50000: default via 169.111.100.1 dev net1  (gw-a1)
Table 50001: default via 169.111.200.2 dev net2  (gw-a2)
```

Expected for ns-b (shared app-net):
```
VIPs: 30.0.0.1/32 on net1, 30.0.0.2/32 on net1, 2001:db8:1::1/128, 2001:db8:1::2/128
Rules: from 30.0.0.1 lookup 50000, from 30.0.0.2 lookup 50001
Table 50000: default via 169.111.100.3 dev net1  (gw-b1, same interface)
Table 50001: default via 169.111.100.4 dev net1  (gw-b2, same interface, different next-hop)
```

## 7. Traffic Tests — ICMP

```bash
# IPv4
for vip in 20.0.0.1 20.0.0.2 30.0.0.1 30.0.0.2; do
  loss=$(docker exec vpn-gateway ping -c 3 -W 2 $vip 2>&1 | grep -oP '\d+(?=% packet loss)')
  echo "IPv4 $vip: ${loss}% loss"
done

# IPv6
for vip in 2001:db8::1 2001:db8::2 2001:db8:1::1 2001:db8:1::2; do
  loss=$(docker exec vpn-gateway ping6 -c 3 -W 2 $vip 2>&1 | grep -oP '\d+(?=% packet loss)')
  echo "IPv6 $vip: ${loss}% loss"
done
```

Expected: 0% packet loss on all 8 VIPs.

## 8. Traffic Tests — TCP Load Balancing

```bash
# IPv4 TCP (100 connections per VIP, expect 2 target hosts each)
for spec in "gw-a1,20.0.0.1" "gw-a2,20.0.0.2" "gw-b1,30.0.0.1" "gw-b2,30.0.0.2"; do
  IFS=, read gw vip <<< "$spec"
  out=$(docker exec vpn-gateway /opt/ctraffic -address $vip:5000 -nconn 100 -timeout 10s -stats all)
  nhosts=$(echo "$out" | jq '[.ConnStats[].Host // empty] | unique | length')
  lost=$(echo "$out" | jq '.FailedConnects')
  echo "$gw TCP IPv4: $nhosts hosts, $lost lost"
done

# IPv6 TCP
for spec in "gw-a1,[2001:db8::1]" "gw-a2,[2001:db8::2]" "gw-b1,[2001:db8:1::1]" "gw-b2,[2001:db8:1::2]"; do
  IFS=, read gw vip <<< "$spec"
  out=$(docker exec vpn-gateway /opt/ctraffic -address "$vip:5000" -nconn 100 -timeout 10s -stats all)
  nhosts=$(echo "$out" | jq '[.ConnStats[].Host // empty] | unique | length')
  lost=$(echo "$out" | jq '.FailedConnects')
  echo "$gw TCP IPv6: $nhosts hosts, $lost lost"
done
```

Expected: 2 hosts per gateway, 0 lost connections.

## Verified Results

```
ICMP (8 tests):
  [PASS] ping IPv4 20.0.0.1:   0% loss
  [PASS] ping IPv4 20.0.0.2:   0% loss
  [PASS] ping IPv4 30.0.0.1:   0% loss
  [PASS] ping IPv4 30.0.0.2:   0% loss
  [PASS] ping IPv6 2001:db8::1:     0% loss
  [PASS] ping IPv6 2001:db8::2:     0% loss
  [PASS] ping IPv6 2001:db8:1::1:   0% loss
  [PASS] ping IPv6 2001:db8:1::2:   0% loss

TCP load balancing (8 tests):
  [PASS] gw-a1 TCP IPv4: 2 hosts, 0 lost
  [PASS] gw-a2 TCP IPv4: 2 hosts, 0 lost
  [PASS] gw-b1 TCP IPv4: 2 hosts, 0 lost
  [PASS] gw-b2 TCP IPv4: 2 hosts, 0 lost
  [PASS] gw-a1 TCP IPv6: 2 hosts, 0 lost
  [PASS] gw-a2 TCP IPv6: 2 hosts, 0 lost
  [PASS] gw-b1 TCP IPv6: 2 hosts, 0 lost
  [PASS] gw-b2 TCP IPv6: 2 hosts, 0 lost

Results: 16 passed, 0 failed
```

## Known Issues

### UDP Load Balancing

UDP traffic tests are currently unreliable. NFQLB forwards the initial UDP packet
to the target correctly, but return UDP packets from some targets are dropped.
TCP works because conntrack tracks the connection state. UDP tests are marked
as Pending in the automated test suite.

### Dual-Stack LB Controller Race

The loadbalancer controller processes EndpointSlices sequentially. Since each
EndpointSlice is either IPv4 or IPv6, the last-processed address family's
routes overwrite the first. This is a race condition — sometimes both families
get routes, sometimes only one does. Needs a fix in the LB controller to
handle both address families per target identifier.
