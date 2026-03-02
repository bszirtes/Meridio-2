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
	"testing"

	meridio2v1alpha1 "github.com/nordix/meridio-2/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestCreateSlicesForNetwork_MaglevCapacityEnforcement(t *testing.T) {
	dg := &meridio2v1alpha1.DistributionGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dg", Namespace: "default"},
		Spec: meridio2v1alpha1.DistributionGroupSpec{
			Type: meridio2v1alpha1.DistributionGroupTypeMaglev,
			Maglev: &meridio2v1alpha1.MaglevConfig{
				MaxEndpoints: 2,
			},
		},
	}

	// 3 Pods with IPs, but only 2 get Maglev IDs (capacity limit)
	podsWithIP := []podWithNetworkIP{
		{pod: corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod-1", Name: "pod-1", Namespace: "default"}}, ip: "10.0.0.1"},
		{pod: corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod-2", Name: "pod-2", Namespace: "default"}}, ip: "10.0.0.2"},
		{pod: corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod-3", Name: "pod-3", Namespace: "default"}}, ip: "10.0.0.3"},
	}

	// Only 2 Pods get IDs (capacity exceeded for pod-3)
	podToID := map[string]int32{
		"pod-1": 0,
		"pod-2": 1,
		// pod-3 intentionally missing (capacity exceeded)
	}

	slices := createSlicesForNetwork(dg, podsWithIP, podToID, "192.168.1.0/24", nil)

	// Verify only 2 endpoints in result (pod-3 excluded)
	totalEndpoints := 0
	for _, slice := range slices {
		totalEndpoints += len(slice.Endpoints)
	}
	if totalEndpoints != 2 {
		t.Errorf("Expected 2 endpoints (capacity limit), got %d", totalEndpoints)
	}

	// Verify all endpoints have Maglev zones
	for _, slice := range slices {
		for _, ep := range slice.Endpoints {
			if ep.Zone == nil {
				t.Errorf("Endpoint %s has no zone (should be excluded)", ep.TargetRef.UID)
			}
			if ep.Zone != nil && (*ep.Zone != "maglev:0" && *ep.Zone != "maglev:1") {
				t.Errorf("Endpoint has invalid zone: %s", *ep.Zone)
			}
		}
	}

	// Verify pod-3 is NOT in any slice
	for _, slice := range slices {
		for _, ep := range slice.Endpoints {
			if ep.TargetRef.UID == types.UID("pod-3") {
				t.Error("pod-3 should be excluded (capacity exceeded)")
			}
		}
	}
}

func TestCreateSlicesForNetwork_NonMaglevIncludesAll(t *testing.T) {
	dg := &meridio2v1alpha1.DistributionGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dg", Namespace: "default"},
		Spec: meridio2v1alpha1.DistributionGroupSpec{
			Type: meridio2v1alpha1.DistributionGroupTypeMaglev, // Type doesn't matter here
		},
	}

	podsWithIP := []podWithNetworkIP{
		{pod: corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod-1", Name: "pod-1", Namespace: "default"}}, ip: "10.0.0.1"},
		{pod: corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod-2", Name: "pod-2", Namespace: "default"}}, ip: "10.0.0.2"},
		{pod: corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod-3", Name: "pod-3", Namespace: "default"}}, ip: "10.0.0.3"},
	}

	// Non-Maglev: podToID is nil
	slices := createSlicesForNetwork(dg, podsWithIP, nil, "192.168.1.0/24", nil)

	// Verify all 3 Pods are included (no capacity limit)
	totalEndpoints := 0
	for _, slice := range slices {
		totalEndpoints += len(slice.Endpoints)
	}
	if totalEndpoints != 3 {
		t.Errorf("Expected 3 endpoints (no capacity limit), got %d", totalEndpoints)
	}
}

func TestEndpointSliceNeedsUpdate(t *testing.T) {
	tests := []struct {
		name     string
		existing *discoveryv1.EndpointSlice
		desired  *discoveryv1.EndpointSlice
		expected bool
	}{
		{
			name: "no change",
			existing: &discoveryv1.EndpointSlice{
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints:   []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.1"}}},
				ObjectMeta:  metav1.ObjectMeta{Labels: map[string]string{"key": "value"}},
			},
			desired: &discoveryv1.EndpointSlice{
				AddressType: discoveryv1.AddressTypeIPv4,
				Endpoints:   []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.1"}}},
				ObjectMeta:  metav1.ObjectMeta{Labels: map[string]string{"key": "value"}},
			},
			expected: false,
		},
		{
			name: "address type changed",
			existing: &discoveryv1.EndpointSlice{
				AddressType: discoveryv1.AddressTypeIPv4,
			},
			desired: &discoveryv1.EndpointSlice{
				AddressType: discoveryv1.AddressTypeIPv6,
			},
			expected: true,
		},
		{
			name: "endpoints changed",
			existing: &discoveryv1.EndpointSlice{
				Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.1"}}},
			},
			desired: &discoveryv1.EndpointSlice{
				Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.2"}}},
			},
			expected: true,
		},
		{
			name: "labels changed",
			existing: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"key": "old"}},
			},
			desired: &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"key": "new"}},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := endpointSliceNeedsUpdate(tt.existing, tt.desired)
			if result != tt.expected {
				t.Errorf("endpointSliceNeedsUpdate() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGroupSlicesByNetwork_NormalizeCIDR(t *testing.T) {
	ctx := context.Background()

	slices := []discoveryv1.EndpointSlice{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "slice-1",
				Labels: map[string]string{labelNetworkSubnet: "192.168.1.0-24"},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "slice-2",
				Labels: map[string]string{labelNetworkSubnet: "192.168.1.5-24"}, // Non-canonical
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "slice-3",
				Labels: map[string]string{labelNetworkSubnet: "10.0.0.0-8"},
			},
		},
	}

	grouped := groupSlicesByNetwork(ctx, slices)

	// Both slice-1 and slice-2 should be grouped under canonical "192.168.1.0/24"
	if len(grouped["192.168.1.0/24"]) != 2 {
		t.Errorf("Expected 2 slices for 192.168.1.0/24, got %d", len(grouped["192.168.1.0/24"]))
	}

	if len(grouped["10.0.0.0/8"]) != 1 {
		t.Errorf("Expected 1 slice for 10.0.0.0/8, got %d", len(grouped["10.0.0.0/8"]))
	}
}

func TestGroupSlicesByNetwork_SkipInvalidLabel(t *testing.T) {
	ctx := context.Background()

	slices := []discoveryv1.EndpointSlice{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "valid",
				Labels: map[string]string{labelNetworkSubnet: "192.168.1.0-24"},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "invalid",
				Labels: map[string]string{labelNetworkSubnet: "not-a-cidr"},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "no-label",
			},
		},
	}

	grouped := groupSlicesByNetwork(ctx, slices)

	// Only valid slice should be grouped
	if len(grouped) != 1 {
		t.Errorf("Expected 1 network group, got %d", len(grouped))
	}
	if len(grouped["192.168.1.0/24"]) != 1 {
		t.Errorf("Expected 1 slice for 192.168.1.0/24, got %d", len(grouped["192.168.1.0/24"]))
	}
}

