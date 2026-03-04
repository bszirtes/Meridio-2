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
	"testing"

	nspAPI "github.com/nordix/meridio/api/nsp/v1"
	"github.com/nordix/meridio/pkg/loadbalancer/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	meridio2v1alpha1 "github.com/nordix/meridio-2/api/v1alpha1"
)

const (
	kindDistributionGroup = "DistributionGroup"
)

func TestLoadBalancerController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "LoadBalancer Controller Suite")
}

// Mock NFQLB instance
type mockNFQLBInstance struct {
	name               string
	activatedTargets   map[int]int // map[fwmark]index for verification
	deactivatedIndexes map[int]bool
}

func (m *mockNFQLBInstance) Activate(index int, fwmark int) error {
	if m.activatedTargets == nil {
		m.activatedTargets = make(map[int]int)
	}
	m.activatedTargets[fwmark] = index
	return nil
}

func (m *mockNFQLBInstance) Deactivate(index int) error {
	if m.deactivatedIndexes == nil {
		m.deactivatedIndexes = make(map[int]bool)
	}
	m.deactivatedIndexes[index] = true
	return nil
}

func (m *mockNFQLBInstance) Start() error                       { return nil }
func (m *mockNFQLBInstance) Delete() error                      { return nil }
func (m *mockNFQLBInstance) SetFlow(flow *nspAPI.Flow) error    { return nil }
func (m *mockNFQLBInstance) DeleteFlow(flow *nspAPI.Flow) error { return nil }
func (m *mockNFQLBInstance) GetName() string                    { return m.name }

// Mock NFQLB factory
type mockNFQLBFactory struct {
	instances map[string]*mockNFQLBInstance
}

func (m *mockNFQLBFactory) Start(ctx context.Context) error {
	return nil
}

func (m *mockNFQLBFactory) New(name string, mParam int, nParam int) (types.NFQueueLoadBalancer, error) {
	if m.instances == nil {
		m.instances = make(map[string]*mockNFQLBInstance)
	}
	instance := &mockNFQLBInstance{name: name}
	m.instances[name] = instance
	return instance, nil
}

