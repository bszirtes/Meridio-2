//go:build e2e
// +build e2e

package e2e

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	e2eutils "github.com/nordix/meridio-2/test/e2e/utils"
)

const (
	// ns-a: gw-a1 and gw-a2, 2 target Pods
	vipA1 = "20.0.0.1"
	vipA2 = "20.0.0.2"

	// ns-b: gw-b1 and gw-b2, 2 target Pods
	vipB1 = "30.0.0.1"
	vipB2 = "30.0.0.2"

	port    = 5000
	udpPort = 5001
	nconn   = 100
)

var _ = Describe("Traffic", Ordered, func() {

	SetDefaultEventuallyTimeout(5 * time.Minute)
	SetDefaultEventuallyPollingInterval(5 * time.Second)

	BeforeAll(func() {
		By("waiting for BGP routes to propagate to VPN gateway")
		Eventually(func() error { return e2eutils.Ping(vipA1) }).Should(Succeed())
		Eventually(func() error { return e2eutils.Ping(vipA2) }).Should(Succeed())
		Eventually(func() error { return e2eutils.Ping(vipB1) }).Should(Succeed())
		Eventually(func() error { return e2eutils.Ping(vipB2) }).Should(Succeed())
	})

	Context("ICMP reachability on all VIPs", func() {
		for _, tc := range []struct{ name, vip string }{
			{"gw-a1", vipA1},
			{"gw-a2", vipA2},
			{"gw-b1", vipB1},
			{"gw-b2", vipB2},
		} {
			tc := tc
			It("handles ping on "+tc.name+" VIP", func() {
				Expect(e2eutils.Ping(tc.vip)).To(Succeed())
			})
		}
	})

	Context("TCP load balancing per gateway", func() {
		for _, tc := range []struct {
			name    string
			vip     string
			targets int
		}{
			{"gw-a1", vipA1, 2},
			{"gw-a2", vipA2, 2},
			{"gw-b1", vipB1, 2},
			{"gw-b2", vipB2, 2},
		} {
			tc := tc
			It("distributes "+tc.name+" TCP traffic across targets", func() {
				lastingConn, lostConn, err := e2eutils.SendTraffic(tc.vip, port, "tcp", nconn)
				Expect(err).NotTo(HaveOccurred())
				Expect(lostConn).To(BeZero(), "no connections should be lost")
				Expect(len(lastingConn)).To(Equal(tc.targets),
					"%s: expected %d targets, got: %v", tc.name, tc.targets, lastingConn)
			})
		}
	})

	// UDP load balancing uses port 5001 (separate from TCP on 5000).
	Context("UDP load balancing per gateway", func() {
		for _, tc := range []struct {
			name    string
			vip     string
			targets int
		}{
			{"gw-a1", vipA1, 2},
			{"gw-a2", vipA2, 2},
			{"gw-b1", vipB1, 2},
			{"gw-b2", vipB2, 2},
		} {
			tc := tc
			It("distributes "+tc.name+" UDP traffic across targets", func() {
				lastingConn, lostConn, err := e2eutils.SendTraffic(tc.vip, udpPort, "udp", nconn)
				Expect(err).NotTo(HaveOccurred())
				Expect(lostConn).To(BeZero(), "no connections should be lost")
				Expect(len(lastingConn)).To(Equal(tc.targets),
					"%s: expected %d targets, got: %v", tc.name, tc.targets, lastingConn)
			})
		}
	})
})
