# Meridio-2 API Reference

## API Groups

**Correct API Group**: `meridio-2.nordix.org/v1alpha1`

❌ **Wrong**: `meridio.nordix.org/v1alpha1`

## Custom Resources

### DistributionGroup

```yaml
apiVersion: meridio-2.nordix.org/v1alpha1
kind: DistributionGroup
metadata:
  name: my-backends
  namespace: default
spec:
  type: Maglev
  maglev:
    maxEndpoints: 32  # N parameter (M = N × 100)
```

### L34Route

```yaml
apiVersion: meridio-2.nordix.org/v1alpha1
kind: L34Route
metadata:
  name: my-route
  namespace: default
spec:
  parentRefs:
  - name: my-gateway
    namespace: default
  destinationCIDRs:
  - 20.0.0.1/32
  protocols:
  - TCP
  destinationPorts:
  - "80"
  priority: 100  # Required: >= 1, higher value = higher priority
  backendRefs:
  - group: meridio-2.nordix.org  # Must match API group
    kind: DistributionGroup
    name: my-backends
```

### EndpointSlice (with identifier)

```yaml
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: my-backends-endpoints
  namespace: default
  labels:
    kubernetes.io/service-name: my-backends  # Must match DistributionGroup name
    discovery.k8s.io/managed-by: distributiongroup-controller
    meridio-2.nordix.org/distributiongroup: my-backends
addressType: IPv4
endpoints:
- addresses:
  - 10.244.1.10
  conditions:
    ready: true
  zone: "maglev:0"  # Maglev identifier (format: "maglev:N" where N=0-31)
ports:
- port: 80
  protocol: TCP
```

**Note**: In production, EndpointSlices are automatically created by the DistributionGroup controller. This example shows the expected format for manual testing.

## Gateway API Resources

### GatewayClass

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: meridio-stateless-lb
spec:
  controllerName: nordix.org/meridio-2-gateway-controller
```

### Gateway

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: my-gateway
  namespace: default
spec:
  gatewayClassName: meridio-stateless-lb
  listeners:
  - name: tcp
    protocol: TCP
    port: 8080
```

## Common Errors

### Missing Priority

❌ Error:
```
spec.priority: Required value
```

✅ Fix: Add priority field (must be >= 1):
```yaml
spec:
  priority: 100  # Higher value = higher priority
```

### Wrong API Group

❌ Error:
```
no matches for kind "DistributionGroup" in version "meridio.nordix.org/v1alpha1"
```

✅ Fix: Use `meridio-2.nordix.org/v1alpha1`

### Missing Identifier

❌ Error: Targets not activating

✅ Fix: Add `zone` field to EndpointSlice endpoints with Maglev format:
```yaml
endpoints:
- addresses: [10.0.0.1]
  zone: "maglev:0"  # Required for NFQLB (format: "maglev:N" where N=0-31)
```

**Note**: The DistributionGroup controller automatically creates EndpointSlices with this format. Manual EndpointSlices should follow the same convention.

### Wrong Label

❌ Error: EndpointSlice not found

✅ Fix: Label must match DistributionGroup name:
```yaml
metadata:
  labels:
    kubernetes.io/service-name: <distgroup-name>
```
