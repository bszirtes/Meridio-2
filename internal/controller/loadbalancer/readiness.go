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
	"fmt"
	"os"
	"path/filepath"
)

const defaultReadinessDir = "/var/run/meridio"

var readinessDir = defaultReadinessDir

// cleanupReadinessDir removes the readiness directory and all its contents on startup.
// This ensures a clean state when the controller starts.
func (c *Controller) cleanupReadinessDir() error {
	if err := os.RemoveAll(readinessDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove readiness directory: %w", err)
	}
	return nil
}

// createReadinessFile creates a readiness file for the DistributionGroup.
// This file can be used by liveness/readiness probes to verify the LB is operational.
func (c *Controller) createReadinessFile(distGroupName string) error {
	// Ensure directory exists
	if err := os.MkdirAll(readinessDir, 0755); err != nil {
		return fmt.Errorf("failed to create readiness directory: %w", err)
	}

	// Create readiness file
	filePath := filepath.Join(readinessDir, fmt.Sprintf("lb-ready-%s", distGroupName))
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create readiness file: %w", err)
	}
	_ = file.Close()

	return nil
}

// removeReadinessFile removes the readiness file for the DistributionGroup.
func (c *Controller) removeReadinessFile(distGroupName string) error {
	filePath := filepath.Join(readinessDir, fmt.Sprintf("lb-ready-%s", distGroupName))
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove readiness file: %w", err)
	}
	return nil
}
