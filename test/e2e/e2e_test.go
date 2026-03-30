//go:build e2e
// +build e2e

package e2e

import (
	"fmt"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	e2eutils "github.com/nordix/meridio-2/test/e2e/utils"
	"github.com/nordix/meridio-2/test/utils"
)

const (
	port    = 5000
	udpPort = 5001
	nconn   = 100
)

// nsTestCase describes a namespace-scoped e2e deployment and its expected traffic targets.
type nsTestCase struct {
	// name is the human-readable test name and testdata directory name.
	name string
	// namespace is the Kubernetes namespace for this deployment.
	namespace string
	targetApp string
	gateways  []gwTestCase
}

type gwTestCase struct {
	name    string
	vip     string
	targets int
}

var testCases = []nsTestCase{
	{
		name:      "separate-app-nets",
		namespace: "e2e-separate-app-nets",
		targetApp: "target-a",
		gateways: []gwTestCase{
			{name: "gw-a1", vip: "20.0.0.1", targets: 2},
			{name: "gw-a2", vip: "20.0.0.2", targets: 2},
		},
	},
	{
		name:      "shared-app-net",
		namespace: "e2e-shared-app-net",
		targetApp: "target-b",
		gateways: []gwTestCase{
			{name: "gw-b1", vip: "30.0.0.1", targets: 2},
			{name: "gw-b2", vip: "30.0.0.2", targets: 2},
		},
	},
}

func init() {
	for _, tc := range testCases {
		tc := tc
		Describe(tc.name, Ordered, func() {
			SetDefaultEventuallyTimeout(5 * time.Minute)
			SetDefaultEventuallyPollingInterval(5 * time.Second)

			BeforeAll(func() {
				By(fmt.Sprintf("deploying resources for %s", tc.namespace))
				cmd := exec.Command("kubectl", "apply", "-f",
					fmt.Sprintf("test/e2e/testdata/%s/", tc.name))
				_, err := utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred())
			})

			AfterAll(func() {
				cmd := exec.Command("kubectl", "delete", "ns", tc.namespace, "--ignore-not-found")
				_, _ = utils.Run(cmd)
			})

			// --- Deployment verification ---

			for _, gw := range tc.gateways {
				gw := gw
				It(fmt.Sprintf("should have %s Accepted", gw.name), func() {
					Eventually(func(g Gomega) {
						cmd := exec.Command("kubectl", "get", "gateway", gw.name,
							"-n", tc.namespace,
							"-o", "jsonpath={.status.conditions[?(@.type=='Accepted')].status}")
						out, err := utils.Run(cmd)
						g.Expect(err).NotTo(HaveOccurred())
						g.Expect(out).To(Equal("True"))
					}).Should(Succeed())
				})

				It(fmt.Sprintf("should deploy SLLBR pods for %s", gw.name), func() {
					Eventually(func(g Gomega) {
						cmd := exec.Command("kubectl", "get", "pods",
							"-n", tc.namespace,
							"-l", fmt.Sprintf("gateway.networking.k8s.io/gateway-name=%s", gw.name),
							"-o", "jsonpath={.items[*].status.phase}")
						out, err := utils.Run(cmd)
						g.Expect(err).NotTo(HaveOccurred())
						g.Expect(out).To(ContainSubstring("Running"))
					}).Should(Succeed())
				})
			}

			It("should create ENCs for target pods", func() {
				Eventually(func(g Gomega) {
					cmd := exec.Command("kubectl", "get", "enc", "-n", tc.namespace,
						"-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}")
					out, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(len(utils.GetNonEmptyLines(out))).To(Equal(2))
				}).Should(Succeed())
			})

			It("should have 2 target pods running", func() {
				Eventually(func(g Gomega) {
					cmd := exec.Command("kubectl", "get", "pods",
						"-n", tc.namespace,
						"-l", fmt.Sprintf("app=%s", tc.targetApp),
						"--field-selector=status.phase=Running",
						"-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}")
					out, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(len(utils.GetNonEmptyLines(out))).To(Equal(2))
				}).Should(Succeed())
			})

			// --- Traffic tests ---

			Context("ICMP reachability", func() {
				for _, gw := range tc.gateways {
					gw := gw
					It(fmt.Sprintf("handles ping on %s VIP", gw.name), func() {
						Eventually(func() error {
							return e2eutils.Ping(gw.vip)
						}).Should(Succeed())
					})
				}
			})

			Context("TCP load balancing", func() {
				for _, gw := range tc.gateways {
					gw := gw
					It(fmt.Sprintf("distributes %s TCP traffic across targets", gw.name), func() {
						lastingConn, lostConn, err := e2eutils.SendTraffic(gw.vip, port, "tcp", nconn)
						Expect(err).NotTo(HaveOccurred())
						Expect(lostConn).To(BeZero(), "no connections should be lost")
						Expect(len(lastingConn)).To(Equal(gw.targets),
							"%s: expected %d targets, got: %v", gw.name, gw.targets, lastingConn)
					})
				}
			})

			Context("UDP load balancing", func() {
				for _, gw := range tc.gateways {
					gw := gw
					It(fmt.Sprintf("distributes %s UDP traffic across targets", gw.name), func() {
						lastingConn, lostConn, err := e2eutils.SendTraffic(gw.vip, udpPort, "udp", nconn)
						Expect(err).NotTo(HaveOccurred())
						Expect(lostConn).To(BeZero(), "no connections should be lost")
						Expect(len(lastingConn)).To(Equal(gw.targets),
							"%s: expected %d targets, got: %v", gw.name, gw.targets, lastingConn)
					})
				}
			})
		})
	}
}
