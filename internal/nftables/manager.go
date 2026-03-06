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

package nftables

import (
	"fmt"
	"net"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

const (
	preroutingChainName = "prerouting"
	outputChainName     = "output"
	ipv4VIPSetName      = "ipv4-vips"
	ipv6VIPSetName      = "ipv6-vips"
	nftqueueFlagFanout  = 0x01
)

// Manager manages nftables rules for VIP traffic.
type Manager struct {
	tableName   string
	queueNum    uint16
	queueTotal  uint16
	table       *nftables.Table
	preChain    *nftables.Chain
	outputChain *nftables.Chain
	ipv4Set     *nftables.Set
	ipv6Set     *nftables.Set
	ipv4PreRule *nftables.Rule
	ipv6PreRule *nftables.Rule
	ipv4OutRule *nftables.Rule
	ipv6OutRule *nftables.Rule
	conn        *nftables.Conn
}

// NewManager creates a new nftables manager.
func NewManager(distGroupName string, queueNum, queueTotal uint16) (*Manager, error) {
	return &Manager{
		tableName:  fmt.Sprintf("meridio-lb-%s", distGroupName),
		queueNum:   queueNum,
		queueTotal: queueTotal,
		conn:       &nftables.Conn{},
	}, nil
}

// Setup creates the nftables table, chains, and sets.
func (m *Manager) Setup() error {
	if err := m.createTable(); err != nil {
		return err
	}
	if err := m.createSets(); err != nil {
		return err
	}
	if err := m.createPreroutingChain(); err != nil {
		return err
	}
	if err := m.createOutputChain(); err != nil {
		return err
	}
	return nil
}

// SetVIPs updates the VIP sets with the given CIDRs.
func (m *Manager) SetVIPs(cidrs []string) error {
	vips := deduplicateVIPs(cidrs)
	ipv4, ipv6 := splitIPv4AndIPv6(vips)

	if err := m.updateSet(m.ipv4Set, ipv4); err != nil {
		return fmt.Errorf("failed to update IPv4 VIPs: %w", err)
	}
	if err := m.updateSet(m.ipv6Set, ipv6); err != nil {
		return fmt.Errorf("failed to update IPv6 VIPs: %w", err)
	}
	return nil
}

// Cleanup removes the nftables table and all associated rules.
func (m *Manager) Cleanup() error {
	m.conn.DelTable(m.table)
	return m.conn.Flush()
}

func (m *Manager) createTable() error {
	m.table = m.conn.AddTable(&nftables.Table{
		Name:   m.tableName,
		Family: nftables.TableFamilyINet,
	})
	return m.conn.Flush()
}

func (m *Manager) createSets() error {
	m.ipv4Set = &nftables.Set{
		Table:    m.table,
		Name:     ipv4VIPSetName,
		KeyType:  nftables.TypeIPAddr,
		Interval: true,
	}
	if err := m.conn.AddSet(m.ipv4Set, []nftables.SetElement{}); err != nil {
		return fmt.Errorf("failed to create IPv4 set: %w", err)
	}

	m.ipv6Set = &nftables.Set{
		Table:    m.table,
		Name:     ipv6VIPSetName,
		KeyType:  nftables.TypeIP6Addr,
		Interval: true,
	}
	if err := m.conn.AddSet(m.ipv6Set, []nftables.SetElement{}); err != nil {
		return fmt.Errorf("failed to create IPv6 set: %w", err)
	}

	return m.conn.Flush()
}

func (m *Manager) createPreroutingChain() error {
	m.preChain = m.conn.AddChain(&nftables.Chain{
		Name:     preroutingChainName,
		Table:    m.table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookPrerouting,
		Priority: nftables.ChainPriorityFilter,
	})

	// IPv4 rule: ip daddr @ipv4-vips counter queue num X-Y fanout
	m.ipv4PreRule = m.conn.AddRule(&nftables.Rule{
		Table: m.table,
		Chain: m.preChain,
		Exprs: []expr.Any{
			&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{unix.AF_INET}},
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 4},
			&expr.Lookup{SourceRegister: 1, SetName: m.ipv4Set.Name, SetID: m.ipv4Set.ID},
			&expr.Counter{},
			&expr.Queue{Num: m.queueNum, Total: m.queueTotal, Flag: nftqueueFlagFanout},
		},
	})

	// IPv6 rule: ip6 daddr @ipv6-vips counter queue num X-Y fanout
	m.ipv6PreRule = m.conn.AddRule(&nftables.Rule{
		Table: m.table,
		Chain: m.preChain,
		Exprs: []expr.Any{
			&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{unix.AF_INET6}},
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 24, Len: 16},
			&expr.Lookup{SourceRegister: 1, SetName: m.ipv6Set.Name, SetID: m.ipv6Set.ID},
			&expr.Counter{},
			&expr.Queue{Num: m.queueNum, Total: m.queueTotal, Flag: nftqueueFlagFanout},
		},
	})

	return m.conn.Flush()
}

