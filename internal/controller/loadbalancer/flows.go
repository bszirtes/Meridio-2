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

	nspAPI "github.com/nordix/meridio/api/nsp/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	meridio2v1alpha1 "github.com/nordix/meridio-2/api/v1alpha1"
)

// reconcileFlows configures NFQLB flows from L34Routes.
func (c *Controller) reconcileFlows(ctx context.Context, distGroup *meridio2v1alpha1.DistributionGroup) error {
	logr := log.FromContext(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()

	instance, exists := c.instances[distGroup.Name]
	if !exists {
		return nil
	}

	// Initialize flows map if needed
	if c.flows == nil {
		c.flows = make(map[string]map[string]*meridio2v1alpha1.L34Route)
	}
	if c.flows[distGroup.Name] == nil {
		c.flows[distGroup.Name] = make(map[string]*meridio2v1alpha1.L34Route)
	}

	// Check if DistributionGroup has endpoints before configuring flows
	hasEndpoints, err := c.hasReadyEndpoints(ctx, distGroup.Name)
	if err != nil {
		return err
	}

	if !hasEndpoints {
		logr.Info("No ready endpoints, deleting flows", "distGroup", distGroup.Name)
		return c.deleteAllFlows(ctx, instance, distGroup.Name)
	}

	// Get L34Routes for this Gateway and DistributionGroup
	newFlows, err := c.listMatchingL34Routes(ctx, distGroup)
	if err != nil {
		return err
	}

	// BUG FIX #2: Handle empty L34Route list
	if len(newFlows) == 0 {
		logr.Info("No L34Routes found, deleting all flows", "distGroup", distGroup.Name)
		if err := c.deleteAllFlows(ctx, instance, distGroup.Name); err != nil {
			logr.Error(err, "Failed to delete all flows")
		}
		// Clear nftables VIPs
		if err := c.configureNftables(ctx, distGroup.Name, []string{}); err != nil {
			logr.Error(err, "Failed to clear nftables VIPs")
		}
		return nil
	}

	// Delete removed flows first
	currentFlows := c.flows[distGroup.Name]
	var errFinal error
	for flowName := range currentFlows {
		if _, exists := newFlows[flowName]; !exists {
			flow := &nspAPI.Flow{Name: flowName}
			if err := instance.DeleteFlow(flow); err != nil {
				logr.Error(err, "Failed to delete flow", "flow", flowName)
				errFinal = fmt.Errorf("%w; failed to delete flow %s: %w", errFinal, flowName, err)
			} else {
				logr.Info("Deleted flow", "distGroup", distGroup.Name, "flow", flowName)
			}
		}
	}

	// IMPROVEMENT #5: Add/update flows BEFORE configuring nftables
	successfulFlows := make(map[string]*meridio2v1alpha1.L34Route)
	for flowName, route := range newFlows {
		flow := c.convertL34RouteToFlow(route)
		if err := instance.SetFlow(flow); err != nil {
			logr.Error(err, "Failed to set flow", "flow", flowName)
			errFinal = fmt.Errorf("%w; failed to set flow %s: %w", errFinal, flowName, err)
		} else {
			logr.Info("Configured flow", "distGroup", distGroup.Name, "flow", flowName)
			successfulFlows[flowName] = route
		}
	}

	// Configure nftables AFTER flows are ready
	vips := extractVIPs(successfulFlows)
	if err := c.configureNftables(ctx, distGroup.Name, vips); err != nil {
		logr.Error(err, "Failed to configure nftables", "distGroup", distGroup.Name)
		return fmt.Errorf("failed to configure nftables: %w", err)
	}

	// BUG FIX #3: Update tracked flows with only successful ones
	c.flows[distGroup.Name] = successfulFlows

	logr.Info("Reconciled flows", "distGroup", distGroup.Name, "count", len(successfulFlows))
	return errFinal
}

// hasReadyEndpoints checks if DistributionGroup has any ready endpoints.
func (c *Controller) hasReadyEndpoints(ctx context.Context, distGroupName string) (bool, error) {
	endpointSliceList := &discoveryv1.EndpointSliceList{}
	if err := c.List(ctx, endpointSliceList,
		client.InNamespace(c.GatewayNamespace)); err != nil {
		return false, err
	}

	// Check EndpointSlices owned by this DistributionGroup
	for _, eps := range endpointSliceList.Items {
		// Check if owned by this DistributionGroup
		isOwned := false
		for _, ownerRef := range eps.GetOwnerReferences() {
			if ownerRef.APIVersion == meridio2v1alpha1.GroupVersion.String() &&
				ownerRef.Kind == kindDistributionGroup &&
				ownerRef.Name == distGroupName &&
				ownerRef.Controller != nil && *ownerRef.Controller {
				isOwned = true
				break
			}
		}
		if !isOwned {
			continue
		}

		// Check for ready endpoints
		for _, endpoint := range eps.Endpoints {
			if endpoint.Conditions.Ready != nil && *endpoint.Conditions.Ready {
				return true, nil
			}
		}
	}
	return false, nil
}

// deleteAllFlows deletes all flows for a DistributionGroup.
func (c *Controller) deleteAllFlows(ctx context.Context, instance interface{ DeleteFlow(*nspAPI.Flow) error }, distGroupName string) error {
	logr := log.FromContext(ctx)
	currentFlows := c.flows[distGroupName]
	var errFinal error
	for flowName := range currentFlows {
		flow := &nspAPI.Flow{Name: flowName}
		if err := instance.DeleteFlow(flow); err != nil {
			logr.Error(err, "Failed to delete flow", "flow", flowName)
			errFinal = fmt.Errorf("%w; failed to delete flow %s: %w", errFinal, flowName, err)
		} else {
			logr.Info("Deleted flow", "distGroup", distGroupName, "flow", flowName)
		}
	}
	c.flows[distGroupName] = make(map[string]*meridio2v1alpha1.L34Route)
	return errFinal
}

// listMatchingL34Routes finds L34Routes that reference this Gateway and DistributionGroup.
func (c *Controller) listMatchingL34Routes(ctx context.Context, distGroup *meridio2v1alpha1.DistributionGroup) (map[string]*meridio2v1alpha1.L34Route, error) {
	logr := log.FromContext(ctx)

	l34routeList := &meridio2v1alpha1.L34RouteList{}
	if err := c.List(ctx, l34routeList, client.InNamespace(c.GatewayNamespace)); err != nil {
		return nil, err
	}

	newFlows := make(map[string]*meridio2v1alpha1.L34Route)
	for i := range l34routeList.Items {
		route := &l34routeList.Items[i]

		// Check if route references this Gateway
		if !c.referencesGateway(route) {
			continue
		}

		// Check if route references this DistributionGroup
		dgName, ok := c.referencesDistributionGroup(route, distGroup.Name)
		if !ok {
			continue
		}

		// Validate that referenced DistributionGroup exists
		var dg meridio2v1alpha1.DistributionGroup
		if err := c.Get(ctx, client.ObjectKey{Name: dgName, Namespace: c.GatewayNamespace}, &dg); err != nil {
			if apierrors.IsNotFound(err) {
				logr.Info("Skipping L34Route: DistributionGroup not found", "route", route.Name, "distributionGroup", dgName)
				continue
			}
			return nil, err
		}

		newFlows[route.Name] = route
	}

	return newFlows, nil
}

// referencesGateway checks if L34Route references this Gateway.
func (c *Controller) referencesGateway(route *meridio2v1alpha1.L34Route) bool {
	for _, parentRef := range route.Spec.ParentRefs {
		if string(parentRef.Name) == c.GatewayName &&
			(parentRef.Namespace == nil || string(*parentRef.Namespace) == c.GatewayNamespace) {
			return true
		}
	}
	return false
}

// referencesDistributionGroup checks if L34Route references the given DistributionGroup.
// Returns the DistributionGroup name and true if found.
func (c *Controller) referencesDistributionGroup(route *meridio2v1alpha1.L34Route, distGroupName string) (string, bool) {
	for _, backendRef := range route.Spec.BackendRefs {
		if backendRef.Group != nil && string(*backendRef.Group) == meridio2v1alpha1.GroupVersion.Group &&
			backendRef.Kind != nil && string(*backendRef.Kind) == "DistributionGroup" &&
			string(backendRef.Name) == distGroupName {
			return string(backendRef.Name), true
		}
	}
	return "", false
}

// convertL34RouteToFlow converts an L34Route to NFQLB Flow.
func (c *Controller) convertL34RouteToFlow(route *meridio2v1alpha1.L34Route) *nspAPI.Flow {
	flow := &nspAPI.Flow{
		Name:                  route.Name,
		Priority:              route.Spec.Priority,
		SourceSubnets:         route.Spec.SourceCIDRs,
		SourcePortRanges:      route.Spec.SourcePorts,
		DestinationPortRanges: route.Spec.DestinationPorts,
		ByteMatches:           route.Spec.ByteMatches,
	}

	// Convert protocols
	protocols := make([]string, len(route.Spec.Protocols))
	for i, p := range route.Spec.Protocols {
		protocols[i] = string(p)
	}
	flow.Protocols = protocols

	// Convert VIPs
	vips := make([]*nspAPI.Vip, len(route.Spec.DestinationCIDRs))
	for i, cidr := range route.Spec.DestinationCIDRs {
		vips[i] = &nspAPI.Vip{Address: cidr}
	}
	flow.Vips = vips

	return flow
}

// extractVIPs extracts all unique VIPs from L34Routes.
func extractVIPs(routes map[string]*meridio2v1alpha1.L34Route) []string {
	vipSet := make(map[string]struct{})
	for _, route := range routes {
		for _, vip := range route.Spec.DestinationCIDRs {
			vipSet[vip] = struct{}{}
		}
	}

	vips := make([]string, 0, len(vipSet))
	for vip := range vipSet {
		vips = append(vips, vip)
	}
	return vips
}

// configureNftables configures nftables rules for VIPs.
func (c *Controller) configureNftables(ctx context.Context, distGroupName string, vips []string) error {
	logr := log.FromContext(ctx)

	nftMgr, exists := c.nftManagers[distGroupName]
	if !exists {
		return fmt.Errorf("nftables manager not found for DistributionGroup %s", distGroupName)
	}

	if err := nftMgr.SetVIPs(vips); err != nil {
		return fmt.Errorf("failed to set VIPs in nftables: %w", err)
	}

	logr.Info("Configured nftables VIP rules", "distGroup", distGroupName, "vipCount", len(vips))
	return nil
}
