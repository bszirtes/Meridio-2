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
	"strconv"
	"strings"

	discoveryv1 "k8s.io/api/discovery/v1"
)

// assignMaglevIDs assigns stable Maglev IDs to Pods for a specific network
func assignMaglevIDs(podsWithIP []podWithNetworkIP, existingAssignments map[string]int32, maxEndpoints int32) map[string]int32 {
	podToID := make(map[string]int32)
	usedIDs := make(map[int32]bool)
	var newPods []podWithNetworkIP

	// Preserve existing assignments
	for _, pwip := range podsWithIP {
		if id, exists := existingAssignments[string(pwip.pod.UID)]; exists {
			podToID[string(pwip.pod.UID)] = id
			usedIDs[id] = true
		} else {
			newPods = append(newPods, pwip)
		}
	}

	// Sort new Pods by CreationTimestamp, tiebreak by namespace/name
	sortPodsByCreationTime(newPods)

	// Build list of available IDs
	availableIDs := make([]int32, 0, maxEndpoints)
	for id := range maxEndpoints {
		if !usedIDs[id] {
			availableIDs = append(availableIDs, id)
		}
	}

	// Assign IDs to new Pods from available pool
	for i, pwip := range newPods {
		if i >= len(availableIDs) {
			break // Capacity exceeded
		}
		podToID[string(pwip.pod.UID)] = availableIDs[i]
	}

	return podToID
}

// extractMaglevAssignments extracts Pod UID → Maglev ID mappings from EndpointSlices
func extractMaglevAssignments(slices []discoveryv1.EndpointSlice) map[string]int32 {
	assignments := make(map[string]int32)
	for _, slice := range slices {
		for _, endpoint := range slice.Endpoints {
			if endpoint.TargetRef == nil || endpoint.TargetRef.Kind != kindPod {
				continue
			}
			if endpoint.Zone == nil || !strings.HasPrefix(*endpoint.Zone, maglevIDPrefix) {
				continue
			}
			idStr := strings.TrimPrefix(*endpoint.Zone, maglevIDPrefix)
			if id, err := strconv.ParseInt(idStr, 10, 32); err == nil {
				assignments[string(endpoint.TargetRef.UID)] = int32(id)
			}
		}
	}
	return assignments
}
