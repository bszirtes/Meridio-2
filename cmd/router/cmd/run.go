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

package cmd

import (
	"context"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	meridio2v1alpha1 "github.com/nordix/meridio-2/api/v1alpha1"
	"github.com/nordix/meridio-2/internal/controller"
)

type runOptions struct {
	name      string
	namespace string
	logLevel  string
}

func newCmdRun() *cobra.Command {
	opts := &runOptions{}

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the router",
		Long:  `Run the router controller`,
		Run: func(cmd *cobra.Command, _ []string) {
			opts.run(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&opts.name, "name", "", "name of the gateway")
	cmd.Flags().StringVar(&opts.namespace, "namespace", "default", "namespace of the gateway")
	cmd.Flags().StringVar(&opts.logLevel, "log-level", "info", "log level (debug, info, warn, error)")

	return cmd
}

func (ro *runOptions) run(ctx context.Context) {
	scheme := runtime.NewScheme()
	setupLog := ctrl.Log.WithName("setup")

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(meridio2v1alpha1.AddToScheme(scheme))
	utilruntime.Must(gatewayapiv1.Install(scheme))

	l := zap.New(zap.UseDevMode(true))
	ctrl.SetLogger(l)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:         scheme,
		LeaderElection: false,
		Cache:          cache.Options{},
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		HealthProbeBindAddress: ":8082",
	})
	if err != nil {
		setupLog.Error(err, "failed to create manager")
		panic(err)
	}

	// TODO: Initialize routing suite (BIRD/FRR)

	if err = (&controller.GatewayRouterReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		GatewayName:      ro.name,
		GatewayNamespace: ro.namespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "failed to create controller", "controller", "GatewayRouter")
		panic(err)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		panic(err)
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		panic(err)
	}

	setupLog.Info("starting router", "gateway", ro.name, "namespace", ro.namespace)

	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "failed to start manager")
		panic(err)
	}
}
