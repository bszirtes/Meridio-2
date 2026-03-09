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
	"fmt"
	"maps"

	meridio2v1alpha1 "github.com/nordix/meridio-2/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// permanentDeploymentError indicates a permanent failure that should be exposed in Gateway status
type permanentDeploymentError struct {
	message string
}

func (e *permanentDeploymentError) Error() string {
	return e.message
}

// reconcileLBDeployment creates or updates the LB Deployment for the Gateway
func (r *GatewayReconciler) reconcileLBDeployment(ctx context.Context, gw *gatewayv1.Gateway) error {
	// Load template
	template, err := r.loadLBDeploymentTemplate()
	if err != nil {
		return fmt.Errorf("failed to load LB deployment template: %w", err)
	}

	// Fetch GatewayConfiguration if referenced
	gwConfig, err := r.getGatewayConfiguration(ctx, gw)
	if err != nil {
		return fmt.Errorf("failed to get GatewayConfiguration: %w", err)
	}

	// Generate deployment name
	deploymentName := lbDeploymentName(gw)

	// Check if Deployment exists (name-based lookup with ownership verification)
	var existing appsv1.Deployment
	err = r.Get(ctx, client.ObjectKey{Namespace: gw.Namespace, Name: deploymentName}, &existing)

	var found bool
	if err == nil {
		// Deployment with desired name exists - verify ownership
		if !metav1.IsControlledBy(&existing, gw) {
			owner := getControllerOwner(&existing)
			return &permanentDeploymentError{
				message: fmt.Sprintf("deployment %s exists but is not owned by gateway %s (owned by %s, cannot proceed due to name collision)", existing.Name, gw.Name, owner),
			}
		}
		found = true
	} else if !apierrors.IsNotFound(err) {
		// API server error - return to trigger retry
		return fmt.Errorf("failed to get LB deployment: %w", err)
	}

	// Build desired state
	var desired *appsv1.Deployment
	if found {
		// Update existing
		desired = reconcileDeploymentSpec(&existing, template, gw, gwConfig, deploymentName, r.LBServiceAccount)
	} else {
		// Create new
		desired = reconcileDeploymentSpec(nil, template, gw, gwConfig, deploymentName, r.LBServiceAccount)
		// Set ownerReference only for new Deployments
		if err := ctrl.SetControllerReference(gw, desired, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference: %w", err)
		}
	}

	// Create or update
	if !found {
		if err := r.Create(ctx, desired); err != nil {
			// AlreadyExists means another reconciliation created it - not a permanent error
			if apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("failed to create LB deployment: %w", err)
			}
			return &permanentDeploymentError{
				message: fmt.Sprintf("failed to create LB deployment: %v", err),
			}
		}
	} else if deploymentNeedsUpdate(&existing, desired) {
		if err := r.Update(ctx, desired); err != nil {
			return fmt.Errorf("failed to update LB deployment: %w", err)
		}
	}

	return nil
}

// getControllerOwner returns the controller owner of a Deployment as "Kind/Name"
func getControllerOwner(deployment *appsv1.Deployment) string {
	for _, ref := range deployment.OwnerReferences {
		if ref.Controller != nil && *ref.Controller {
			return fmt.Sprintf("%s/%s", ref.Kind, ref.Name)
		}
	}
	return "unknown"
}