func TestCreateSlicesForNetwork_SliceSplitting(t *testing.T) {
	dg := &meridio2v1alpha1.DistributionGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dg", Namespace: "default"},
	}

	// Create 150 Pods (should split into 2 slices: 100 + 50)
	podsWithIP := make([]podWithNetworkIP, 150)
	for i := range 150 {
		podsWithIP[i] = podWithNetworkIP{
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					UID:       types.UID("pod-" + string(rune(i))),
					Name:      "pod-" + string(rune(i)),
					Namespace: "default",
				},
			},
			ip: "10.0.0." + string(rune(i)),
		}
	}

	slices := createSlicesForNetwork(dg, podsWithIP, nil, "192.168.1.0/24", nil)

	// Should create 2 slices
	if len(slices) != 2 {
		t.Errorf("Expected 2 slices (100+50), got %d", len(slices))
	}

	// Verify total endpoints
	totalEndpoints := 0
	for _, slice := range slices {
		totalEndpoints += len(slice.Endpoints)
		if len(slice.Endpoints) > 100 {
			t.Errorf("Slice has %d endpoints, max should be 100", len(slice.Endpoints))
		}
	}
	if totalEndpoints != 150 {
		t.Errorf("Expected 150 total endpoints, got %d", totalEndpoints)
	}
}

