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
	instances map[string]types.NFQueueLoadBalancer             // key: DistributionGroup name
	targets   map[string]map[int][]string                      // key: DistributionGroup name -> identifier -> IPs
	flows     map[string]map[string]*meridio2v1alpha1.L34Route // key: DistributionGroup name -> L34Route name
}

const kindDistributionGroup = "DistributionGroup"

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

	// Reconcile flows from L34Routes
	if err := c.reconcileFlows(ctx, distGroup); err != nil {
		logr.Error(err, "Failed to reconcile flows")
		return ctrl.Result{}, err
	}

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
		if !c.referencesGateway(route) {
			continue
		}

		// Check if route references this DistributionGroup
		for _, backendRef := range route.Spec.BackendRefs {
			if backendRef.Group != nil && string(*backendRef.Group) == meridio2v1alpha1.GroupVersion.Group &&
				backendRef.Kind != nil && string(*backendRef.Kind) == kindDistributionGroup &&
				string(backendRef.Name) == distGroup.Name {
				return true
			}
		}
	}

	return false
}

func (c *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&meridio2v1alpha1.DistributionGroup{}).
		Watches(&discoveryv1.EndpointSlice{}, handler.EnqueueRequestsFromMapFunc(c.endpointSliceEnqueue)).
		Watches(&meridio2v1alpha1.L34Route{}, handler.EnqueueRequestsFromMapFunc(c.l34RouteEnqueue)).
		Named("loadbalancer").
		Complete(c)
}

// endpointSliceEnqueue maps EndpointSlice events to DistributionGroup reconcile requests
func (c *Controller) endpointSliceEnqueue(ctx context.Context, obj client.Object) []ctrl.Request {
	// EndpointSlices are labeled with meridio-2.nordix.org/distributiongroup = DistributionGroup name
	distGroupName := obj.GetLabels()["meridio-2.nordix.org/distributiongroup"]
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

// l34RouteEnqueue maps L34Route events to DistributionGroup reconcile requests
func (c *Controller) l34RouteEnqueue(ctx context.Context, obj client.Object) []ctrl.Request {
	route, ok := obj.(*meridio2v1alpha1.L34Route)
	if !ok {
		return nil
	}

	// Check if route references this Gateway
	if !c.referencesGateway(route) {
		return nil
	}

	// Enqueue all DistributionGroups referenced by this route
	var requests []ctrl.Request
	for _, backendRef := range route.Spec.BackendRefs {
		if backendRef.Group != nil && string(*backendRef.Group) == meridio2v1alpha1.GroupVersion.Group &&
			backendRef.Kind != nil && string(*backendRef.Kind) == "DistributionGroup" {
			requests = append(requests, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      string(backendRef.Name),
					Namespace: c.GatewayNamespace,
				},
			})
		}
	}

	return requests
}