func (m *Manager) createOutputChain() error {
	m.outputChain = m.conn.AddChain(&nftables.Chain{
		Name:     outputChainName,
		Table:    m.table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookOutput,
		Priority: nftables.ChainPriorityFilter,
	})

	// IPv4 rule: meta l4proto icmp ip daddr @ipv4-vips counter queue num X-Y fanout
	m.ipv4OutRule = m.conn.AddRule(&nftables.Rule{
		Table: m.table,
		Chain: m.outputChain,
		Exprs: []expr.Any{
			&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{unix.AF_INET}},
			&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{unix.IPPROTO_ICMP}},
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 4},
			&expr.Lookup{SourceRegister: 1, SetName: m.ipv4Set.Name, SetID: m.ipv4Set.ID},
			&expr.Counter{},
			&expr.Queue{Num: m.queueNum, Total: m.queueTotal, Flag: nftqueueFlagFanout},
		},
	})

	// IPv6 rule: meta l4proto icmpv6 ip6 daddr @ipv6-vips counter queue num X-Y fanout
	m.ipv6OutRule = m.conn.AddRule(&nftables.Rule{
		Table: m.table,
		Chain: m.outputChain,
		Exprs: []expr.Any{
			&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{unix.AF_INET6}},
			&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{unix.IPPROTO_ICMPV6}},
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 24, Len: 16},
			&expr.Lookup{SourceRegister: 1, SetName: m.ipv6Set.Name, SetID: m.ipv6Set.ID},
			&expr.Counter{},
			&expr.Queue{Num: m.queueNum, Total: m.queueTotal, Flag: nftqueueFlagFanout},
		},
	})

	return m.conn.Flush()
}

func (m *Manager) updateSet(set *nftables.Set, cidrs []string) error {
	elements := []nftables.SetElement{}
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			return fmt.Errorf("invalid CIDR %s: %w", cidr, err)
		}
		elements = append(elements, cidrToSetElements(ipNet)...)
	}

	m.conn.FlushSet(set)
	if err := m.conn.SetAddElements(set, elements); err != nil {
		return fmt.Errorf("failed to add elements: %w", err)
	}
	return m.conn.Flush()
}

func cidrToSetElements(ipNet *net.IPNet) []nftables.SetElement {
	start := ipNet.IP
	end := nextIP(broadcast(ipNet))

	// Normalize to IPv4 if applicable
	if v4 := start.To4(); v4 != nil {
		start = v4
		end = end.To4()
	}

	return []nftables.SetElement{
		{Key: start, IntervalEnd: false},
		{Key: end, IntervalEnd: true},
	}
}

func broadcast(ipNet *net.IPNet) net.IP {
	ip := ipNet.IP
	mask := ipNet.Mask
	broadcast := make(net.IP, len(ip))
	for i := range ip {
		broadcast[i] = ip[i] | ^mask[i]
	}
	return broadcast
}

func nextIP(ip net.IP) net.IP {
	next := make(net.IP, len(ip))
	copy(next, ip)
	for i := len(next) - 1; i >= 0; i-- {
		next[i]++
		if next[i] != 0 {
			break
		}
	}
	return next
}

func deduplicateVIPs(cidrs []string) []string {
	seen := make(map[string]struct{})
	result := []string{}
	for _, cidr := range cidrs {
		if _, exists := seen[cidr]; !exists {
			seen[cidr] = struct{}{}
			result = append(result, cidr)
		}
	}
	return result
}

func splitIPv4AndIPv6(cidrs []string) ([]string, []string) {
	ipv4 := []string{}
	ipv6 := []string{}
	for _, cidr := range cidrs {
		ip, _, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if ip.To4() != nil {
			ipv4 = append(ipv4, cidr)
		} else {
			ipv6 = append(ipv6, cidr)
		}
	}
	return ipv4, ipv6
}
