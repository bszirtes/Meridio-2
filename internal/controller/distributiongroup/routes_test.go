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
	"testing"

	meridio2v1alpha1 "github.com/nordix/meridio-2/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestExtractDGsFromBackendRefs_DistributionGroup(t *testing.T) {
	dgGroup := meridio2v1alpha1.GroupVersion.Group
	dgKind := kindDistributionGroup
	ns := "test-ns"

	backendRefs := []gatewayv1.BackendRef{
		{
			BackendObjectReference: gatewayv1.BackendObjectReference{
				Group:     (*gatewayv1.Group)(&dgGroup),
				Kind:      (*gatewayv1.Kind)(&dgKind),
				Name:      "dg-1",
				Namespace: (*gatewayv1.Namespace)(&ns),
			},
		},
	}

	result := extractDGsFromBackendRefs(backendRefs, "default")

	expected := client.ObjectKey{Namespace: "test-ns", Name: "dg-1"}
	if !result[expected] {
		t.Errorf("Expected to extract %v, got %v", expected, result)
	}
	if len(result) != 1 {
		t.Errorf("Expected 1 DG, got %d", len(result))
	}
}

func TestExtractDGsFromBackendRefs_DefaultNamespace(t *testing.T) {
	dgGroup := meridio2v1alpha1.GroupVersion.Group
	dgKind := kindDistributionGroup

	backendRefs := []gatewayv1.BackendRef{
		{
			BackendObjectReference: gatewayv1.BackendObjectReference{
				Group: (*gatewayv1.Group)(&dgGroup),
				Kind:  (*gatewayv1.Kind)(&dgKind),
				Name:  "dg-1",
			},
		},
	}

	result := extractDGsFromBackendRefs(backendRefs, "local-ns")

	expected := client.ObjectKey{Namespace: "local-ns", Name: "dg-1"}
	if !result[expected] {
		t.Errorf("Expected to use local namespace, got %v", result)
	}
}

func TestExtractDGsFromBackendRefs_SkipService(t *testing.T) {
	backendRefs := []gatewayv1.BackendRef{
		{
			BackendObjectReference: gatewayv1.BackendObjectReference{
				Name: "service-1",
			},
		},
	}

	result := extractDGsFromBackendRefs(backendRefs, "default")

	if len(result) != 0 {
		t.Errorf("Expected to skip Service backend, got %d DGs", len(result))
	}
}

func TestExtractDGsFromBackendRefs_Multiple(t *testing.T) {
	dgGroup := meridio2v1alpha1.GroupVersion.Group
	dgKind := kindDistributionGroup

	backendRefs := []gatewayv1.BackendRef{
		{
			BackendObjectReference: gatewayv1.BackendObjectReference{
				Group: (*gatewayv1.Group)(&dgGroup),
				Kind:  (*gatewayv1.Kind)(&dgKind),
				Name:  "dg-1",
			},
		},
		{
			BackendObjectReference: gatewayv1.BackendObjectReference{
				Group: (*gatewayv1.Group)(&dgGroup),
				Kind:  (*gatewayv1.Kind)(&dgKind),
				Name:  "dg-2",
			},
		},
	}

	result := extractDGsFromBackendRefs(backendRefs, "default")

	if len(result) != 2 {
		t.Errorf("Expected 2 DGs, got %d", len(result))
	}
}

func TestBackendRefMatchesDG_Match(t *testing.T) {
	dgGroup := meridio2v1alpha1.GroupVersion.Group
	dgKind := kindDistributionGroup

	backendRef := gatewayv1.BackendRef{
		BackendObjectReference: gatewayv1.BackendObjectReference{
			Group: (*gatewayv1.Group)(&dgGroup),
			Kind:  (*gatewayv1.Kind)(&dgKind),
			Name:  "dg-1",
		},
	}

	dgKey := client.ObjectKey{Namespace: "default", Name: "dg-1"}

	if !backendRefMatchesDG(backendRef, "default", dgKey) {
		t.Error("Expected match")
	}
}

func TestBackendRefMatchesDG_DifferentName(t *testing.T) {
	dgGroup := meridio2v1alpha1.GroupVersion.Group
	dgKind := kindDistributionGroup

	backendRef := gatewayv1.BackendRef{
		BackendObjectReference: gatewayv1.BackendObjectReference{
			Group: (*gatewayv1.Group)(&dgGroup),
			Kind:  (*gatewayv1.Kind)(&dgKind),
			Name:  "dg-1",
		},
	}

	dgKey := client.ObjectKey{Namespace: "default", Name: "dg-2"}

	if backendRefMatchesDG(backendRef, "default", dgKey) {
		t.Error("Expected no match (different name)")
	}
}

func TestBackendRefMatchesDG_DifferentNamespace(t *testing.T) {
	dgGroup := meridio2v1alpha1.GroupVersion.Group
	dgKind := kindDistributionGroup
	ns := "other-ns"

	backendRef := gatewayv1.BackendRef{
		BackendObjectReference: gatewayv1.BackendObjectReference{
			Group:     (*gatewayv1.Group)(&dgGroup),
			Kind:      (*gatewayv1.Kind)(&dgKind),
			Name:      "dg-1",
			Namespace: (*gatewayv1.Namespace)(&ns),
		},
	}

	dgKey := client.ObjectKey{Namespace: "default", Name: "dg-1"}

	if backendRefMatchesDG(backendRef, "default", dgKey) {
		t.Error("Expected no match (different namespace)")
	}
}

func TestBackendRefMatchesDG_WrongKind(t *testing.T) {
	dgGroup := meridio2v1alpha1.GroupVersion.Group
	wrongKind := "Service"

	backendRef := gatewayv1.BackendRef{
		BackendObjectReference: gatewayv1.BackendObjectReference{
			Group: (*gatewayv1.Group)(&dgGroup),
			Kind:  (*gatewayv1.Kind)(&wrongKind),
			Name:  "dg-1",
		},
	}

	dgKey := client.ObjectKey{Namespace: "default", Name: "dg-1"}

	if backendRefMatchesDG(backendRef, "default", dgKey) {
		t.Error("Expected no match (wrong kind)")
	}
}

func TestBackendRefMatchesDG_WrongGroup(t *testing.T) {
	wrongGroup := "core"
	dgKind := kindDistributionGroup

	backendRef := gatewayv1.BackendRef{
		BackendObjectReference: gatewayv1.BackendObjectReference{
			Group: (*gatewayv1.Group)(&wrongGroup),
			Kind:  (*gatewayv1.Kind)(&dgKind),
			Name:  "dg-1",
		},
	}

	dgKey := client.ObjectKey{Namespace: "default", Name: "dg-1"}

	if backendRefMatchesDG(backendRef, "default", dgKey) {
		t.Error("Expected no match (wrong group)")
	}
}

func TestBackendRefMatchesDG_DefaultGroupKind(t *testing.T) {
	// BackendRef with no Group/Kind defaults to Service
	backendRef := gatewayv1.BackendRef{
		BackendObjectReference: gatewayv1.BackendObjectReference{
			Name: "dg-1",
		},
	}

	dgKey := client.ObjectKey{Namespace: "default", Name: "dg-1"}

	if backendRefMatchesDG(backendRef, "default", dgKey) {
		t.Error("Expected no match (defaults to Service, not DistributionGroup)")
	}
}
