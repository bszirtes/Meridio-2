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
	Scheme            *runtime.Scheme
	GatewayName       string
	GatewayNamespace  string
	LBFactory         types.NFQueueLoadBalancerFactory
	NFTConn           *nftables.Conn
	NFTTable          *nftables.Table
	NFTChain          *nftables.Chain
	NftManagerFactory func(distGroupName string, queueNum, queueTotal uint16) (nftablesManager, error)

	mu             sync.Mutex
	instances      map[string]types.NFQueueLoadBalancer             // key: DistributionGroup name
	nftManagers    map[string]nftablesManager                       // key: DistributionGroup name
	routingManager *RoutingManager                                  // Manages policy routing for all targets
	targets        map[string]map[int][]string                      // key: DistributionGroup name -> identifier -> IPs
	flows          map[string]map[string]*meridio2v1alpha1.L34Route // key: DistributionGroup name -> L34Route name
}

// nftablesManager interface for nftables operations
type nftablesManager interface {
	Setup() error
	SetVIPs(cidrs []string) error
	Cleanup() error
}

const kindDistributionGroup = "DistributionGroup"

func (c *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logr := log.FromContext(ctx)

	// Get DistributionGroup
	distGroup := &meridio2v1alpha1.DistributionGroup{}
	if err := c.Get(ctx, req.NamespacedName, distGroup); err != nil {
		if apierrors.IsNotFound(err) {
			// DistributionGroup deleted - cleanup NFQLB instance and nftables
			logr.Info("DistributionGroup deleted, cleaning up resources", "distGroup", req.Name)
			return c.cleanupDistributionGroup(ctx, req.Name)
		}
		return ctrl.Result{}, err
	}

	// Filter: Only reconcile DistributionGroups for this Gateway
	if !c.belongsToGateway(ctx, distGroup) {
		// Check if we previously managed this DistributionGroup
		c.mu.Lock()
		_, wasManaged := c.instances[distGroup.Name]
		c.mu.Unlock()

		if wasManaged {
			// DistributionGroup moved to another Gateway - cleanup local resources
			logr.Info("DistributionGroup moved to another Gateway, cleaning up local resources",
				"distGroup", distGroup.Name,
				"gateway", c.GatewayName)
			return c.cleanupDistributionGroup(ctx, distGroup.Name)
		}

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

	// TODO: Write readiness file

	return ctrl.Result{}, nil
}

// cleanupDistributionGroup removes all local resources for a DistributionGroup.
// Used when DG is deleted or moved to another Gateway.
func (c *Controller) cleanupDistributionGroup(ctx context.Context, distGroupName string) (ctrl.Result, error) {
	logr := log.FromContext(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Cleanup NFQLB instance
	if instance, exists := c.instances[distGroupName]; exists {
		logr.Info("Deleting NFQLB instance", "distGroup", distGroupName)
		if err := instance.Delete(); err != nil {
			logr.Error(err, "Failed to delete NFQLB instance", "distGroup", distGroupName)
		}
		delete(c.instances, distGroupName)
		delete(c.targets, distGroupName)
		delete(c.flows, distGroupName)
	}

	// Cleanup nftables
	if nftMgr, exists := c.nftManagers[distGroupName]; exists {
		if err := nftMgr.Cleanup(); err != nil {
			logr.Error(err, "Failed to cleanup nftables", "distGroup", distGroupName)
		}
		delete(c.nftManagers, distGroupName)
	}

	// Remove readiness file
	if err := c.removeReadinessFile(distGroupName); err != nil {
		logr.Error(err, "Failed to remove readiness file", "distGroup", distGroupName)
	}

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
			// Default Group to "" (core API group) when unspecified
			group := ""
			if backendRef.Group != nil {
				group = string(*backendRef.Group)
			}

			// Default Kind to "Service" when unspecified
			kind := "Service"
			if backendRef.Kind != nil {
				kind = string(*backendRef.Kind)
			}

			// Default Namespace to Route's namespace when unspecified
			namespace := route.Namespace
			if backendRef.Namespace != nil {
				namespace = string(*backendRef.Namespace)
			}

			// Check if this backendRef matches our DistributionGroup
			if group == meridio2v1alpha1.GroupVersion.Group &&
				kind == kindDistributionGroup &&
				string(backendRef.Name) == distGroup.Name &&
				namespace == distGroup.Namespace {
				return true
			}
		}
	}

	return false
}

func (c *Controller) SetupWithManager(mgr ctrl.Manager) error {
	// Initialize routing manager
	c.routingManager = NewRoutingManager()

	// Clean up readiness directory on startup
	if err := c.cleanupReadinessDir(); err != nil {
		return fmt.Errorf("failed to cleanup readiness directory: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&meridio2v1alpha1.DistributionGroup{}).
		Watches(&discoveryv1.EndpointSlice{}, handler.EnqueueRequestsFromMapFunc(c.endpointSliceEnqueue)).
		Watches(&meridio2v1alpha1.L34Route{}, handler.EnqueueRequestsFromMapFunc(c.l34RouteEnqueue)).
		Named("loadbalancer").
		Complete(c)
}

// endpointSliceEnqueue maps EndpointSlice events to DistributionGroup reconcile requests
func (c *Controller) endpointSliceEnqueue(ctx context.Context, obj client.Object) []ctrl.Request {
	// EndpointSlices have OwnerReference to their DistributionGroup
	for _, ownerRef := range obj.GetOwnerReferences() {
		if ownerRef.APIVersion == meridio2v1alpha1.GroupVersion.String() &&
			ownerRef.Kind == kindDistributionGroup &&
			ownerRef.Controller != nil && *ownerRef.Controller {
			// Only trigger if in our namespace
			if obj.GetNamespace() != c.GatewayNamespace {
				return nil
			}

			return []ctrl.Request{{
				NamespacedName: client.ObjectKey{
					Name:      ownerRef.Name,
					Namespace: obj.GetNamespace(),
				},
			}}
		}
	}

	return nil
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
		// Default Group to "" (core API group) when unspecified
		group := ""
		if backendRef.Group != nil {
			group = string(*backendRef.Group)
		}

		// Default Kind to "Service" when unspecified
		kind := "Service"
		if backendRef.Kind != nil {
			kind = string(*backendRef.Kind)
		}

		// Default Namespace to Route's namespace when unspecified
		namespace := route.Namespace
		if backendRef.Namespace != nil {
			namespace = string(*backendRef.Namespace)
		}

		// Check if this backendRef is a DistributionGroup
		if group == meridio2v1alpha1.GroupVersion.Group &&
			kind == kindDistributionGroup {
			requests = append(requests, ctrl.Request{
				NamespacedName: client.ObjectKey{
					Name:      string(backendRef.Name),
					Namespace: namespace,
				},
			})
		}
	}

	return requests
}
