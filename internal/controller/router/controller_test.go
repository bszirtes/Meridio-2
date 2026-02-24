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

package router

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	meridio2v1alpha1 "github.com/nordix/meridio-2/api/v1alpha1"
	"github.com/nordix/meridio-2/internal/bird"
)

const testGatewayName = "test-gateway"
const testNamespace = "default"

// mockingBird is a test double for bird.BirdInterface that doesn't perform file system operations
type mockingBird struct{}

func (m *mockingBird) Run(ctx context.Context) error {
	return nil
}

func (m *mockingBird) Configure(ctx context.Context, vips []string, routers []*meridio2v1alpha1.GatewayRouter) error {
	return nil
}

func (m *mockingBird) Monitor(ctx context.Context, interval time.Duration) (<-chan bird.MonitorStatus, error) {
	ch := make(chan bird.MonitorStatus)
	close(ch)
	return ch, nil
}

func newScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = gatewayapiv1.Install(scheme)
	_ = meridio2v1alpha1.AddToScheme(scheme)
	return scheme
}

func newGateway(name, namespace string, addresses ...string) *gatewayapiv1.Gateway {
	gw := &gatewayapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: gatewayapiv1.GatewaySpec{
			GatewayClassName: "test-class",
		},
	}

	if len(addresses) > 0 {
		gw.Status.Addresses = make([]gatewayapiv1.GatewayStatusAddress, len(addresses))
		for i, addr := range addresses {
			gw.Status.Addresses[i] = gatewayapiv1.GatewayStatusAddress{
				Type:  ptr(gatewayapiv1.IPAddressType),
				Value: addr,
			}
		}
	}

	return gw
}

func newGatewayRouter(name, namespace string, gatewayRef gatewayapiv1.ParentReference) *meridio2v1alpha1.GatewayRouter {
	return &meridio2v1alpha1.GatewayRouter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: meridio2v1alpha1.GatewayRouterSpec{
			GatewayRef: gatewayRef,
		},
	}
}

func setupReconciler(gatewayName, gatewayNamespace string, objects ...client.Object) (*GatewayRouterReconciler, client.Client) {
	fakeClient := fake.NewClientBuilder().
		WithScheme(newScheme()).
		WithObjects(objects...).
		WithStatusSubresource(&gatewayapiv1.Gateway{}, &meridio2v1alpha1.GatewayRouter{}).
		Build()

	return &GatewayRouterReconciler{
		Client:           fakeClient,
		Scheme:           newScheme(),
		GatewayName:      gatewayName,
		GatewayNamespace: gatewayNamespace,
		Bird:             &mockingBird{},
	}, fakeClient
}

func ptr[T any](v T) *T {
	return &v
}

func TestGatewayRouterReconciler_Reconcile(t *testing.T) {
	t.Run("Reconcile_SkipsNonMatchingGatewayName", func(t *testing.T) {
		gw := newGateway("other-gateway", testNamespace)
		reconciler, _ := setupReconciler(testGatewayName, testNamespace, gw)

		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: "other-gateway", Namespace: testNamespace}}
		result, err := reconciler.Reconcile(context.Background(), request)

		assert.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, result)
	})

	t.Run("Reconcile_SkipsNonMatchingGatewayNamespace", func(t *testing.T) {
		gw := newGateway(testGatewayName, "other-namespace")
		reconciler, _ := setupReconciler(testGatewayName, testNamespace, gw)

		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: testGatewayName, Namespace: "other-namespace"}}
		result, err := reconciler.Reconcile(context.Background(), request)

		assert.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, result)
	})

	t.Run("Reconcile_HandlesGatewayNotFound", func(t *testing.T) {
		reconciler, _ := setupReconciler(testGatewayName, testNamespace)

		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: testGatewayName, Namespace: testNamespace}}
		result, err := reconciler.Reconcile(context.Background(), request)

		assert.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, result)
	})

	t.Run("Reconcile_ProcessesMatchingGateway", func(t *testing.T) {
		gw := newGateway(testGatewayName, testNamespace, "40.0.0.1/32", "40.0.0.2/32")
		reconciler, fakeClient := setupReconciler(testGatewayName, testNamespace, gw)

		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: testGatewayName, Namespace: testNamespace}}
		result, err := reconciler.Reconcile(context.Background(), request)

		assert.NoError(t, err)
		assert.Equal(t, ctrl.Result{}, result)

		// Verify gateway was fetched
		fetched := &gatewayapiv1.Gateway{}
		err = fakeClient.Get(context.Background(), types.NamespacedName{Name: testGatewayName, Namespace: testNamespace}, fetched)
		assert.NoError(t, err)
		assert.Equal(t, 2, len(fetched.Status.Addresses))
	})
}

