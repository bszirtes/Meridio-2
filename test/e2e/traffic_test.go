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
	// Namespace A: Gateway gw-a, 3 target Pods
	vipAv4 = "20.0.0.1"
	vipAv6 = "2001:db8::1"
	portA  = 5000

	// Namespace B: Gateway gw-b, 2 target Pods
	vipBv4 = "30.0.0.1"
	vipBv6 = "2001:db8:1::1"
	portB  = 5000

	nconn = 100 // connections per traffic test
)

var _ = Describe("Single Gateway Traffic", Ordered, func() {

	SetDefaultEventuallyTimeout(5 * time.Minute)
	SetDefaultEventuallyPollingInterval(5 * time.Second)

	// Wait for BGP routes to propagate before traffic tests
	BeforeAll(func() {
		By("waiting for BGP routes to propagate to VPN gateway")
		Eventually(func() error {
			return e2eutils.Ping(vipAv4)
		}).Should(Succeed(), "VIP %s should be reachable via BGP", vipAv4)
	})

	Context("ICMP", func() {
		It("handles ping on IPv4 VIP", func() {
			Expect(e2eutils.Ping(vipAv4)).To(Succeed())
		})

		It("handles ping on IPv6 VIP", func() {
			Expect(e2eutils.Ping(vipAv6)).To(Succeed())
		})
	})

	Context("TCP", func() {
		It("load-balances TCP IPv4 traffic across all targets", func() {
			lastingConn, lostConn, err := e2eutils.SendTraffic(vipAv4, portA, "tcp", nconn)
			Expect(err).NotTo(HaveOccurred())
			Expect(lostConn).To(BeZero(), "no connections should be lost")
			Expect(len(lastingConn)).To(Equal(3),
				"all 3 target-a Pods should receive traffic: %v", lastingConn)
		})

		It("load-balances TCP IPv6 traffic across all targets", func() {
			lastingConn, lostConn, err := e2eutils.SendTraffic(vipAv6, portA, "tcp", nconn)
			Expect(err).NotTo(HaveOccurred())
			Expect(lostConn).To(BeZero())
			Expect(len(lastingConn)).To(Equal(3),
				"all 3 target-a Pods should receive traffic: %v", lastingConn)
		})
	})

	Context("UDP", func() {
		It("load-balances UDP IPv4 traffic across all targets", func() {
			lastingConn, lostConn, err := e2eutils.SendTraffic(vipAv4, portA, "udp", nconn)
			Expect(err).NotTo(HaveOccurred())
			Expect(lostConn).To(BeZero())
			Expect(len(lastingConn)).To(Equal(3),
				"all 3 target-a Pods should receive traffic: %v", lastingConn)
		})

		It("load-balances UDP IPv6 traffic across all targets", func() {
			lastingConn, lostConn, err := e2eutils.SendTraffic(vipAv6, portA, "udp", nconn)
			Expect(err).NotTo(HaveOccurred())
			Expect(lostConn).To(BeZero())
			Expect(len(lastingConn)).To(Equal(3),
				"all 3 target-a Pods should receive traffic: %v", lastingConn)
		})
	})
})

var _ = Describe("Multi-Gateway Traffic", Ordered, func() {

	SetDefaultEventuallyTimeout(5 * time.Minute)
	SetDefaultEventuallyPollingInterval(5 * time.Second)

	BeforeAll(func() {
		By("waiting for BGP routes for both Gateways")
		Eventually(func() error { return e2eutils.Ping(vipAv4) }).Should(Succeed())
		Eventually(func() error { return e2eutils.Ping(vipBv4) }).Should(Succeed())
	})

	Context("ICMP on both VIP sets", func() {
		It("handles ping on gw-a IPv4 VIP", func() {
			Expect(e2eutils.Ping(vipAv4)).To(Succeed())
		})

		It("handles ping on gw-b IPv4 VIP", func() {
			Expect(e2eutils.Ping(vipBv4)).To(Succeed())
		})

		It("handles ping on gw-a IPv6 VIP", func() {
			Expect(e2eutils.Ping(vipAv6)).To(Succeed())
		})

		It("handles ping on gw-b IPv6 VIP", func() {
			Expect(e2eutils.Ping(vipBv6)).To(Succeed())
		})
	})

	Context("TCP load balancing on separate Gateways", func() {
		It("distributes gw-a traffic to ns-a targets only", func() {
			lastingConn, _, err := e2eutils.SendTraffic(vipAv4, portA, "tcp", nconn)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(lastingConn)).To(Equal(3), "gw-a: expected 3 targets: %v", lastingConn)
		})

		It("distributes gw-b traffic to ns-b targets only", func() {
			lastingConn, _, err := e2eutils.SendTraffic(vipBv4, portB, "tcp", nconn)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(lastingConn)).To(Equal(2), "gw-b: expected 2 targets: %v", lastingConn)
		})
	})

	Context("UDP load balancing on IPv6 VIPs", func() {
		It("distributes gw-a IPv6 UDP to ns-a targets", func() {
			lastingConn, _, err := e2eutils.SendTraffic(vipAv6, portA, "udp", nconn)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(lastingConn)).To(Equal(3), "gw-a IPv6: expected 3 targets: %v", lastingConn)
		})

		It("distributes gw-b IPv6 UDP to ns-b targets", func() {
			lastingConn, _, err := e2eutils.SendTraffic(vipBv6, portB, "udp", nconn)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(lastingConn)).To(Equal(2), "gw-b IPv6: expected 2 targets: %v", lastingConn)
		})
	})
})