// reconcileDeploymentSpec builds desired deployment state (works for both create and update)
// If base is nil, template is used as starting point (create scenario)
// If base is provided, it's used as starting point and template fields are merged (update scenario)
func reconcileDeploymentSpec(base, template *appsv1.Deployment, gw *gatewayv1.Gateway, gwConfig *meridio2v1alpha1.GatewayConfiguration, deploymentName, serviceAccount string) *appsv1.Deployment {
	var desired *appsv1.Deployment

	if base == nil {
		// Create scenario: start from template
		desired = template.DeepCopy()
	} else {
		// Update scenario: start from existing, merge template fields
		desired = base.DeepCopy()

		desired.Spec.Template.Spec.InitContainers = template.Spec.Template.Spec.InitContainers
		desired.Spec.Template.Spec.Containers = template.Spec.Template.Spec.Containers
		desired.Spec.Template.Spec.Volumes = template.Spec.Template.Spec.Volumes

		// Merge labels/annotations (preserves external additions, overwrites controller-managed)
		desired.Labels = mergeMaps(desired.Labels, template.Labels)
		desired.Annotations = mergeMaps(desired.Annotations, template.Annotations)
		desired.Spec.Template.Labels = mergeMaps(desired.Spec.Template.Labels, template.Spec.Template.Labels)
		desired.Spec.Template.Annotations = mergeMaps(desired.Spec.Template.Annotations, template.Spec.Template.Annotations)
	}

	// Apply Gateway infrastructure metadata (overwrites tempalate/existing)
	mergeInfrastructureMetadata(&desired.ObjectMeta, gw)
	mergeInfrastructureMetadata(&desired.Spec.Template.ObjectMeta, gw)

	// Set deployment-specific values (always enforced)
	desired.Name = deploymentName
	desired.Namespace = gw.Namespace
	desired.Spec.Template.Spec.ServiceAccountName = serviceAccount

	// Set controller-managed labels
	setControllerLabels(&desired.ObjectMeta, deploymentName, gw.Name)
	setControllerLabels(&desired.Spec.Template.ObjectMeta, deploymentName, gw.Name)

	// Set selector (immutable, but safe to set on every reconcile)
	desired.Spec.Selector = &metav1.LabelSelector{
		MatchLabels: map[string]string{"app": deploymentName},
	}

	// Update pod anti-affinity to match deployment name
	updateAntiAffinityLabels(desired, deploymentName)

	// Apply GatewayConfiguration (replicas, resources, etc.) - after labels/annotations
	if gwConfig != nil {
		applyGatewayConfiguration(desired, gwConfig)
	}

	return desired
}

// deploymentNeedsUpdate checks if Deployment needs update using semantic equality
func deploymentNeedsUpdate(existing, desired *appsv1.Deployment) bool {
	return !apiequality.Semantic.DeepEqual(existing.Spec, desired.Spec) ||
		!maps.Equal(existing.Labels, desired.Labels) ||
		!maps.Equal(existing.Annotations, desired.Annotations)
}

// mergeMaps merges two maps, with values from 'overwrite' taking precedence
func mergeMaps(base, overwrite map[string]string) map[string]string {
	result := make(map[string]string, len(base)+len(overwrite))
	maps.Copy(result, base)
	maps.Copy(result, overwrite)
	return result
}

// lbDeploymentName generates the LB Deployment name for a Gateway
func lbDeploymentName(gw *gatewayv1.Gateway) string {
	return lbDeploymentPrefix + gw.Name
}

// setControllerLabels sets labels managed by the controller
func setControllerLabels(meta *metav1.ObjectMeta, deploymentName, gatewayName string) {
	if meta.Labels == nil {
		meta.Labels = make(map[string]string)
	}
	meta.Labels["app"] = deploymentName
	meta.Labels[labelGatewayName] = gatewayName
}

// mergeInfrastructureMetadata merges Gateway.spec.infrastructure labels/annotations
func mergeInfrastructureMetadata(meta *metav1.ObjectMeta, gw *gatewayv1.Gateway) {
	if gw.Spec.Infrastructure == nil {
		return
	}

	if meta.Labels == nil {
		meta.Labels = make(map[string]string)
	}
	if meta.Annotations == nil {
		meta.Annotations = make(map[string]string)
	}

	for k, v := range gw.Spec.Infrastructure.Labels {
		meta.Labels[string(k)] = string(v)
	}
	for k, v := range gw.Spec.Infrastructure.Annotations {
		meta.Annotations[string(k)] = string(v)
	}
}

// updateAntiAffinityLabels updates pod anti-affinity to match deployment-specific labels
func updateAntiAffinityLabels(deployment *appsv1.Deployment, deploymentName string) {
	if deployment.Spec.Template.Spec.Affinity == nil ||
		deployment.Spec.Template.Spec.Affinity.PodAntiAffinity == nil {
		return
	}

	for i := range deployment.Spec.Template.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution {
		term := &deployment.Spec.Template.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution[i]
		if term.LabelSelector != nil {
			for j := range term.LabelSelector.MatchExpressions {
				if term.LabelSelector.MatchExpressions[j].Key == "app" {
					term.LabelSelector.MatchExpressions[j].Values = []string{deploymentName}
				}
			}
		}
	}
}

// applyGatewayConfiguration applies GatewayConfiguration values to the Deployment
func applyGatewayConfiguration(deployment *appsv1.Deployment, gwConfig *meridio2v1alpha1.GatewayConfiguration) {
	// TODO: Apply configuration values (replicas, resources, etc.)
	// This will be implemented when we add GatewayConfiguration support
}
