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
	"os"
	"path/filepath"
	"testing"
)

func TestReadinessFile(t *testing.T) {
	// Use temp directory for testing
	tempDir := t.TempDir()
	originalReadinessDir := readinessDir
	readinessDir = tempDir
	defer func() {
		readinessDir = originalReadinessDir
	}()

	controller := &Controller{}
	distGroupName := "test-distgroup"

	t.Run("create readiness file", func(t *testing.T) {
		err := controller.createReadinessFile(distGroupName)
		if err != nil {
			t.Fatalf("createReadinessFile failed: %v", err)
		}

		// Verify file exists
		filePath := filepath.Join(tempDir, "lb-ready-"+distGroupName)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Errorf("readiness file was not created: %s", filePath)
		}
	})

	t.Run("remove readiness file", func(t *testing.T) {
		err := controller.removeReadinessFile(distGroupName)
		if err != nil {
			t.Fatalf("removeReadinessFile failed: %v", err)
		}

		// Verify file removed
		filePath := filepath.Join(tempDir, "lb-ready-"+distGroupName)
		if _, err := os.Stat(filePath); !os.IsNotExist(err) {
			t.Errorf("readiness file was not removed: %s", filePath)
		}
	})

	t.Run("remove non-existent file", func(t *testing.T) {
		// Should not error when file doesn't exist
		err := controller.removeReadinessFile("non-existent")
		if err != nil {
			t.Errorf("removeReadinessFile should not error on non-existent file: %v", err)
		}
	})
}
