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

package prerequisites

import (
	"fmt"
	"os"
	"path/filepath"
)

// CheckFile verifies that a file exists and is readable
func CheckFile(dir, filename string) error {
	fullPath := filepath.Join(dir, filename)
	info, err := os.Stat(fullPath)
	if err != nil {
		return fmt.Errorf("file %q not found in %q: %w", filename, dir, err)
	}
	if info.IsDir() {
		return fmt.Errorf("path %q is a directory, expected file", fullPath)
	}
	return nil
}