func TestCreateSlicesForNetwork_IPv6AddressType(t *testing.T) {
	dg := &meridio2v1alpha1.DistributionGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dg", Namespace: "default"},
	}

	podsWithIP := []podWithNetworkIP{
		{pod: corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod-1", Name: "pod-1"}}, ip: "2001:db8::1"},
	}

	slices := createSlicesForNetwork(dg, podsWithIP, nil, "2001:db8::/32", nil)

	if len(slices) != 1 {
		t.Fatalf("Expected 1 slice, got %d", len(slices))
	}
	if slices[0].AddressType != discoveryv1.AddressTypeIPv6 {
		t.Errorf("Expected IPv6 address type, got %v", slices[0].AddressType)
	}
}

func TestCreateSlicesForNetwork_LabelsAndMetadata(t *testing.T) {
	dg := &meridio2v1alpha1.DistributionGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dg", Namespace: "default"},
	}

	podsWithIP := []podWithNetworkIP{
		{pod: corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod-1", Name: "pod-1"}}, ip: "10.0.0.1"},
	}

	slices := createSlicesForNetwork(dg, podsWithIP, nil, "192.168.1.0/24", nil)

	if len(slices) != 1 {
		t.Fatalf("Expected 1 slice, got %d", len(slices))
	}

	slice := slices[0]
	if slice.Labels[labelManagedBy] != managedByValue {
		t.Errorf("Expected managed-by label %q, got %q", managedByValue, slice.Labels[labelManagedBy])
	}
	if slice.Labels[labelDistributionGroup] != "test-dg" {
		t.Errorf("Expected distribution-group label 'test-dg', got %q", slice.Labels[labelDistributionGroup])
	}
	if slice.Labels[labelNetworkSubnet] != "192.168.1.0-24" {
		t.Errorf("Expected network-subnet label '192.168.1.0-24', got %q", slice.Labels[labelNetworkSubnet])
	}
	if slice.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got %q", slice.Namespace)
	}
}

func TestCreateSlicesForNetwork_StructurePreservation(t *testing.T) {
	dg := &meridio2v1alpha1.DistributionGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dg", Namespace: "default"},
	}

	// Existing slice with 2 endpoints
	existingSlice := discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{Name: "existing-slice"},
		Endpoints: []discoveryv1.Endpoint{
			{TargetRef: &corev1.ObjectReference{UID: "pod-1"}},
			{TargetRef: &corev1.ObjectReference{UID: "pod-2"}},
		},
	}

	// 3 Pods: 2 existing + 1 new
	podsWithIP := []podWithNetworkIP{
		{pod: corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod-1", Name: "pod-1"}}, ip: "10.0.0.1"},
		{pod: corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod-2", Name: "pod-2"}}, ip: "10.0.0.2"},
		{pod: corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: "pod-3", Name: "pod-3"}}, ip: "10.0.0.3"},
	}

	slices := createSlicesForNetwork(dg, podsWithIP, nil, "192.168.1.0/24", []discoveryv1.EndpointSlice{existingSlice})

	// Should reuse existing slice
	if len(slices) != 1 {
		t.Errorf("Expected 1 slice (reused), got %d", len(slices))
	}
	if slices[0].Name != "existing-slice" {
		t.Errorf("Expected to reuse 'existing-slice', got %q", slices[0].Name)
	}
	if len(slices[0].Endpoints) != 3 {
		t.Errorf("Expected 3 endpoints in reused slice, got %d", len(slices[0].Endpoints))
	}
}
