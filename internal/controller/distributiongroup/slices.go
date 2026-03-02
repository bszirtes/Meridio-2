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
	"net"
	"sort"
	"strconv"

	meridio2v1alpha1 "github.com/nordix/meridio-2/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// listOwnedSlices returns EndpointSlices owned by the DistributionGroup
func (r *DistributionGroupReconciler) listOwnedSlices(ctx context.Context, dg *meridio2v1alpha1.DistributionGroup) ([]discoveryv1.EndpointSlice, error) {
	var sliceList discoveryv1.EndpointSliceList
	if err := r.List(ctx, &sliceList, client.InNamespace(dg.Namespace)); err != nil {
		return nil, err
	}

	var owned []discoveryv1.EndpointSlice
	for _, slice := range sliceList.Items {
		if metav1.IsControlledBy(&slice, dg) {
			owned = append(owned, slice)
		}
	}

	return owned, nil
}

// deleteAllOwnedSlices deletes all EndpointSlices owned by the DistributionGroup
func (r *DistributionGroupReconciler) deleteAllOwnedSlices(ctx context.Context, dg *meridio2v1alpha1.DistributionGroup) error {
	slices, err := r.listOwnedSlices(ctx, dg)
	if err != nil {
		return err
	}

	for i := range slices {
		if err := r.Delete(ctx, &slices[i]); err != nil {
			return client.IgnoreNotFound(err)
		}
	}

	return nil
}

// calculateDesiredSlices computes the desired EndpointSlices
func (r *DistributionGroupReconciler) calculateDesiredSlices(ctx context.Context, dg *meridio2v1alpha1.DistributionGroup, pods []corev1.Pod, networkContexts map[string]string, existingSlices []discoveryv1.EndpointSlice) ([]discoveryv1.EndpointSlice, *maglevCapacityInfo) {
	if len(networkContexts) == 0 {
		return nil, nil
	}

	// Group existing slices by network once (O(M) instead of O(N×M))
	slicesByNetwork := groupSlicesByNetwork(ctx, existingSlices)

	// Filter Pods per network (common for all DG types)
	podsByNetwork := make(map[string][]podWithNetworkIP)
	for cidr, attachmentType := range networkContexts {
		podsWithIP := r.filterPodsWithNetworkContextIP(pods, cidr, attachmentType)
		if len(podsWithIP) > 0 {
			podsByNetwork[cidr] = podsWithIP
		}
	}

	// Early exit if no Pods have IPs in any network
	if len(podsByNetwork) == 0 {
		return nil, nil
	}

	// Maglev-specific assignment logic
	if dg.Spec.Type == meridio2v1alpha1.DistributionGroupTypeMaglev {
		return r.calculateMaglevSlices(dg, podsByNetwork, slicesByNetwork)
	}

	// Non-Maglev: no capacity restrictions, no stable IDs
	var desiredSlices []discoveryv1.EndpointSlice
	for cidr, podsWithIP := range podsByNetwork {
		existingForNetwork := slicesByNetwork[cidr]
		slices := createSlicesForNetwork(dg, podsWithIP, nil, cidr, existingForNetwork)
		desiredSlices = append(desiredSlices, slices...)
	}
	return desiredSlices, nil
}

// calculateMaglevSlices handles Maglev-specific endpoint assignment with stable IDs
func (r *DistributionGroupReconciler) calculateMaglevSlices(dg *meridio2v1alpha1.DistributionGroup, podsByNetwork map[string][]podWithNetworkIP, slicesByNetwork map[string][]discoveryv1.EndpointSlice) ([]discoveryv1.EndpointSlice, *maglevCapacityInfo) {
	// Determine maxEndpoints (default to 32 if MaglevConfig is nil or MaxEndpoints is 0)
	maxEndpoints := meridio2v1alpha1.DefaultMaglevMaxEndpoints
	if dg.Spec.Maglev != nil && dg.Spec.Maglev.MaxEndpoints > 0 {
		maxEndpoints = dg.Spec.Maglev.MaxEndpoints
	}

	var desiredSlices []discoveryv1.EndpointSlice
	capacityInfo := &maglevCapacityInfo{
		networkIssues: make(map[string]struct{ excluded, total int32 }),
	}

	for cidr, podsWithIP := range podsByNetwork {
		existingForNetwork := slicesByNetwork[cidr]

		// Extract existing Pod→ID assignments for this network
		existingAssignments := extractMaglevAssignments(existingForNetwork)

		// Assign Maglev IDs for this network context
		podToID := assignMaglevIDs(podsWithIP, existingAssignments, maxEndpoints)

		// Track capacity issues
		total := int32(len(podsWithIP))
		assigned := int32(len(podToID))
		if assigned < total {
			capacityInfo.networkIssues[cidr] = struct{ excluded, total int32 }{
				excluded: total - assigned,
				total:    total,
			}
		}

		// Create slices for this network
		slices := createSlicesForNetwork(dg, podsWithIP, podToID, cidr, existingForNetwork)
		desiredSlices = append(desiredSlices, slices...)
	}

	return desiredSlices, capacityInfo
}

