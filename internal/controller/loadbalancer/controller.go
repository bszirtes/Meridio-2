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

	"github.com/google/nftables"
	"github.com/nordix/meridio/pkg/loadbalancer/types"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	meridio2v1alpha1 "github.com/nordix/meridio-2/api/v1alpha1"
)

// Controller reconciles Gateway resources to manage NFQLB and nftables
type Controller struct {
	client.Client
	Scheme           *runtime.Scheme
	GatewayName      string
	GatewayNamespace string
	LBFactory        types.NFQueueLoadBalancerFactory
	NFTConn          *nftables.Conn
	NFTTable         *nftables.Table
	NFTChain         *nftables.Chain
}

func (c *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logr := log.FromContext(ctx)

	// Filter: only reconcile our Gateway
	if req.Name != c.GatewayName || req.Namespace != c.GatewayNamespace {
		return ctrl.Result{}, nil
	}

	gateway := &gatewayv1.Gateway{}
	if err := c.Get(ctx, req.NamespacedName, gateway); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logr.Info("Reconciling Gateway", "gateway", gateway.Name)

	// TODO: Reconcile L34Routes
	// TODO: Reconcile DistributionGroups
	// TODO: Reconcile EndpointSlices

	return ctrl.Result{}, nil
}

func (c *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&gatewayv1.Gateway{}).
		Watches(&meridio2v1alpha1.L34Route{}, handler.EnqueueRequestsFromMapFunc(c.l34RouteEnqueue)).
		Watches(&meridio2v1alpha1.DistributionGroup{}, handler.EnqueueRequestsFromMapFunc(c.distributionGroupEnqueue)).
		Watches(&discoveryv1.EndpointSlice{}, handler.EnqueueRequestsFromMapFunc(c.endpointSliceEnqueue)).
		Named("loadbalancer").
		Complete(c)
}

// l34RouteEnqueue maps L34Route events to Gateway reconcile requests
func (c *Controller) l34RouteEnqueue(ctx context.Context, obj client.Object) []ctrl.Request {
	l34route := obj.(*meridio2v1alpha1.L34Route)

	// Check if this L34Route references our Gateway
	for _, parentRef := range l34route.Spec.ParentRefs {
		if string(parentRef.Name) == c.GatewayName &&
			(parentRef.Namespace == nil || string(*parentRef.Namespace) == c.GatewayNamespace) {
			return []ctrl.Request{{
				NamespacedName: client.ObjectKey{
					Name:      c.GatewayName,
					Namespace: c.GatewayNamespace,
				},
			}}
		}
	}
	return nil
}

// distributionGroupEnqueue maps DistributionGroup events to Gateway reconcile requests
func (c *Controller) distributionGroupEnqueue(ctx context.Context, obj client.Object) []ctrl.Request {
	// TODO: Check if this DistributionGroup is referenced by any L34Route for our Gateway
	// For now, trigger reconcile for all DistributionGroup changes
	return []ctrl.Request{{
		NamespacedName: client.ObjectKey{
			Name:      c.GatewayName,
			Namespace: c.GatewayNamespace,
		},
	}}
}

// endpointSliceEnqueue maps EndpointSlice events to Gateway reconcile requests
func (c *Controller) endpointSliceEnqueue(ctx context.Context, obj client.Object) []ctrl.Request {
	// TODO: Check if this EndpointSlice belongs to a DistributionGroup for our Gateway
	// For now, trigger reconcile for all EndpointSlice changes in our namespace
	if obj.GetNamespace() == c.GatewayNamespace {
		return []ctrl.Request{{
			NamespacedName: client.ObjectKey{
				Name:      c.GatewayName,
				Namespace: c.GatewayNamespace,
			},
		}}
	}
	return nil
}
