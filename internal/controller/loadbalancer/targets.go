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
	"hash/fnv"
	"strconv"

	discoveryv1 "k8s.io/api/discovery/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	meridio2v1alpha1 "github.com/nordix/meridio-2/api/v1alpha1"
)

const (
	baseOffset       = 5000 // Base offset for all fwmarks
	offsetMultiplier = 1024 // Offset multiplier per DistributionGroup
)

// getFwmarkOffset calculates the fwmark offset for a DistributionGroup
// Formula: DistributionGroup_ID * 1024 + 5000
// DistributionGroup_ID is derived from a hash of the DG name
func getFwmarkOffset(distGroupName string) int {
	// Generate stable ID from name using FNV hash
	h := fnv.New32a()
	h.Write([]byte(distGroupName))
	dgID := int(h.Sum32() % 1024) // Limit to 0-1023 to prevent huge offsets

	return dgID*offsetMultiplier + baseOffset
}

// reconcileTargets synchronizes NFQLB targets from EndpointSlices.
func (c *Controller) reconcileTargets(ctx context.Context, distGroup *meridio2v1alpha1.DistributionGroup) error {
	logr := log.FromContext(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()

	instance, exists := c.instances[distGroup.Name]
	if !exists {
		return nil
	}

	// Calculate fwmark offset for this DistributionGroup
	fwmarkOffset := getFwmarkOffset(distGroup.Name)

	// Get EndpointSlices for this DistributionGroup
	endpointSliceList := &discoveryv1.EndpointSliceList{}
	if err := c.List(ctx, endpointSliceList,
		client.InNamespace(c.GatewayNamespace),
		client.MatchingLabels{
			"meridio-2.nordix.org/distribution-group": distGroup.Name,
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

			// Parse Zone field - expected format: "maglev:N"
			zoneStr := *endpoint.Zone
			if len(zoneStr) < 8 || zoneStr[:7] != "maglev:" {
				logr.Error(nil, "Invalid Zone format, expected 'maglev:N'", "zone", zoneStr)
				continue
			}

			identifier, err := strconv.Atoi(zoneStr[7:])
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
			index := identifier + 1 // NFQLB uses 1-based indexing
			fwmark := identifier + fwmarkOffset
			if err := instance.Deactivate(index); err != nil {
				logr.Error(err, "Failed to deactivate target", "identifier", identifier)
			} else {
				logr.Info("Deactivated target", "distGroup", distGroup.Name, "identifier", identifier)
			}
			// Remove routing for this target
			if err := c.routingManager.DeleteRoute(fwmark); err != nil {
				logr.Error(err, "Failed to delete route", "fwmark", fwmark)
			}
		}
	}

	// Activate new/updated targets
	for identifier, ips := range newTargets {
		index := identifier + 1             // NFQLB uses 1-based indexing
		fwmark := identifier + fwmarkOffset // fwmark = identifier + offset

		// Configure routing BEFORE activating target to prevent traffic loss
		if len(ips) > 0 {
			if err := c.routingManager.AddRoute(fwmark, ips[0]); err != nil {
				logr.Error(err, "Failed to add route", "fwmark", fwmark, "targetIP", ips[0])
				continue // Skip activation if routing fails
			}
		}

		// Now activate target in NFQLB
		if err := instance.Activate(index, fwmark); err != nil {
			logr.Error(err, "Failed to activate target", "identifier", identifier, "ips", ips)
			// Cleanup routing on activation failure
			_ = c.routingManager.DeleteRoute(fwmark)
		} else {
			logr.Info("Activated target", "distGroup", distGroup.Name, "identifier", identifier, "ips", ips)
		}
	}

	// Update tracked targets
	c.targets[distGroup.Name] = newTargets

	// Manage readiness file based on endpoint count
	if len(newTargets) > 0 {
		// At least one endpoint ready - create readiness file
		if err := c.createReadinessFile(distGroup.Name); err != nil {
			logr.Error(err, "Failed to create readiness file", "distGroup", distGroup.Name)
		}
	} else {
		// No endpoints ready - remove readiness file
		if err := c.removeReadinessFile(distGroup.Name); err != nil {
			logr.Error(err, "Failed to remove readiness file", "distGroup", distGroup.Name)
		}
	}

	logr.Info("Reconciled targets", "distGroup", distGroup.Name, "count", len(newTargets))
	return nil
}
