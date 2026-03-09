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
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestIsGatewayAcceptedByController(t *testing.T) {
	t.Run("AcceptedByController", func(t *testing.T) {
		gw := &gatewayv1.Gateway{
			Status: gatewayv1.GatewayStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(gatewayv1.GatewayConditionAccepted),
						Status:  metav1.ConditionTrue,
						Message: "Gateway accepted by test-controller",
					},
				},
			},
		}
		assert.True(t, isGatewayAcceptedByController(gw, "test-controller"))
	})

	t.Run("AcceptedByDifferentController", func(t *testing.T) {
		gw := &gatewayv1.Gateway{
			Status: gatewayv1.GatewayStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(gatewayv1.GatewayConditionAccepted),
						Status:  metav1.ConditionTrue,
						Message: "Gateway accepted by other-controller",
					},
				},
			},
		}
		assert.False(t, isGatewayAcceptedByController(gw, "test-controller"))
	})

	t.Run("NotAccepted", func(t *testing.T) {
		gw := &gatewayv1.Gateway{
			Status: gatewayv1.GatewayStatus{
				Conditions: []metav1.Condition{
					{
						Type:   string(gatewayv1.GatewayConditionAccepted),
						Status: metav1.ConditionFalse,
					},
				},
			},
		}
		assert.False(t, isGatewayAcceptedByController(gw, "test-controller"))
	})

	t.Run("NoConditions", func(t *testing.T) {
		gw := &gatewayv1.Gateway{}
		assert.False(t, isGatewayAcceptedByController(gw, "test-controller"))
	})
}

func TestFindAcceptedConditionIndex(t *testing.T) {
	t.Run("FoundAtIndex0", func(t *testing.T) {
		gw := &gatewayv1.Gateway{
			Status: gatewayv1.GatewayStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(gatewayv1.GatewayConditionAccepted),
						Status:  metav1.ConditionTrue,
						Message: "Gateway accepted by test-controller",
					},
				},
			},
		}
		assert.Equal(t, 0, findAcceptedConditionIndex(gw, "test-controller"))
	})

	t.Run("FoundAtIndex1", func(t *testing.T) {
		gw := &gatewayv1.Gateway{
			Status: gatewayv1.GatewayStatus{
				Conditions: []metav1.Condition{
					{
						Type:   string(gatewayv1.GatewayConditionProgrammed),
						Status: metav1.ConditionTrue,
					},
					{
						Type:    string(gatewayv1.GatewayConditionAccepted),
						Status:  metav1.ConditionTrue,
						Message: "Gateway accepted by test-controller",
					},
				},
			},
		}
		assert.Equal(t, 1, findAcceptedConditionIndex(gw, "test-controller"))
	})

	t.Run("NotFound_DifferentController", func(t *testing.T) {
		gw := &gatewayv1.Gateway{
			Status: gatewayv1.GatewayStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(gatewayv1.GatewayConditionAccepted),
						Status:  metav1.ConditionTrue,
						Message: "Gateway accepted by other-controller",
					},
				},
			},
		}
		assert.Equal(t, -1, findAcceptedConditionIndex(gw, "test-controller"))
	})

	t.Run("NotFound_StatusFalse", func(t *testing.T) {
		gw := &gatewayv1.Gateway{
			Status: gatewayv1.GatewayStatus{
				Conditions: []metav1.Condition{
					{
						Type:    string(gatewayv1.GatewayConditionAccepted),
						Status:  metav1.ConditionFalse,
						Message: "Gateway accepted by test-controller",
					},
				},
			},
		}
		assert.Equal(t, -1, findAcceptedConditionIndex(gw, "test-controller"))
	})

	t.Run("NotFound_NoConditions", func(t *testing.T) {
		gw := &gatewayv1.Gateway{}
		assert.Equal(t, -1, findAcceptedConditionIndex(gw, "test-controller"))
	})
}
