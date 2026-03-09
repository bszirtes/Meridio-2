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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// shouldManageGateway checks if this controller should manage the Gateway
func (r *GatewayReconciler) shouldManageGateway(ctx context.Context, gw *gatewayv1.Gateway) (bool, error) {
	var gwClass gatewayv1.GatewayClass
	if err := r.Get(ctx, client.ObjectKey{Name: string(gw.Spec.GatewayClassName)}, &gwClass); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	return string(gwClass.Spec.ControllerName) == r.ControllerName, nil
}

// mapGatewayClassToGateway maps GatewayClass changes to Gateway reconciliation requests
func (r *GatewayReconciler) mapGatewayClassToGateway(ctx context.Context, obj client.Object) []ctrl.Request {
	gwClass, ok := obj.(*gatewayv1.GatewayClass)
	if !ok {
		return nil
	}

	// Only process GatewayClasses managed by this controller
	if string(gwClass.Spec.ControllerName) != r.ControllerName {
		return nil
	}

	// List all Gateways referencing this GatewayClass
	var gwList gatewayv1.GatewayList
	listOpts := []client.ListOption{}
	if r.Namespace != "" {
		listOpts = append(listOpts, client.InNamespace(r.Namespace))
	}
	if err := r.List(ctx, &gwList, listOpts...); err != nil {
		return nil
	}

	var requests []ctrl.Request
	for _, gw := range gwList.Items {
		if string(gw.Spec.GatewayClassName) == gwClass.Name {
			requests = append(requests, ctrl.Request{
				NamespacedName: client.ObjectKeyFromObject(&gw),
			})
		}
	}
	return requests
}