// reconcileSlices creates, updates, or deletes EndpointSlices to match desired state
func (r *DistributionGroupReconciler) reconcileSlices(ctx context.Context, dg *meridio2v1alpha1.DistributionGroup, desired, existing []discoveryv1.EndpointSlice) error {
	// Build maps for efficient lookup
	desiredByName := make(map[string]*discoveryv1.EndpointSlice)
	for i := range desired {
		desiredByName[desired[i].Name] = &desired[i]
	}

	existingByName := make(map[string]*discoveryv1.EndpointSlice)
	for i := range existing {
		existingByName[existing[i].Name] = &existing[i]
	}

	// Create or update slices
	for name, desiredSlice := range desiredByName {
		existingSlice, exists := existingByName[name]

		if !exists {
			// Create new slice
			slice := desiredSlice.DeepCopy()
			if err := ctrl.SetControllerReference(dg, slice, r.Scheme); err != nil {
				return err
			}
			if err := r.Create(ctx, slice); err != nil {
				return err
			}
		} else {
			// Update if endpoints, labels, or addressType changed
			if !endpointSliceNeedsUpdate(existingSlice, desiredSlice) {
				continue
			}

			slice := existingSlice.DeepCopy()
			slice.AddressType = desiredSlice.AddressType
			slice.Endpoints = desiredSlice.Endpoints
			slice.Labels = desiredSlice.Labels
			if err := r.Update(ctx, slice); err != nil {
				return err
			}
		}
	}

	// Delete orphaned slices
	for name, existingSlice := range existingByName {
		if _, desired := desiredByName[name]; !desired {
			if err := r.Delete(ctx, existingSlice); err != nil {
				return client.IgnoreNotFound(err)
			}
		}
	}

	return nil
}

// endpointSliceNeedsUpdate checks if EndpointSlice needs update using semantic equality
func endpointSliceNeedsUpdate(existing, desired *discoveryv1.EndpointSlice) bool {
	return existing.AddressType != desired.AddressType ||
		!apiequality.Semantic.DeepEqual(existing.Endpoints, desired.Endpoints) ||
		!apiequality.Semantic.DeepEqual(existing.Labels, desired.Labels)
}

// groupSlicesByNetwork groups EndpointSlices by network-subnet label
// Returns map keyed by actual CIDR (decoded from label)
func groupSlicesByNetwork(ctx context.Context, slices []discoveryv1.EndpointSlice) map[string][]discoveryv1.EndpointSlice {
	logger := log.FromContext(ctx)
	grouped := make(map[string][]discoveryv1.EndpointSlice)
	for _, slice := range slices {
		if slice.Labels != nil {
			if encodedCIDR, ok := slice.Labels[labelNetworkSubnet]; ok {
				cidr := decodeCIDRFromLabel(encodedCIDR)
				// Normalize to handle tampered/weird labels
				normalized, err := normalizeCIDR(cidr)
				if err != nil {
					logger.Info("Skipping EndpointSlice with invalid network-subnet label", "slice", slice.Name, "label", cidr, "error", err)
					continue
				}
				grouped[normalized] = append(grouped[normalized], slice)
			}
		}
	}
	return grouped
}