var _ = Describe("LoadBalancer Controller", func() {
	var (
		scheme      *runtime.Scheme
		fakeClient  client.Client
		controller  *Controller
		mockFactory *mockNFQLBFactory
		ctx         context.Context
		gatewayName = "test-gateway"
		namespace   = "default"
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(meridio2v1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(gatewayv1.Install(scheme)).To(Succeed())
		Expect(discoveryv1.AddToScheme(scheme)).To(Succeed())

		mockFactory = &mockNFQLBFactory{}

		controller = &Controller{
			Scheme:           scheme,
			GatewayName:      gatewayName,
			GatewayNamespace: namespace,
			LBFactory:        mockFactory,
		}
	})

	Describe("belongsToGateway", func() {
		It("should return true when L34Route references both Gateway and DistributionGroup", func() {
			distGroup := &meridio2v1alpha1.DistributionGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-distgroup",
					Namespace: namespace,
				},
			}

			group := meridio2v1alpha1.GroupVersion.Group
			kind := kindDistributionGroup
			l34route := &meridio2v1alpha1.L34Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: namespace,
				},
				Spec: meridio2v1alpha1.L34RouteSpec{
					ParentRefs: []gatewayv1.ParentReference{
						{
							Name: gatewayv1.ObjectName(gatewayName),
						},
					},
					BackendRefs: []gatewayv1.BackendRef{
						{
							BackendObjectReference: gatewayv1.BackendObjectReference{
								Group: (*gatewayv1.Group)(&group),
								Kind:  (*gatewayv1.Kind)(&kind),
								Name:  gatewayv1.ObjectName(distGroup.Name),
							},
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(l34route).
				Build()
			controller.Client = fakeClient

			result := controller.belongsToGateway(ctx, distGroup)
			Expect(result).To(BeTrue())
		})

		It("should return false when no L34Route references the DistributionGroup", func() {
			distGroup := &meridio2v1alpha1.DistributionGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-distgroup",
					Namespace: namespace,
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				Build()
			controller.Client = fakeClient

			result := controller.belongsToGateway(ctx, distGroup)
			Expect(result).To(BeFalse())
		})

		It("should return false when L34Route references different Gateway", func() {
			distGroup := &meridio2v1alpha1.DistributionGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-distgroup",
					Namespace: namespace,
				},
			}

			group := meridio2v1alpha1.GroupVersion.Group
			kind := kindDistributionGroup
			l34route := &meridio2v1alpha1.L34Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: namespace,
				},
				Spec: meridio2v1alpha1.L34RouteSpec{
					ParentRefs: []gatewayv1.ParentReference{
						{
							Name: gatewayv1.ObjectName("other-gateway"),
						},
					},
					BackendRefs: []gatewayv1.BackendRef{
						{
							BackendObjectReference: gatewayv1.BackendObjectReference{
								Group: (*gatewayv1.Group)(&group),
								Kind:  (*gatewayv1.Kind)(&kind),
								Name:  gatewayv1.ObjectName(distGroup.Name),
							},
						},
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(l34route).
				Build()
			controller.Client = fakeClient

			result := controller.belongsToGateway(ctx, distGroup)
			Expect(result).To(BeFalse())
		})
	})

	Describe("reconcileNFQLBInstance", func() {
		BeforeEach(func() {
			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				Build()
			controller.Client = fakeClient
		})

		It("should create NFQLB instance with M=N×100", func() {
			maxEndpoints := int32(32)
			distGroup := &meridio2v1alpha1.DistributionGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-distgroup",
					Namespace: namespace,
				},
				Spec: meridio2v1alpha1.DistributionGroupSpec{
					Maglev: &meridio2v1alpha1.MaglevConfig{
						MaxEndpoints: maxEndpoints,
					},
				},
			}

			err := controller.reconcileNFQLBInstance(ctx, distGroup)
			Expect(err).ToNot(HaveOccurred())

			// Verify instance was created
			Expect(controller.instances).To(HaveKey(distGroup.Name))
			Expect(mockFactory.instances).To(HaveKey(distGroup.Name))
		})

		It("should use default N=32 when Maglev config is nil", func() {
			distGroup := &meridio2v1alpha1.DistributionGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-distgroup",
					Namespace: namespace,
				},
				Spec: meridio2v1alpha1.DistributionGroupSpec{
					Maglev: nil,
				},
			}

			err := controller.reconcileNFQLBInstance(ctx, distGroup)
			Expect(err).ToNot(HaveOccurred())

			Expect(controller.instances).To(HaveKey(distGroup.Name))
		})

		It("should not recreate existing instance", func() {
			distGroup := &meridio2v1alpha1.DistributionGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-distgroup",
					Namespace: namespace,
				},
			}

			// Create instance first time
			err := controller.reconcileNFQLBInstance(ctx, distGroup)
			Expect(err).ToNot(HaveOccurred())
			firstInstance := controller.instances[distGroup.Name]

			// Reconcile again
			err = controller.reconcileNFQLBInstance(ctx, distGroup)
			Expect(err).ToNot(HaveOccurred())
			secondInstance := controller.instances[distGroup.Name]

			// Should be same instance
			Expect(firstInstance).To(BeIdenticalTo(secondInstance))
		})
	})

	Describe("reconcileTargets", func() {
		var distGroup *meridio2v1alpha1.DistributionGroup

		BeforeEach(func() {
			distGroup = &meridio2v1alpha1.DistributionGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-distgroup",
					Namespace: namespace,
				},
			}

			// Create NFQLB instance first
			err := controller.reconcileNFQLBInstance(ctx, distGroup)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should activate new targets with correct index and fwmark", func() {
			ready := true
			// Zone field contains identifier (0-based)
			zone0 := "0"
			zone1 := "1"
			endpointSlice := &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-eps",
					Namespace: namespace,
					Labels: map[string]string{
						"kubernetes.io/service-name": distGroup.Name,
					},
				},
				Endpoints: []discoveryv1.Endpoint{
					{
						Addresses: []string{"10.0.0.1"},
						Conditions: discoveryv1.EndpointConditions{
							Ready: &ready,
						},
						Zone: &zone0,
					},
					{
						Addresses: []string{"10.0.0.2"},
						Conditions: discoveryv1.EndpointConditions{
							Ready: &ready,
						},
						Zone: &zone1,
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(endpointSlice).
				Build()
			controller.Client = fakeClient

			err := controller.reconcileTargets(ctx, distGroup)
			Expect(err).ToNot(HaveOccurred())

			// Verify targets were activated with correct fwmark
			// identifier=0 -> index=1, fwmark=5000
			// identifier=1 -> index=2, fwmark=5001
			mockInstance := mockFactory.instances[distGroup.Name]
			Expect(mockInstance.activatedTargets).To(HaveKey(5000))  // fwmark = 0 + 5000
			Expect(mockInstance.activatedTargets[5000]).To(Equal(1)) // index = 0 + 1
			Expect(mockInstance.activatedTargets).To(HaveKey(5001))  // fwmark = 1 + 5000
			Expect(mockInstance.activatedTargets[5001]).To(Equal(2)) // index = 1 + 1
		})

		It("should skip endpoints without Zone field", func() {
			ready := true
			endpointSlice := &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-eps",
					Namespace: namespace,
					Labels: map[string]string{
						"kubernetes.io/service-name": distGroup.Name,
					},
				},
				Endpoints: []discoveryv1.Endpoint{
					{
						Addresses: []string{"10.0.0.1"},
						Conditions: discoveryv1.EndpointConditions{
							Ready: &ready,
						},
						Zone: nil,
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(endpointSlice).
				Build()
			controller.Client = fakeClient

			err := controller.reconcileTargets(ctx, distGroup)
			Expect(err).ToNot(HaveOccurred())

			// No targets should be activated
			mockInstance := mockFactory.instances[distGroup.Name]
			Expect(mockInstance.activatedTargets).To(BeEmpty())
		})

		It("should skip non-ready endpoints", func() {
			ready := false
			zone := "0"
			endpointSlice := &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-eps",
					Namespace: namespace,
					Labels: map[string]string{
						"kubernetes.io/service-name": distGroup.Name,
					},
				},
				Endpoints: []discoveryv1.Endpoint{
					{
						Addresses: []string{"10.0.0.1"},
						Conditions: discoveryv1.EndpointConditions{
							Ready: &ready,
						},
						Zone: &zone,
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(endpointSlice).
				Build()
			controller.Client = fakeClient

			err := controller.reconcileTargets(ctx, distGroup)
			Expect(err).ToNot(HaveOccurred())

			// No targets should be activated
			mockInstance := mockFactory.instances[distGroup.Name]
			Expect(mockInstance.activatedTargets).To(BeEmpty())
		})

		It("should deactivate removed targets with correct index", func() {
			// Setup: activate a target first (identifier=0)
			controller.targets = map[string]map[int][]string{
				distGroup.Name: {
					0: {"10.0.0.1"},
				},
			}

			// No EndpointSlices (target removed)
			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				Build()
			controller.Client = fakeClient

			err := controller.reconcileTargets(ctx, distGroup)
			Expect(err).ToNot(HaveOccurred())

			// Verify target was deactivated with index=1 (identifier 0 + 1)
			mockInstance := mockFactory.instances[distGroup.Name]
			Expect(mockInstance.deactivatedIndexes).To(HaveKey(1))
		})

		It("should parse Zone field with maglev: prefix", func() {
			ready := true
			zone0 := "maglev:0"
			zone1 := "maglev:1"
			endpointSlice := &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-eps",
					Namespace: namespace,
					Labels: map[string]string{
						"kubernetes.io/service-name": distGroup.Name,
					},
				},
				Endpoints: []discoveryv1.Endpoint{
					{
						Addresses: []string{"10.0.0.1"},
						Conditions: discoveryv1.EndpointConditions{
							Ready: &ready,
						},
						Zone: &zone0,
					},
					{
						Addresses: []string{"10.0.0.2"},
						Conditions: discoveryv1.EndpointConditions{
							Ready: &ready,
						},
						Zone: &zone1,
					},
				},
			}

			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(endpointSlice).
				Build()
			controller.Client = fakeClient

			err := controller.reconcileTargets(ctx, distGroup)
			Expect(err).ToNot(HaveOccurred())

			// Verify targets were activated with correct identifiers
			mockInstance := mockFactory.instances[distGroup.Name]
			Expect(mockInstance.activatedTargets).To(HaveKey(5000)) // fwmark = 0 + 5000
			Expect(mockInstance.activatedTargets[5000]).To(Equal(1)) // index = 0 + 1
			Expect(mockInstance.activatedTargets).To(HaveKey(5001)) // fwmark = 1 + 5000
			Expect(mockInstance.activatedTargets[5001]).To(Equal(2)) // index = 1 + 1
		})
	})

	Describe("endpointSliceEnqueue", func() {
		It("should map EndpointSlice to DistributionGroup", func() {
			endpointSlice := &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-eps",
					Namespace: namespace,
					Labels: map[string]string{
						"kubernetes.io/service-name": "test-distgroup",
					},
				},
			}

			requests := controller.endpointSliceEnqueue(ctx, endpointSlice)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal("test-distgroup"))
			Expect(requests[0].Namespace).To(Equal(namespace))
		})

		It("should return nil when label is missing", func() {
			endpointSlice := &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-eps",
					Namespace: namespace,
					Labels:    map[string]string{},
				},
			}

			requests := controller.endpointSliceEnqueue(ctx, endpointSlice)
			Expect(requests).To(BeNil())
		})

		It("should filter by namespace", func() {
			endpointSlice := &discoveryv1.EndpointSlice{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-eps",
					Namespace: "other-namespace",
					Labels: map[string]string{
						"kubernetes.io/service-name": "test-distgroup",
					},
				},
			}

			requests := controller.endpointSliceEnqueue(ctx, endpointSlice)
			Expect(requests).To(BeNil())
		})
	})
})
