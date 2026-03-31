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
	e2eutils "github.com/nordix/meridio-2/test/e2e/utils"
)

const (
	separateAppnetNamespace = "e2e-separate-appnet"
	vipA1                   = "20.0.0.1"
	vipA2                   = "20.0.0.2"
)

var _ = Describe("Separate App Network Suite", Ordered, func() {

	SetDefaultEventuallyTimeout(5 * time.Minute)
	SetDefaultEventuallyPollingInterval(2 * time.Second)

	// Deployment validation
	Context("Deployment", func() {
		for _, gw := range []string{"gw-a1", "gw-a2"} {
			gw := gw
			It(fmt.Sprintf("should have %s Accepted", gw), func() {
				Eventually(func(g Gomega) {
					cmd := exec.Command("kubectl", "get", "gateway", gw, "-n", separateAppnetNamespace,
						"-o", "jsonpath={.status.conditions[?(@.type=='Accepted')].status}")
					out, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(out).To(Equal("True"))
				}).Should(Succeed())
			})

			It(fmt.Sprintf("should deploy LB Pod for %s", gw), func() {
				Eventually(func(g Gomega) {
					cmd := exec.Command("kubectl", "get", "pods", "-n", separateAppnetNamespace,
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
				cmd := exec.Command("kubectl", "get", "enc", "-n", separateAppnetNamespace,
					"-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(len(utils.GetNonEmptyLines(out))).To(Equal(2))
			}).Should(Succeed())
		})

		It("should have 2 target Pods running with sidecar", func() {
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods", "-n", separateAppnetNamespace,
					"-l", "app=target-a", "--field-selector=status.phase=Running",
					"-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}")
				out, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(len(utils.GetNonEmptyLines(out))).To(Equal(2))
			}).Should(Succeed())
		})
	})

	// Traffic validation
	Context("Traffic", func() {
		BeforeAll(func() {
			By("waiting for BGP routes to propagate to VPN gateway")
			Eventually(func() error { return e2eutils.Ping(vipA1) }).Should(Succeed())
			Eventually(func() error { return e2eutils.Ping(vipA2) }).Should(Succeed())
		})

		Context("ICMP reachability", func() {
			for _, tc := range []struct{ name, vip string }{
				{"gw-a1", vipA1},
				{"gw-a2", vipA2},
			} {
				tc := tc
				It("handles ping on "+tc.name+" VIP", func() {
					Expect(e2eutils.Ping(tc.vip)).To(Succeed())
				})
			}
		})

		Context("TCP load balancing", func() {
			for _, tc := range []struct {
				name    string
				vip     string
				targets int
			}{
				{"gw-a1", vipA1, 2},
				{"gw-a2", vipA2, 2},
			} {
				tc := tc
				It("distributes "+tc.name+" TCP traffic across targets", func() {
					lastingConn, lostConn, err := e2eutils.SendTraffic(tc.vip, 5000, "tcp", 100)
					Expect(err).NotTo(HaveOccurred())
					Expect(lostConn).To(BeZero(), "no connections should be lost")
					Expect(len(lastingConn)).To(Equal(tc.targets),
						"%s: expected %d targets, got: %v", tc.name, tc.targets, lastingConn)
				})
			}
		})

		Context("UDP load balancing", func() {
			for _, tc := range []struct {
				name    string
				vip     string
				targets int
			}{
				{"gw-a1", vipA1, 2},
				{"gw-a2", vipA2, 2},
			} {
				tc := tc
				It("distributes "+tc.name+" UDP traffic across targets", func() {
					lastingConn, lostConn, err := e2eutils.SendTraffic(tc.vip, 5001, "udp", 100)
					Expect(err).NotTo(HaveOccurred())
					Expect(lostConn).To(BeZero(), "no connections should be lost")
					Expect(len(lastingConn)).To(Equal(tc.targets),
						"%s: expected %d targets, got: %v", tc.name, tc.targets, lastingConn)
				})
			}
		})
	})
})
