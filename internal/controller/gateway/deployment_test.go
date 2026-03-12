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
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestLbDeploymentName(t *testing.T) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "my-gateway"},
	}

	name := lbDeploymentName(gw)

	assert.Equal(t, "sllb-my-gateway", name)
}

func TestSetControllerLabels(t *testing.T) {
	t.Run("SetsRequiredLabels", func(t *testing.T) {
		meta := &metav1.ObjectMeta{}

		setControllerLabels(meta, "sllb-test", "test-gw")

		assert.Equal(t, "sllb-test", meta.Labels["app"])
		assert.Equal(t, "test-gw", meta.Labels[labelGatewayName])
	})

	t.Run("InitializesLabelsMapIfNil", func(t *testing.T) {
		meta := &metav1.ObjectMeta{Labels: nil}

		setControllerLabels(meta, "sllb-test", "test-gw")

		assert.NotNil(t, meta.Labels)
		assert.Len(t, meta.Labels, 2)
	})

	t.Run("PreservesExistingLabels", func(t *testing.T) {
		meta := &metav1.ObjectMeta{
			Labels: map[string]string{"existing": "label"},
		}

		setControllerLabels(meta, "sllb-test", "test-gw")

		assert.Equal(t, "label", meta.Labels["existing"])
		assert.Len(t, meta.Labels, 3)
	})
}

func TestMergeInfrastructureMetadata(t *testing.T) {
	t.Run("MergesLabelsAndAnnotations", func(t *testing.T) {
		gw := &gatewayv1.Gateway{
			Spec: gatewayv1.GatewaySpec{
				Infrastructure: &gatewayv1.GatewayInfrastructure{
					Labels: map[gatewayv1.LabelKey]gatewayv1.LabelValue{
						"infra-label": "value1",
					},
					Annotations: map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
						"infra-annotation": "value2",
					},
				},
			},
		}
		meta := &metav1.ObjectMeta{}

		mergeInfrastructureMetadata(meta, gw)

		assert.Equal(t, "value1", meta.Labels["infra-label"])
		assert.Equal(t, "value2", meta.Annotations["infra-annotation"])
	})

	t.Run("NoOpIfInfrastructureIsNil", func(t *testing.T) {
		gw := &gatewayv1.Gateway{
			Spec: gatewayv1.GatewaySpec{Infrastructure: nil},
		}
		meta := &metav1.ObjectMeta{}

		mergeInfrastructureMetadata(meta, gw)

		assert.Nil(t, meta.Labels)
		assert.Nil(t, meta.Annotations)
	})

	t.Run("CanOverrideExistingLabels", func(t *testing.T) {
		gw := &gatewayv1.Gateway{
			Spec: gatewayv1.GatewaySpec{
				Infrastructure: &gatewayv1.GatewayInfrastructure{
					Labels: map[gatewayv1.LabelKey]gatewayv1.LabelValue{
						"app": "overridden",
					},
				},
			},
		}
		meta := &metav1.ObjectMeta{
			Labels: map[string]string{"app": "original"},
		}

		mergeInfrastructureMetadata(meta, gw)

		assert.Equal(t, "overridden", meta.Labels["app"])
	})
}

func TestDeploymentNeedsUpdate(t *testing.T) {
	t.Run("NoChangeNeeded", func(t *testing.T) {
		existing := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      map[string]string{"app": "test"},
				Annotations: map[string]string{"key": "value"},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr(int32(2)),
			},
		}
		desired := existing.DeepCopy()

		assert.False(t, deploymentNeedsUpdate(existing, desired))
	})

	t.Run("SpecChanged", func(t *testing.T) {
		existing := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{Replicas: ptr(int32(2))},
		}
		desired := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{Replicas: ptr(int32(3))},
		}

		assert.True(t, deploymentNeedsUpdate(existing, desired))
	})

	t.Run("LabelsChanged", func(t *testing.T) {
		existing := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "old"}},
		}
		desired := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "new"}},
		}

		assert.True(t, deploymentNeedsUpdate(existing, desired))
	})

	t.Run("AnnotationsChanged", func(t *testing.T) {
		existing := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"key": "old"}},
		}
		desired := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"key": "new"}},
		}

		assert.True(t, deploymentNeedsUpdate(existing, desired))
	})
}