func TestGetVIPs(t *testing.T) {
	t.Run("ExtractsAddressesFromGatewayStatus", func(t *testing.T) {
		gw := newGateway(testGatewayName, testNamespace, "20.0.0.1/32", "40.0.0.1/32")
		vips := getVIPs(gw)

		assert.Equal(t, []string{"20.0.0.1/32", "40.0.0.1/32"}, vips)
	})

	t.Run("ReturnsEmptyWhenNoAddresses", func(t *testing.T) {
		gw := newGateway(testGatewayName, testNamespace)
		vips := getVIPs(gw)

		assert.Empty(t, vips)
	})
}

func TestMakeNamespacedName(t *testing.T) {
	t.Run("UsesDefaultNamespaceWhenRefNamespaceIsNil", func(t *testing.T) {
		ref := gatewayapiv1.ParentReference{
			Name: "test-gateway",
		}
		result := makeNamespacedName(ref, "default-ns")

		assert.Equal(t, types.NamespacedName{
			Name:      "test-gateway",
			Namespace: "default-ns",
		}, result)
	})

	t.Run("UsesExplicitNamespaceWhenProvided", func(t *testing.T) {
		ns := gatewayapiv1.Namespace("explicit-ns")
		ref := gatewayapiv1.ParentReference{
			Name:      "test-gateway",
			Namespace: &ns,
		}
		result := makeNamespacedName(ref, "default-ns")

		assert.Equal(t, types.NamespacedName{
			Name:      "test-gateway",
			Namespace: "explicit-ns",
		}, result)
	})
}

func TestGetGatewayRouters(t *testing.T) {
	t.Run("ReturnsEmptyWhenNoRoutersMatch", func(t *testing.T) {
		gw := newGateway(testGatewayName, testNamespace)
		reconciler, _ := setupReconciler(testGatewayName, testNamespace, gw)

		gateway := types.NamespacedName{Name: testGatewayName, Namespace: testNamespace}
		routers, err := reconciler.getGatewayRouters(context.Background(), gateway)

		assert.NoError(t, err)
		assert.Empty(t, routers)
	})

	t.Run("ReturnsMatchingRouters", func(t *testing.T) {
		gw := newGateway(testGatewayName, testNamespace)
		gwRef := gatewayapiv1.ParentReference{
			Name:      gatewayapiv1.ObjectName(testGatewayName),
			Namespace: ptr(gatewayapiv1.Namespace(testNamespace)),
		}
		router1 := newGatewayRouter("router1", testNamespace, gwRef)
		router2 := newGatewayRouter("router2", testNamespace, gwRef)

		reconciler, _ := setupReconciler(testGatewayName, testNamespace, gw, router1, router2)

		gateway := types.NamespacedName{Name: testGatewayName, Namespace: testNamespace}
		routers, err := reconciler.getGatewayRouters(context.Background(), gateway)

		assert.NoError(t, err)
		assert.Equal(t, 2, len(routers))
	})

	t.Run("FiltersOutNonMatchingRouters", func(t *testing.T) {
		gw := newGateway(testGatewayName, testNamespace)
		matchingRef := gatewayapiv1.ParentReference{
			Name:      gatewayapiv1.ObjectName(testGatewayName),
			Namespace: ptr(gatewayapiv1.Namespace(testNamespace)),
		}
		nonMatchingRef := gatewayapiv1.ParentReference{
			Name:      "other-gateway",
			Namespace: ptr(gatewayapiv1.Namespace(testNamespace)),
		}
		router1 := newGatewayRouter("router1", testNamespace, matchingRef)
		router2 := newGatewayRouter("router2", testNamespace, nonMatchingRef)

		reconciler, _ := setupReconciler(testGatewayName, testNamespace, gw, router1, router2)

		gateway := types.NamespacedName{Name: testGatewayName, Namespace: testNamespace}
		routers, err := reconciler.getGatewayRouters(context.Background(), gateway)

		assert.NoError(t, err)
		assert.Equal(t, 1, len(routers))
		assert.Equal(t, "router1", routers[0].Name)
	})
}
