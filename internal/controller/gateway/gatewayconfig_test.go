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

	meridio2v1alpha1 "github.com/nordix/meridio-2/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestGetGatewayConfiguration(t *testing.T) {
	t.Run("ReturnsNilIfNoParametersRef", func(t *testing.T) {
		gw := &gatewayv1.Gateway{
			Spec: gatewayv1.GatewaySpec{Infrastructure: nil},
		}
		reconciler, _ := setupReconciler(gw)

		gwConfig, err := reconciler.getGatewayConfiguration(context.Background(), gw)

		assert.NoError(t, err)
		assert.Nil(t, gwConfig)
	})

	t.Run("FetchesReferencedConfig", func(t *testing.T) {
		gwConfig := &meridio2v1alpha1.GatewayConfiguration{
			ObjectMeta: metav1.ObjectMeta{Name: "test-config", Namespace: "default"},
		}
		gw := &gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: "default"},
			Spec: gatewayv1.GatewaySpec{
				Infrastructure: &gatewayv1.GatewayInfrastructure{
					ParametersRef: &gatewayv1.LocalParametersReference{
						Group: gatewayv1.Group(meridio2v1alpha1.GroupVersion.Group),
						Kind:  gatewayv1.Kind(kindGatewayConfiguration),
						Name:  "test-config",
					},
				},
			},
		}
		reconciler, _ := setupReconciler(gw, gwConfig)

		result, err := reconciler.getGatewayConfiguration(context.Background(), gw)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "test-config", result.Name)
	})

	t.Run("ReturnsErrorIfConfigNotFound", func(t *testing.T) {
		gw := &gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: "default"},
			Spec: gatewayv1.GatewaySpec{
				Infrastructure: &gatewayv1.GatewayInfrastructure{
					ParametersRef: &gatewayv1.LocalParametersReference{
						Group: gatewayv1.Group(meridio2v1alpha1.GroupVersion.Group),
						Kind:  gatewayv1.Kind(kindGatewayConfiguration),
						Name:  "missing-config",
					},
				},
			},
		}
		reconciler, _ := setupReconciler(gw)

		result, err := reconciler.getGatewayConfiguration(context.Background(), gw)

		assert.Error(t, err)
		assert.Nil(t, result)
	})
}
