/*
Copyright (c) 2026 OpenInfra Foundation Europe. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package loadbalancer

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/google/nftables"
	"github.com/nordix/meridio/pkg/loadbalancer/types"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	meridio2v1alpha1 "github.com/nordix/meridio-2/api/v1alpha1"
)

// Controller reconciles DistributionGroup resources to manage NFQLB instances.
//
// Architectural Pattern: Mirrors Kubernetes Service/kube-proxy model
// ┌─────────────────────────────────┬──────────────────────────────────────┐
// │ Kubernetes                      │ Meridio-2                            │
// ├─────────────────────────────────┼──────────────────────────────────────┤
// │ Service (abstract LB)           │ DistributionGroup (abstract LB)      │
// │ EndpointSlice (backends)        │ EndpointSlice (backends)             │
// │ kube-proxy (per-node agent)     │ LB controller (per-Gateway agent)    │
// │ Watches: Service (primary)      │ Watches: DistributionGroup (primary) │
// │ Implements: iptables/ipvs       │ Implements: NFQLB (Maglev)           │
// └─────────────────────────────────┴──────────────────────────────────────┘
//
// Design Decision: DistributionGroup as Primary Resource
// - Direct mapping: DistributionGroup → NFQLB instance (1:1)
// - Clear lifecycle: NFQLB instance lifecycle tied to DistributionGroup
// - Architectural consistency: Matches Service/kube-proxy pattern
// - Gateway filtering: Only reconciles DistributionGroups for this Gateway
type Controller struct {
	client.Client
	Scheme           *runtime.Scheme
	GatewayName      string
	GatewayNamespace string
	LBFactory        types.NFQueueLoadBalancerFactory
	NFTConn          *nftables.Conn
	NFTTable         *nftables.Table
	NFTChain         *nftables.Chain

	mu        sync.Mutex
	instances map[string]types.NFQueueLoadBalancer // key: DistributionGroup name
	targets   map[string]map[int][]string          // key: DistributionGroup name -> identifier -> IPs
}

const identifierOffset = 5000 // TODO: port identifierOffsetGenerator from Meridio

func (c *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logr := log.FromContext(ctx)

	// Get DistributionGroup
	distGroup := &meridio2v1alpha1.DistributionGroup{}
	if err := c.Get(ctx, req.NamespacedName, distGroup); err != nil {
		if apierrors.IsNotFound(err) {
			// DistributionGroup deleted - cleanup NFQLB instance
			c.mu.Lock()
			defer c.mu.Unlock()
			if _, exists := c.instances[req.Name]; exists {
				logr.Info("Deleting NFQLB instance for deleted DistributionGroup", "distGroup", req.Name)
				delete(c.instances, req.Name)
				delete(c.targets, req.Name)
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Filter: Only reconcile DistributionGroups for this Gateway
	if !c.belongsToGateway(ctx, distGroup) {
		logr.V(1).Info("DistributionGroup does not belong to this Gateway, skipping",
			"distGroup", distGroup.Name,
			"gateway", c.GatewayName)
		return ctrl.Result{}, nil
	}

	logr.Info("Reconciling DistributionGroup", "distGroup", distGroup.Name)

	// Reconcile NFQLB instance
	if err := c.reconcileNFQLBInstance(ctx, distGroup); err != nil {
		logr.Error(err, "Failed to reconcile NFQLB instance")
		return ctrl.Result{}, err
	}

	// Reconcile targets from EndpointSlices
	if err := c.reconcileTargets(ctx, distGroup); err != nil {
		logr.Error(err, "Failed to reconcile targets")
		return ctrl.Result{}, err
	}

	// TODO: Reconcile flows from L34Routes
	// TODO: Update nftables VIP rules
	// TODO: Write readiness file

	return ctrl.Result{}, nil
}

// belongsToGateway checks if a DistributionGroup belongs to this Gateway
// by checking if any L34Route references both this Gateway and this DistributionGroup
func (c *Controller) belongsToGateway(ctx context.Context, distGroup *meridio2v1alpha1.DistributionGroup) bool {
	l34routeList := &meridio2v1alpha1.L34RouteList{}
	if err := c.List(ctx, l34routeList, client.InNamespace(c.GatewayNamespace)); err != nil {
		return false
	}

	for i := range l34routeList.Items {
		route := &l34routeList.Items[i]

		// Check if route references this Gateway
		referencesGateway := false
		for _, parentRef := range route.Spec.ParentRefs {
			if string(parentRef.Name) == c.GatewayName &&
				(parentRef.Namespace == nil || string(*parentRef.Namespace) == c.GatewayNamespace) {
				referencesGateway = true
				break
			}
		}

		if !referencesGateway {
			continue
		}

		// Check if route references this DistributionGroup
		for _, backendRef := range route.Spec.BackendRefs {
			if backendRef.Group != nil && string(*backendRef.Group) == meridio2v1alpha1.GroupVersion.Group &&
				backendRef.Kind != nil && string(*backendRef.Kind) == "DistributionGroup" &&
				string(backendRef.Name) == distGroup.Name {
				return true
			}
		}
	}

	return false
}

func (c *Controller) reconcileNFQLBInstance(ctx context.Context, distGroup *meridio2v1alpha1.DistributionGroup) error {
	logr := log.FromContext(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Initialize maps if needed
	if c.instances == nil {
		c.instances = make(map[string]types.NFQueueLoadBalancer)
	}
	if c.targets == nil {
		c.targets = make(map[string]map[int][]string)
	}

	// Check if instance already exists
	if _, exists := c.instances[distGroup.Name]; exists {
		return nil
	}

	// Get Maglev parameters
	n := int32(32) // default MaxEndpoints
	if distGroup.Spec.Maglev != nil {
		n = distGroup.Spec.Maglev.MaxEndpoints
	}
	m := int(n * 100) // M = N × 100

	// Create NFQLB instance
	instance, err := c.LBFactory.New(distGroup.Name, m, int(n))
	if err != nil {
		return err
	}

	// Start the instance to create shared memory
	if err := instance.Start(); err != nil {
		logr.Error(err, fmt.Sprintf("failed to start NFQLB instance, distGroup: %s", distGroup.Name))
		return err
	}

	c.instances[distGroup.Name] = instance
	c.targets[distGroup.Name] = make(map[int][]string)

	logr.Info(fmt.Sprintf("Created NFQLB instance, distGroup: %s, M: %d, N: %d", distGroup.Name, m, n))
	return nil
}

func (c *Controller) reconcileTargets(ctx context.Context, distGroup *meridio2v1alpha1.DistributionGroup) error {
	logr := log.FromContext(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()

	instance, exists := c.instances[distGroup.Name]
	if !exists {
		return nil
	}

	// Get EndpointSlices for this DistributionGroup
	endpointSliceList := &discoveryv1.EndpointSliceList{}
	if err := c.List(ctx, endpointSliceList,
		client.InNamespace(c.GatewayNamespace),
		client.MatchingLabels{
			"kubernetes.io/service-name": distGroup.Name,
		}); err != nil {
		return err
	}

	// Get current targets
	currentTargets := c.targets[distGroup.Name]
	if currentTargets == nil {
		currentTargets = make(map[int][]string)
		c.targets[distGroup.Name] = currentTargets
	}

	// Build new targets map from EndpointSlices
	newTargets := make(map[int][]string)
	for _, eps := range endpointSliceList.Items {
		for _, endpoint := range eps.Endpoints {
			if endpoint.Conditions.Ready == nil || !*endpoint.Conditions.Ready {
				continue
			}
			if endpoint.Zone == nil {
				logr.V(1).Info("Endpoint missing identifier (Zone field)", "addresses", endpoint.Addresses)
				continue
			}
			identifier, err := strconv.Atoi(*endpoint.Zone)
			if err != nil {
				logr.Error(err, "Invalid identifier in Zone field", "zone", *endpoint.Zone)
				continue
			}
			newTargets[identifier] = endpoint.Addresses
		}
	}

	// Deactivate removed targets
	for identifier := range currentTargets {
		if _, exists := newTargets[identifier]; !exists {
			if err := instance.Deactivate(identifier + 1); err != nil {
				logr.Error(err, "Failed to deactivate target", "identifier", identifier)
			} else {
				logr.Info("Deactivated target", "distGroup", distGroup.Name, "identifier", identifier)
			}
		}
	}

	// Activate new targets
	for identifier, ips := range newTargets {
		if _, exists := currentTargets[identifier]; !exists {
			// NFQLB expects 1-based index, so add 1 to 0-based identifier
			// Fwmark is identifier + offset for routing
			if err := instance.Activate(identifier+1, identifier+identifierOffset); err != nil {
				logr.Error(err, "Failed to activate target", "identifier", identifier, "ips", ips)
			} else {
				logr.Info("Activated target", "distGroup", distGroup.Name, "identifier", identifier, "ips", ips)
			}
		}
	}

	// Update tracked targets
	c.targets[distGroup.Name] = newTargets

	logr.Info("Reconciled targets", "distGroup", distGroup.Name, "count", len(newTargets))
	return nil
}

func (c *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&meridio2v1alpha1.DistributionGroup{}).
		Watches(&discoveryv1.EndpointSlice{}, handler.EnqueueRequestsFromMapFunc(c.endpointSliceEnqueue)).
		Named("loadbalancer").
		Complete(c)
}

// endpointSliceEnqueue maps EndpointSlice events to DistributionGroup reconcile requests
func (c *Controller) endpointSliceEnqueue(ctx context.Context, obj client.Object) []ctrl.Request {
	// EndpointSlices are labeled with kubernetes.io/service-name = DistributionGroup name
	distGroupName := obj.GetLabels()["kubernetes.io/service-name"]
	if distGroupName == "" {
		return nil
	}

	// Only trigger if in our namespace
	if obj.GetNamespace() != c.GatewayNamespace {
		return nil
	}

	return []ctrl.Request{{
		NamespacedName: client.ObjectKey{
			Name:      distGroupName,
			Namespace: obj.GetNamespace(),
		},
	}}
}
