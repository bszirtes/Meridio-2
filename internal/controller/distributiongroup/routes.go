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

package distributiongroup

import (
	"context"

	meridio2v1alpha1 "github.com/nordix/meridio-2/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// findDGsReferencingGateways finds DistributionGroups that reference any of the given Gateways
// Handles both direct parentRefs and indirect references via L34Route backendRefs
func (r *DistributionGroupReconciler) findDGsReferencingGateways(ctx context.Context, gatewayKeys []client.ObjectKey) []ctrl.Request {
	if len(gatewayKeys) == 0 {
		return nil
	}

	gatewaySet := make(map[client.ObjectKey]bool)
	for _, key := range gatewayKeys {
		gatewaySet[key] = true
	}

	// Find L34Routes referencing these Gateways
	var routeList meridio2v1alpha1.L34RouteList
	listOpts := []client.ListOption{}
	if r.Namespace != "" {
		listOpts = append(listOpts, client.InNamespace(r.Namespace))
	}
	if err := r.List(ctx, &routeList, listOpts...); err != nil {
		return nil
	}

	// Collect DistributionGroup keys from L34Route backendRefs
	dgKeys := make(map[client.ObjectKey]bool)
	for _, route := range routeList.Items {
		// Check if route references any of the target Gateways
		referencesGateway := false
		for _, parentRef := range route.Spec.ParentRefs {
			ns := route.Namespace
			if parentRef.Namespace != nil {
				ns = string(*parentRef.Namespace)
			}
			if gatewaySet[client.ObjectKey{Namespace: ns, Name: string(parentRef.Name)}] {
				referencesGateway = true
				break
			}
		}

		if !referencesGateway {
			continue
		}

		// Extract DistributionGroups from backendRefs
		for key := range extractDGsFromBackendRefs(route.Spec.BackendRefs, route.Namespace) {
			dgKeys[key] = true
		}
	}

	// Find DistributionGroups with direct parentRefs to these Gateways
	// and also check indirectly referenced DGs availability before enqueue
	var dgList meridio2v1alpha1.DistributionGroupList
	listOpts = []client.ListOption{}
	if r.Namespace != "" {
		listOpts = append(listOpts, client.InNamespace(r.Namespace))
	}
	if err := r.List(ctx, &dgList, listOpts...); err != nil {
		return nil
	}

	requestMap := make(map[client.ObjectKey]bool)
	for _, dg := range dgList.Items {
		key := client.ObjectKeyFromObject(&dg)

		// Check direct parentRef
		for _, parentRef := range dg.Spec.ParentRefs {
			ns := dg.Namespace
			if parentRef.Namespace != nil {
				ns = *parentRef.Namespace
			}
			if gatewaySet[client.ObjectKey{Namespace: ns, Name: parentRef.Name}] {
				requestMap[key] = true
				break
			}
		}

		// Check indirect reference via L34Route
		if dgKeys[key] {
			requestMap[key] = true
		}
	}

	requests := make([]ctrl.Request, 0, len(requestMap))
	for key := range requestMap {
		requests = append(requests, ctrl.Request{NamespacedName: key})
	}
	return requests
}

// listRoutesReferencingDG finds L34Routes that reference this DistributionGroup in backendRefs
func (r *DistributionGroupReconciler) listRoutesReferencingDG(ctx context.Context, dg *meridio2v1alpha1.DistributionGroup) ([]meridio2v1alpha1.L34Route, error) {
	var routeList meridio2v1alpha1.L34RouteList
	listOpts := []client.ListOption{}

	if r.Namespace != "" {
		listOpts = append(listOpts, client.InNamespace(r.Namespace))
	}

	if err := r.List(ctx, &routeList, listOpts...); err != nil {
		return nil, err
	}

	// Filter routes that reference this DG in backendRefs
	// NOTE: Ne referenceGrant like policy is enforced if controller namespace is not restricted
	var matchingRoutes []meridio2v1alpha1.L34Route
	for _, route := range routeList.Items {
		for _, backendRef := range route.Spec.BackendRefs {
			if backendRefMatchesDG(backendRef, route.Namespace, client.ObjectKeyFromObject(dg)) {
				matchingRoutes = append(matchingRoutes, route)
				break
			}
		}
	}

	return matchingRoutes, nil
}

// extractDGsFromBackendRefs extracts DistributionGroup ObjectKeys from BackendRefs
func extractDGsFromBackendRefs(backendRefs []gatewayv1.BackendRef, localNs string) map[client.ObjectKey]bool {
	dgKeys := make(map[client.ObjectKey]bool)
	for _, backendRef := range backendRefs {
		group := gatewayv1.GroupName
		if backendRef.Group != nil {
			group = string(*backendRef.Group)
		}
		kind := kindService
		if backendRef.Kind != nil {
			kind = string(*backendRef.Kind)
		}
		ns := localNs
		if backendRef.Namespace != nil {
			ns = string(*backendRef.Namespace)
		}

		if group == meridio2v1alpha1.GroupVersion.Group && kind == kindDistributionGroup {
			dgKeys[client.ObjectKey{Namespace: ns, Name: string(backendRef.Name)}] = true
		}
	}
	return dgKeys
}

// backendRefMatchesDG checks if a BackendRef references a specific DistributionGroup
func backendRefMatchesDG(backendRef gatewayv1.BackendRef, localNs string, dgKey client.ObjectKey) bool {
	group := gatewayv1.GroupName
	if backendRef.Group != nil {
		group = string(*backendRef.Group)
	}
	kind := kindService
	if backendRef.Kind != nil {
		kind = string(*backendRef.Kind)
	}
	ns := localNs
	if backendRef.Namespace != nil {
		ns = string(*backendRef.Namespace)
	}

	return group == meridio2v1alpha1.GroupVersion.Group &&
		kind == kindDistributionGroup &&
		ns == dgKey.Namespace &&
		string(backendRef.Name) == dgKey.Name
}
