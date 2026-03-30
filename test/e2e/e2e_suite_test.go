//go:build e2e
// +build e2e

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

package e2e

import (
	"fmt"
	"os/exec"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nordix/meridio-2/test/utils"
)

// TestE2E runs the e2e test suite.
// Expects a running Kind cluster with Multus, Whereabouts, Gateway API CRDs,
// cert-manager, controller-manager deployed, and VPN gateway container running.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting meridio-2 e2e test suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	By("applying cluster-scoped common resources")
	cmd := exec.Command("kubectl", "apply", "-f", "test/e2e/testdata/common/")
	_, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to apply common testdata (GatewayClass)")
})

var _ = AfterSuite(func() {
	By("cleaning up cluster-scoped common resources")
	cmd := exec.Command("kubectl", "delete", "-f", "test/e2e/testdata/common/", "--ignore-not-found")
	_, _ = utils.Run(cmd)
})
