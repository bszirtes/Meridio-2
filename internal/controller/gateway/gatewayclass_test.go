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

package gateway

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShouldManageGateway(t *testing.T) {
	t.Run("MatchingController_ReturnsTrue", func(t *testing.T) {
		gwClass := newGatewayClass(testControllerName)
		gw := newGateway(gwClass.Name)

		reconciler, _ := setupReconciler(gwClass, gw)

		shouldManage, err := reconciler.shouldManageGateway(context.Background(), gw)

		assert.NoError(t, err)
		assert.True(t, shouldManage)
	})

	t.Run("DifferentController_ReturnsFalse", func(t *testing.T) {
		gwClass := newGatewayClass("other-controller")
		gw := newGateway(gwClass.Name)

		reconciler, _ := setupReconciler(gwClass, gw)

		shouldManage, err := reconciler.shouldManageGateway(context.Background(), gw)

		assert.NoError(t, err)
		assert.False(t, shouldManage)
	})

	t.Run("MissingGatewayClass_ReturnsFalse", func(t *testing.T) {
		gw := newGateway("nonexistent-class")

		reconciler, _ := setupReconciler(gw)

		shouldManage, err := reconciler.shouldManageGateway(context.Background(), gw)

		assert.NoError(t, err)
		assert.False(t, shouldManage)
	})

	t.Run("EmptyControllerName_ReturnsFalse", func(t *testing.T) {
		gwClass := newGatewayClass("")
		gw := newGateway(gwClass.Name)

		reconciler, _ := setupReconciler(gwClass, gw)

		shouldManage, err := reconciler.shouldManageGateway(context.Background(), gw)

		assert.NoError(t, err)
		assert.False(t, shouldManage)
	})
}

func TestMapGatewayClassToGateway(t *testing.T) {
	t.Run("ManagedGatewayClass", func(t *testing.T) {
		gwClass := newGatewayClass(testControllerName)
		gw := newGateway(gwClass.Name)

		reconciler, _ := setupReconciler(gwClass, gw)
		requests := reconciler.mapGatewayClassToGateway(context.Background(), gwClass)

		assert.Len(t, requests, 1)
		assert.Equal(t, gw.Name, requests[0].Name)
		assert.Equal(t, gw.Namespace, requests[0].Namespace)
	})

	t.Run("UnmanagedGatewayClass", func(t *testing.T) {
		gwClass := newGatewayClass("other-controller")
		gw := newGateway(gwClass.Name)

		reconciler, _ := setupReconciler(gwClass, gw)
		requests := reconciler.mapGatewayClassToGateway(context.Background(), gwClass)

		assert.Empty(t, requests)
	})

	t.Run("MultipleGateways", func(t *testing.T) {
		gwClass := newGatewayClass(testControllerName)
		gw1 := newGateway(gwClass.Name)
		gw1.Name = "gw-1"
		gw2 := newGateway(gwClass.Name)
		gw2.Name = "gw-2"

		reconciler, _ := setupReconciler(gwClass, gw1, gw2)
		requests := reconciler.mapGatewayClassToGateway(context.Background(), gwClass)

		assert.Len(t, requests, 2)
	})

	t.Run("NoMatchingGateways", func(t *testing.T) {
		gwClass := newGatewayClass(testControllerName)
		otherGwClass := newGatewayClass(testControllerName)
		otherGwClass.Name = "other-class"
		gw := newGateway(otherGwClass.Name)

		reconciler, _ := setupReconciler(gwClass, otherGwClass, gw)
		requests := reconciler.mapGatewayClassToGateway(context.Background(), gwClass)

		assert.Empty(t, requests)
	})
}
