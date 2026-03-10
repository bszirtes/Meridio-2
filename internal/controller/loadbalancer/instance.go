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

	meridio2v1alpha1 "github.com/nordix/meridio-2/api/v1alpha1"
	"github.com/nordix/meridio-2/internal/nftables"
	"github.com/nordix/meridio/pkg/loadbalancer/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const defaultMaxEndpoints = 32

// reconcileNFQLBInstance creates or retrieves the NFQLB instance for a DistributionGroup.
func (c *Controller) reconcileNFQLBInstance(ctx context.Context, distGroup *meridio2v1alpha1.DistributionGroup) error {
	logr := log.FromContext(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()

	// Initialize maps if needed
	if c.instances == nil {
		c.instances = make(map[string]types.NFQueueLoadBalancer)
	}
	if c.nftManagers == nil {
		c.nftManagers = make(map[string]nftablesManager)
	}
	if c.targets == nil {
		c.targets = make(map[string]map[int][]string)
	}

	// Check if instance already exists
	if _, exists := c.instances[distGroup.Name]; exists {
		return nil
	}

	// Calculate M and N parameters for Maglev
	// M = N × 100 (as per design)
	// N = maxEndpoints from spec, default 32
	n := int32(defaultMaxEndpoints)
	if distGroup.Spec.Maglev != nil && distGroup.Spec.Maglev.MaxEndpoints > 0 {
		n = distGroup.Spec.Maglev.MaxEndpoints
	}
	m := n * 100

	// Create nftables manager
	var nftMgr nftablesManager
	var err error
	if c.NftManagerFactory != nil {
		nftMgr, err = c.NftManagerFactory(distGroup.Name, 0, 4)
	} else {
		nftMgr, err = nftables.NewManager(distGroup.Name, 0, 4)
	}
	if err != nil {
		return err
	}
	if err := nftMgr.Setup(); err != nil {
		return err
	}

	// Create NFQLB instance
	instance, err := c.LBFactory.New(distGroup.Name, int(m), int(n))
	if err != nil {
		_ = nftMgr.Cleanup() // Cleanup nftables on error
		return err
	}

	// Start the instance
	if err := instance.Start(); err != nil {
		_ = nftMgr.Cleanup() // Cleanup nftables on error
		return err
	}

	c.instances[distGroup.Name] = instance
	c.nftManagers[distGroup.Name] = nftMgr
	c.targets[distGroup.Name] = make(map[int][]string)

	logr.Info("Created NFQLB instance", "distGroup", distGroup.Name, "M", m, "N", n)

	return nil
}
