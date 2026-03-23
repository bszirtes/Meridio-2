//go:build e2e
// +build e2e

package e2e

import (
	"fmt"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/nordix/meridio-2/test/utils"
)

var _ = Describe("System Deployment", Ordered, func() {

	BeforeAll(func() {
		By("creating test namespaces and applying resources for ns-a")
		applyTestdata("ns-a")

		By("creating test namespaces and applying resources for ns-b")
		applyTestdata("ns-b")
	})

	AfterAll(func() {
		cleanupNamespace("e2e-ns-a")
		cleanupNamespace("e2e-ns-b")
	})

	SetDefaultEventuallyTimeout(5 * time.Minute)
	SetDefaultEventuallyPollingInterval(2 * time.Second)

	Context("Gateway A in e2e-ns-a", func() {
		It("should have Gateway Accepted", func() {
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "gateway", "gw-a", "-n", "e2e-ns-a",
					"-o", "jsonpath={.status.conditions[?(@.type=='Accepted')].status}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("True"))
			}).Should(Succeed())
		})

		It("should deploy SLLBR Pods", func() {
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "-n", "e2e-ns-a",
					"-l", "gateway.networking.k8s.io/gateway-name=gw-a",
					"-o", "jsonpath={.items[*].status.phase}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(ContainSubstring("Running"))
			}).Should(Succeed())
		})

		It("should create ENC for each target Pod", func() {
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "enc", "-n", "e2e-ns-a",
					"-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				names := utils.GetNonEmptyLines(out)
				g.Expect(len(names)).To(BeNumerically(">=", 3), "Expected ENC for each of 3 target Pods")
			}).Should(Succeed())
		})

		It("should have target Pods running with sidecar", func() {
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "-n", "e2e-ns-a",
					"-l", "app=target-a", "--field-selector=status.phase=Running",
					"-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				pods := utils.GetNonEmptyLines(out)
				g.Expect(len(pods)).To(Equal(3))
			}).Should(Succeed())
		})
	})

	Context("Gateway B in e2e-ns-b", func() {
		It("should have Gateway Accepted", func() {
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "gateway", "gw-b", "-n", "e2e-ns-b",
					"-o", "jsonpath={.status.conditions[?(@.type=='Accepted')].status}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(Equal("True"))
			}).Should(Succeed())
		})

		It("should deploy SLLBR Pods", func() {
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "-n", "e2e-ns-b",
					"-l", "gateway.networking.k8s.io/gateway-name=gw-b",
					"-o", "jsonpath={.items[*].status.phase}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(out).To(ContainSubstring("Running"))
			}).Should(Succeed())
		})

		It("should create ENC for each target Pod", func() {
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "enc", "-n", "e2e-ns-b",
					"-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				names := utils.GetNonEmptyLines(out)
				g.Expect(len(names)).To(BeNumerically(">=", 2), "Expected ENC for each of 2 target Pods")
			}).Should(Succeed())
		})
	})
})

func applyTestdata(ns string) {
	// Apply namespace-specific resources (creates the namespace + NADs, Gateway, routes, targets, etc.)
	dir := fmt.Sprintf("test/e2e/testdata/%s/", ns)
	cmd := exec.Command("kubectl", "apply", "-f", dir)
	_, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to apply testdata for %s", ns)
}

func cleanupNamespace(ns string) {
	cmd := exec.Command("kubectl", "delete", "ns", ns, "--ignore-not-found")
	_, _ = utils.Run(cmd)
}