// createSlicesForNetwork creates EndpointSlices for a specific network context
func createSlicesForNetwork(dg *meridio2v1alpha1.DistributionGroup, podsWithIP []podWithNetworkIP, podToID map[string]int32, cidr string, existingSlicesForNetwork []discoveryv1.EndpointSlice) []discoveryv1.EndpointSlice {
	// Detect address type from CIDR
	_, ipnet, err := net.ParseCIDR(cidr)
	addressType := discoveryv1.AddressTypeIPv4
	if err == nil && ipnet.IP.To4() == nil {
		addressType = discoveryv1.AddressTypeIPv6
	}

	// Build endpoint map: Pod UID → Endpoint
	// For Maglev: only include Pods with assigned IDs (capacity enforcement)
	endpointMap := make(map[string]discoveryv1.Endpoint)
	for _, pwip := range podsWithIP {
		if podToID != nil {
			// Skip Pods without Maglev IDs (capacity exceeded)
			if _, hasID := podToID[string(pwip.pod.UID)]; !hasID {
				continue
			}
		}

		endpoint := discoveryv1.Endpoint{
			Addresses: []string{pwip.ip},
			TargetRef: &corev1.ObjectReference{
				Kind:      kindPod,
				Namespace: pwip.pod.Namespace,
				Name:      pwip.pod.Name,
				UID:       pwip.pod.UID,
			},
			Conditions: discoveryv1.EndpointConditions{
				Ready: ptr(isPodReady(&pwip.pod)),
			},
		}

		// Set zone field for Maglev
		if podToID != nil {
			if id, exists := podToID[string(pwip.pod.UID)]; exists {
				zone := "maglev:" + strconv.FormatInt(int64(id), 10)
				endpoint.Zone = &zone
			}
		}

		endpointMap[string(pwip.pod.UID)] = endpoint
	}

	// Preserve structure: map existing slices to their endpoints
	type sliceWithEndpoints struct {
		slice     discoveryv1.EndpointSlice
		endpoints []discoveryv1.Endpoint
	}
	var slices []sliceWithEndpoints

	// Fill existing slices first (structure preservation)
	for _, existingSlice := range existingSlicesForNetwork {
		var endpoints []discoveryv1.Endpoint
		for _, ep := range existingSlice.Endpoints {
			if ep.TargetRef == nil {
				continue
			}
			if newEp, exists := endpointMap[string(ep.TargetRef.UID)]; exists {
				endpoints = append(endpoints, newEp)
				delete(endpointMap, string(ep.TargetRef.UID))
			}
		}
		if len(endpoints) > 0 {
			slices = append(slices, sliceWithEndpoints{
				slice:     existingSlice,
				endpoints: endpoints,
			})
		}
	}

	// Collect remaining endpoints and sort deterministically by Pod UID
	remainingEndpoints := make([]discoveryv1.Endpoint, 0, len(endpointMap))
	for _, ep := range endpointMap {
		remainingEndpoints = append(remainingEndpoints, ep)
	}
	sort.Slice(remainingEndpoints, func(i, j int) bool {
		return remainingEndpoints[i].TargetRef.UID < remainingEndpoints[j].TargetRef.UID
	})

	// Fill remaining capacity in existing slices
	for i := range slices {
		capacity := maxEndpointsPerSlice - len(slices[i].endpoints)
		if capacity > 0 && len(remainingEndpoints) > 0 {
			toAdd := min(capacity, len(remainingEndpoints))
			slices[i].endpoints = append(slices[i].endpoints, remainingEndpoints[:toAdd]...)
			remainingEndpoints = remainingEndpoints[toAdd:]
		}
	}

	// Create new slices for remaining endpoints
	for len(remainingEndpoints) > 0 {
		toAdd := min(maxEndpointsPerSlice, len(remainingEndpoints))
		slices = append(slices, sliceWithEndpoints{
			endpoints: remainingEndpoints[:toAdd],
		})
		remainingEndpoints = remainingEndpoints[toAdd:]
	}

	// Build final slices with labels and metadata
	result := make([]discoveryv1.EndpointSlice, 0, len(slices))
	for i, s := range slices {
		slice := s.slice
		if slice.Name == "" {
			// New slice - generate name
			slice.Name = dg.Name + "-" + hashCIDR(cidr) + "-" + strconv.Itoa(i)
			slice.Namespace = dg.Namespace
		}

		slice.AddressType = addressType
		slice.Endpoints = s.endpoints
		slice.Labels = map[string]string{
			labelManagedBy:         managedByValue,
			labelDistributionGroup: dg.Name,
			labelNetworkSubnet:     encodeCIDRForLabel(cidr),
		}

		result = append(result, slice)
	}

	return result
}
