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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func TestUpdateAcceptedStatus(t *testing.T) {
	t.Run("SetsAcceptedCondition", func(t *testing.T) {
		gwClass := newGatewayClass(testControllerName)
		gw := newGateway(gwClass.Name)

		reconciler, fakeClient := setupReconciler(gwClass, gw)

		err := reconciler.updateAcceptedStatus(context.Background(), gw, metav1.ConditionTrue,
			string(gatewayv1.GatewayReasonAccepted), reconciler.acceptedMessage())
		assert.NoError(t, err)

		// Verify condition was set
		fetched := &gatewayv1.Gateway{}
		err = fakeClient.Get(context.Background(), types.NamespacedName{Name: gw.Name, Namespace: gw.Namespace}, fetched)
		assert.NoError(t, err)

		var acceptedCondition *metav1.Condition
		for i := range fetched.Status.Conditions {
			if fetched.Status.Conditions[i].Type == string(gatewayv1.GatewayConditionAccepted) {
				acceptedCondition = &fetched.Status.Conditions[i]
				break
			}
		}

		assert.NotNil(t, acceptedCondition)
		assert.Equal(t, metav1.ConditionTrue, acceptedCondition.Status)
		assert.Equal(t, string(gatewayv1.GatewayReasonAccepted), acceptedCondition.Reason)
		assert.Contains(t, acceptedCondition.Message, testControllerName)
		assert.Equal(t, gw.Generation, acceptedCondition.ObservedGeneration)
	})
}

func TestUpdateProgrammedStatus(t *testing.T) {
	t.Run("SetsProgrammedCondition", func(t *testing.T) {
		gwClass := newGatewayClass(testControllerName)
		gw := newGateway(gwClass.Name)

		reconciler, fakeClient := setupReconciler(gwClass, gw)

		err := reconciler.updateProgrammedStatus(context.Background(), gw, metav1.ConditionTrue,
			string(gatewayv1.GatewayReasonProgrammed), messageProgrammed)
		assert.NoError(t, err)

		// Verify condition was set
		fetched := &gatewayv1.Gateway{}
		err = fakeClient.Get(context.Background(), types.NamespacedName{Name: gw.Name, Namespace: gw.Namespace}, fetched)
		assert.NoError(t, err)

		var programmedCondition *metav1.Condition
		for i := range fetched.Status.Conditions {
			if fetched.Status.Conditions[i].Type == string(gatewayv1.GatewayConditionProgrammed) {
				programmedCondition = &fetched.Status.Conditions[i]
				break
			}
		}

		assert.NotNil(t, programmedCondition)
		assert.Equal(t, metav1.ConditionTrue, programmedCondition.Status)
		assert.Equal(t, string(gatewayv1.GatewayReasonProgrammed), programmedCondition.Reason)
		assert.Equal(t, messageProgrammed, programmedCondition.Message)
		assert.Equal(t, gw.Generation, programmedCondition.ObservedGeneration)
	})

	t.Run("SetsProgrammedFalse", func(t *testing.T) {
		gwClass := newGatewayClass(testControllerName)
		gw := newGateway(gwClass.Name)

		reconciler, fakeClient := setupReconciler(gwClass, gw)

		err := reconciler.updateProgrammedStatus(context.Background(), gw, metav1.ConditionFalse,
			string(gatewayv1.GatewayReasonInvalid), "Deployment failed")
		assert.NoError(t, err)

		fetched := &gatewayv1.Gateway{}
		err = fakeClient.Get(context.Background(), types.NamespacedName{Name: gw.Name, Namespace: gw.Namespace}, fetched)
		assert.NoError(t, err)

		var programmedCondition *metav1.Condition
		for i := range fetched.Status.Conditions {
			if fetched.Status.Conditions[i].Type == string(gatewayv1.GatewayConditionProgrammed) {
				programmedCondition = &fetched.Status.Conditions[i]
				break
			}
		}

		assert.NotNil(t, programmedCondition)
		assert.Equal(t, metav1.ConditionFalse, programmedCondition.Status)
		assert.Equal(t, string(gatewayv1.GatewayReasonInvalid), programmedCondition.Reason)
		assert.Equal(t, "Deployment failed", programmedCondition.Message)
	})

	t.Run("UpdatesExistingCondition", func(t *testing.T) {
		gwClass := newGatewayClass(testControllerName)
		gw := newGateway(gwClass.Name)
		gw.Status.Conditions = []metav1.Condition{
			{
				Type:               string(gatewayv1.GatewayConditionProgrammed),
				Status:             metav1.ConditionFalse,
				Reason:             string(gatewayv1.GatewayReasonPending),
				Message:            "Old message",
				ObservedGeneration: gw.Generation,
			},
		}

		reconciler, fakeClient := setupReconciler(gwClass, gw)

		err := reconciler.updateProgrammedStatus(context.Background(), gw, metav1.ConditionTrue,
			string(gatewayv1.GatewayReasonProgrammed), messageProgrammed)
		assert.NoError(t, err)

		fetched := &gatewayv1.Gateway{}
		err = fakeClient.Get(context.Background(), types.NamespacedName{Name: gw.Name, Namespace: gw.Namespace}, fetched)
		assert.NoError(t, err)

		assert.Len(t, fetched.Status.Conditions, 1)
		assert.Equal(t, metav1.ConditionTrue, fetched.Status.Conditions[0].Status)
		assert.Equal(t, messageProgrammed, fetched.Status.Conditions[0].Message)
	})
}