func TestMergeMaps(t *testing.T) {
	t.Run("MergesNonOverlapping", func(t *testing.T) {
		base := map[string]string{"a": "1", "b": "2"}
		overwrite := map[string]string{"c": "3", "d": "4"}

		result := mergeMaps(base, overwrite)

		assert.Len(t, result, 4)
		assert.Equal(t, "1", result["a"])
		assert.Equal(t, "3", result["c"])
	})

	t.Run("OverwritesTakesPrecedence", func(t *testing.T) {
		base := map[string]string{"a": "old", "b": "2"}
		overwrite := map[string]string{"a": "new", "c": "3"}

		result := mergeMaps(base, overwrite)

		assert.Equal(t, "new", result["a"])
		assert.Equal(t, "2", result["b"])
		assert.Equal(t, "3", result["c"])
	})

	t.Run("HandlesNilMaps", func(t *testing.T) {
		result := mergeMaps(nil, map[string]string{"a": "1"})
		assert.Equal(t, "1", result["a"])

		result = mergeMaps(map[string]string{"a": "1"}, nil)
		assert.Equal(t, "1", result["a"])
	})
}

func ptr[T any](v T) *T {
	return &v
}

func TestReconcileLBDeployment(t *testing.T) {
	t.Run("CreatesDeploymentFromTemplate", func(t *testing.T) {
		// Setup temp template directory
		tmpDir := t.TempDir()
		templateFile := filepath.Join(tmpDir, LBDeploymentTemplateFile)
		templateYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: placeholder
  labels:
    template-label: template-value
spec:
  replicas: 1
  selector:
    matchLabels:
      app: placeholder
  template:
    metadata:
      labels:
        app: placeholder
    spec:
      serviceAccountName: placeholder
      containers:
      - name: loadbalancer
        image: registry.nordix.org/cloud-native/meridio-2/loadbalancer:latest
`
		err := os.WriteFile(templateFile, []byte(templateYAML), 0644)
		assert.NoError(t, err)

		// Setup Gateway and reconciler
		gwClass := newGatewayClass(testControllerName)
		gw := newGateway(gwClass.Name)
		reconciler, fakeClient := setupReconciler(gwClass, gw)
		reconciler.TemplatePath = tmpDir

		// Reconcile
		err = reconciler.reconcileLBDeployment(context.Background(), gw)
		assert.NoError(t, err)

		// Verify Deployment created
		var deployment appsv1.Deployment
		err = fakeClient.Get(context.Background(), client.ObjectKey{
			Namespace: gw.Namespace,
			Name:      "sllb-" + gw.Name,
		}, &deployment)
		assert.NoError(t, err)
		assert.Equal(t, "sllb-"+gw.Name, deployment.Name)
		assert.Equal(t, "template-value", deployment.Labels["template-label"])
		assert.Equal(t, gw.Name, deployment.Labels[labelGatewayName])
	})

	t.Run("UpdatesExistingDeployment", func(t *testing.T) {
		// Setup temp template directory
		tmpDir := t.TempDir()
		templateFile := filepath.Join(tmpDir, LBDeploymentTemplateFile)
		templateYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: placeholder
  labels:
    template-label: new-value
spec:
  replicas: 1
  selector:
    matchLabels:
      app: placeholder
  template:
    metadata:
      labels:
        app: placeholder
    spec:
      serviceAccountName: placeholder
      containers:
      - name: loadbalancer
        image: registry.nordix.org/cloud-native/meridio-2/loadbalancer:v2
`
		err := os.WriteFile(templateFile, []byte(templateYAML), 0644)
		assert.NoError(t, err)

		// Setup existing Deployment
		gwClass := newGatewayClass(testControllerName)
		gw := newGateway(gwClass.Name)
		existing := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sllb-" + gw.Name,
				Namespace: gw.Namespace,
				Labels: map[string]string{
					"template-label": "old-value",
					labelGatewayName: gw.Name,
					"external-label": "preserved",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         gatewayv1.GroupVersion.String(),
						Kind:               kindGateway,
						Name:               gw.Name,
						UID:                gw.UID,
						Controller:         ptr(true),
						BlockOwnerDeletion: ptr(true),
					},
				},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr(int32(1)),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "sllb-" + gw.Name},
				},
			},
		}

		reconciler, fakeClient := setupReconciler(gwClass, gw, existing)
		reconciler.TemplatePath = tmpDir

		// Reconcile
		err = reconciler.reconcileLBDeployment(context.Background(), gw)
		assert.NoError(t, err)

		// Verify Deployment updated
		var deployment appsv1.Deployment
		err = fakeClient.Get(context.Background(), client.ObjectKey{
			Namespace: gw.Namespace,
			Name:      "sllb-" + gw.Name,
		}, &deployment)
		assert.NoError(t, err)
		assert.Equal(t, "new-value", deployment.Labels["template-label"])
		assert.Equal(t, "preserved", deployment.Labels["external-label"])
	})

	t.Run("NameCollision_ReturnsError", func(t *testing.T) {
		// Setup temp template directory
		tmpDir := t.TempDir()
		templateFile := filepath.Join(tmpDir, LBDeploymentTemplateFile)
		templateYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: placeholder
spec:
  replicas: 1
  selector:
    matchLabels:
      app: placeholder
  template:
    metadata:
      labels:
        app: placeholder
    spec:
      serviceAccountName: placeholder
      containers:
      - name: loadbalancer
        image: registry.nordix.org/cloud-native/meridio-2/loadbalancer:latest
`
		err := os.WriteFile(templateFile, []byte(templateYAML), 0644)
		assert.NoError(t, err)

		// Setup Gateway and existing Deployment owned by someone else
		gwClass := newGatewayClass(testControllerName)
		gw := newGateway(gwClass.Name)
		otherOwner := &gatewayv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-gateway",
				Namespace: gw.Namespace,
				UID:       "other-uid",
			},
		}
		deploymentName := lbDeploymentName(gw)
		existing := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      deploymentName,
				Namespace: gw.Namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         gatewayv1.GroupVersion.String(),
						Kind:               kindGateway,
						Name:               otherOwner.Name,
						UID:                otherOwner.UID,
						Controller:         ptr(true),
						BlockOwnerDeletion: ptr(true),
					},
				},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr(int32(1)),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": deploymentName},
				},
			},
		}

		reconciler, _ := setupReconciler(gwClass, gw, existing)
		reconciler.TemplatePath = tmpDir

		// Reconcile should fail with name collision error
		err = reconciler.reconcileLBDeployment(context.Background(), gw)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "name collision")
		assert.Contains(t, err.Error(), "Gateway/other-gateway")

		// Verify it's a permanent error
		var permErr *permanentDeploymentError
		assert.True(t, errors.As(err, &permErr))
	})

	t.Run("TemplateMissing_ReturnsError", func(t *testing.T) {
		gwClass := newGatewayClass(testControllerName)
		gw := newGateway(gwClass.Name)

		reconciler, _ := setupReconciler(gwClass, gw)
		reconciler.TemplatePath = "/nonexistent/path"

		// Reconcile should fail with permanent error
		err := reconciler.reconcileLBDeployment(context.Background(), gw)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to load LB deployment template")

		// Verify it's a permanent error
		var permErr *permanentDeploymentError
		assert.True(t, errors.As(err, &permErr))
	})
}
