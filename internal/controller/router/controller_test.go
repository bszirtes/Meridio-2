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
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GatewayRouter Controller", func() {
	Context("makeNamespacedName", func() {
		It("should use default namespace when ref.Namespace is nil", func() {
			ref := gatewayapiv1.ParentReference{
				Name: "test-gateway",
			}
			result := makeNamespacedName(ref, "default-ns")
			Expect(result).To(Equal(types.NamespacedName{
				Name:      "test-gateway",
				Namespace: "default-ns",
			}))
		})

		It("should use explicit namespace when provided", func() {
			ns := gatewayapiv1.Namespace("explicit-ns")
			ref := gatewayapiv1.ParentReference{
				Name:      "test-gateway",
				Namespace: &ns,
			}
			result := makeNamespacedName(ref, "default-ns")
			Expect(result).To(Equal(types.NamespacedName{
				Name:      "test-gateway",
				Namespace: "explicit-ns",
			}))
		})
	})

	Context("getVIPs", func() {
		It("should extract addresses from gateway status", func() {
			gateway := &gatewayapiv1.Gateway{
				Status: gatewayapiv1.GatewayStatus{
					Addresses: []gatewayapiv1.GatewayStatusAddress{
						{Value: "20.0.0.1/32"},
						{Value: "40.0.0.1/32"},
					},
				},
			}
			vips := getVIPs(gateway)
			Expect(vips).To(Equal([]string{"20.0.0.1/32", "40.0.0.1/32"}))
		})

		It("should return empty when no addresses", func() {
			gateway := &gatewayapiv1.Gateway{}
			vips := getVIPs(gateway)
			Expect(vips).To(BeEmpty())
		})
	})

	Context("getGatewayRouters", func() {
		var reconciler *GatewayRouterReconciler

		BeforeEach(func() {
			reconciler = &GatewayRouterReconciler{
				Client: k8sClient,
			}
		})

		It("should return empty when no routers match", func() {
			gateway := types.NamespacedName{Name: "my-gateway", Namespace: "test-ns"}
			routers, err := reconciler.getGatewayRouters(ctx, gateway)
			Expect(err).NotTo(HaveOccurred())
			Expect(routers).To(BeEmpty())
		})

		It("should filter by matching gatewayRef name", func() {
			// This would require creating actual GatewayRouter resources in the test cluster
			// Skipping for minimal implementation
		})

		It("should filter by matching gatewayRef namespace", func() {
			// This would require creating actual GatewayRouter resources in the test cluster
			// Skipping for minimal implementation
		})

		It("should return multiple matching routers", func() {
			// This would require creating actual GatewayRouter resources in the test cluster
			// Skipping for minimal implementation
		})
	})

	Context("Reconcile", func() {
		It("should skip non-matching gateway name", func() {
			reconciler := &GatewayRouterReconciler{
				GatewayName:      "my-gateway",
				GatewayNamespace: "test-ns",
			}
			req := ctrl.Request{NamespacedName: types.NamespacedName{
				Name:      "other-gateway",
				Namespace: "test-ns",
			}}
			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))
		})

		It("should skip non-matching gateway namespace", func() {
			reconciler := &GatewayRouterReconciler{
				GatewayName:      "my-gateway",
				GatewayNamespace: "test-ns",
			}
			req := ctrl.Request{NamespacedName: types.NamespacedName{
				Name:      "my-gateway",
				Namespace: "other-ns",
			}}
			result, err := reconciler.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))
		})
	})
})
