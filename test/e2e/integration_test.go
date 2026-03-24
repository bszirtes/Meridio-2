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

	// ns-a: 2 gateways (gw-a1, gw-a2) with separate app-nets, 2 target pods
	Context("Gateways in e2e-ns-a", func() {
		for _, gw := range []string{"gw-a1", "gw-a2"} {
			gw := gw
			It(fmt.Sprintf("should have %s Accepted", gw), func() {
				Eventually(func(g Gomega) {
					cmd := exec.Command("kubectl", "get", "gateway", gw, "-n", "e2e-ns-a",
						"-o", "jsonpath={.status.conditions[?(@.type=='Accepted')].status}")
					out, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(out).To(Equal("True"))
				}).Should(Succeed())
			})

			It(fmt.Sprintf("should deploy SLLBR Pod for %s", gw), func() {
				Eventually(func(g Gomega) {
					cmd := exec.Command("kubectl", "get", "pods", "-n", "e2e-ns-a",
						"-l", fmt.Sprintf("gateway.networking.k8s.io/gateway-name=%s", gw),
						"-o", "jsonpath={.items[*].status.phase}")
					out, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(out).To(ContainSubstring("Running"))
				}).Should(Succeed())
			})
		}

		It("should create 2 ENCs (one per target Pod) with 2 gateways each", func() {
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "enc", "-n", "e2e-ns-a",
					"-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(len(utils.GetNonEmptyLines(out))).To(Equal(2))
			}).Should(Succeed())
		})

		It("should have 2 target Pods running with sidecar", func() {
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "-n", "e2e-ns-a",
					"-l", "app=target-a", "--field-selector=status.phase=Running",
					"-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(len(utils.GetNonEmptyLines(out))).To(Equal(2))
			}).Should(Succeed())
		})
	})

	// ns-b: 2 gateways (gw-b1, gw-b2) with shared app-net, 2 target pods
	Context("Gateways in e2e-ns-b", func() {
		for _, gw := range []string{"gw-b1", "gw-b2"} {
			gw := gw
			It(fmt.Sprintf("should have %s Accepted", gw), func() {
				Eventually(func(g Gomega) {
					cmd := exec.Command("kubectl", "get", "gateway", gw, "-n", "e2e-ns-b",
						"-o", "jsonpath={.status.conditions[?(@.type=='Accepted')].status}")
					out, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(out).To(Equal("True"))
				}).Should(Succeed())
			})

			It(fmt.Sprintf("should deploy SLLBR Pod for %s", gw), func() {
				Eventually(func(g Gomega) {
					cmd := exec.Command("kubectl", "get", "pods", "-n", "e2e-ns-b",
						"-l", fmt.Sprintf("gateway.networking.k8s.io/gateway-name=%s", gw),
						"-o", "jsonpath={.items[*].status.phase}")
					out, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(out).To(ContainSubstring("Running"))
				}).Should(Succeed())
			})
		}

		It("should create 2 ENCs (one per target Pod) with 2 gateways each", func() {
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "enc", "-n", "e2e-ns-b",
					"-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(len(utils.GetNonEmptyLines(out))).To(Equal(2))
			}).Should(Succeed())
		})
	})
})

func applyTestdata(ns string) {
	dir := fmt.Sprintf("test/e2e/testdata/%s/", ns)
	cmd := exec.Command("kubectl", "apply", "-f", dir)
	_, err := utils.Run(cmd)
	ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to apply testdata for %s", ns)
}

func cleanupNamespace(ns string) {
	cmd := exec.Command("kubectl", "delete", "ns", ns, "--ignore-not-found")
	_, _ = utils.Run(cmd)
}
