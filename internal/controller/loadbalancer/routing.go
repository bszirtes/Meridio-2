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
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

// RoutingManager manages policy routing for load balancer targets
type RoutingManager struct {
	// Track configured fwmarks to avoid duplicates
	configuredFwmarks map[int]routeInfo
	// Allow disabling for tests
	disabled bool
}

type routeInfo struct {
	tableID int
	gateway net.IP
}

// NewRoutingManager creates a new routing manager
func NewRoutingManager() *RoutingManager {
	return &RoutingManager{
		configuredFwmarks: make(map[int]routeInfo),
		disabled:          false,
	}
}

// NewMockRoutingManager creates a disabled routing manager for tests
func NewMockRoutingManager() *RoutingManager {
	return &RoutingManager{
		configuredFwmarks: make(map[int]routeInfo),
		disabled:          true,
	}
}

// AddRoute configures policy routing for a target
// Creates: ip rule add fwmark <fwmark> table <tableID>
//
//	ip route add default via <targetIP> table <tableID>
//
// Kernel will determine the interface based on target IP subnet
func (r *RoutingManager) AddRoute(fwmark int, targetIP string) error {
	// Skip if disabled (for tests)
	if r.disabled {
		r.configuredFwmarks[fwmark] = routeInfo{tableID: fwmark}
		return nil
	}

	tableID := fwmark // Use fwmark as table ID

	// Parse target IP
	ip := net.ParseIP(targetIP)
	if ip == nil {
		return fmt.Errorf("invalid target IP: %s", targetIP)
	}

	// Check if route already exists and is valid
	if r.isValidRoute(fwmark, ip) {
		r.configuredFwmarks[fwmark] = routeInfo{
			tableID: tableID,
			gateway: ip,
		}
		return nil
	}

	// Cleanup old route if exists (might be stale)
	r.deleteRouteInternal(fwmark, ip)

	// Clean stale neighbor entries
	_ = r.cleanNeighbor(ip)

	// Determine IP family
	family := netlink.FAMILY_V6
	if ip.To4() != nil {
		family = netlink.FAMILY_V4
	}

	// Add policy routing rule: fwmark -> table
	rule := netlink.NewRule()
	rule.Mark = uint32(fwmark)
	rule.Table = tableID
	rule.Family = family
	rule.Priority = 32000 // Standard priority for fwmark rules

	if err := netlink.RuleAdd(rule); err != nil {
		return fmt.Errorf("failed to add routing rule for fwmark %d: %w", fwmark, err)
	}

	// Add default route in custom table (kernel finds interface)
	route := &netlink.Route{
		Gw:    ip,
		Table: tableID,
	}

	if err := netlink.RouteAdd(route); err != nil {
		// Cleanup rule on failure
		_ = netlink.RuleDel(rule)
		return fmt.Errorf("failed to add route for fwmark %d: %w", fwmark, err)
	}

	r.configuredFwmarks[fwmark] = routeInfo{
		tableID: tableID,
		gateway: ip,
	}
	return nil
}

// isValidRoute checks if the route already exists and is correct
func (r *RoutingManager) isValidRoute(fwmark int, ip net.IP) bool {
	info, exists := r.configuredFwmarks[fwmark]
	if !exists {
		return false
	}

	family := netlink.FAMILY_V6
	if ip.To4() != nil {
		family = netlink.FAMILY_V4
	}

	route := &netlink.Route{
		Gw:    ip,
		Table: info.tableID,
	}

	routes, err := netlink.RouteListFiltered(family, route, netlink.RT_FILTER_GW|netlink.RT_FILTER_TABLE)
	if err != nil {
		return false
	}

	return len(routes) == 1 && routes[0].Gw.Equal(ip)
}

// cleanNeighbor removes stale ARP/NDP entries for the IP
func (r *RoutingManager) cleanNeighbor(ip net.IP) error {
	neighbors, err := netlink.NeighList(0, 0)
	if err != nil {
		return err
	}

	for _, neighbor := range neighbors {
		if neighbor.IP.Equal(ip) {
			_ = netlink.NeighDel(&neighbor)
		}
	}

	return nil
}

// deleteRouteInternal deletes route without checking configuredFwmarks
func (r *RoutingManager) deleteRouteInternal(fwmark int, ip net.IP) {
	tableID := fwmark

	family := netlink.FAMILY_V6
	if ip.To4() != nil {
		family = netlink.FAMILY_V4
	}

	rule := netlink.NewRule()
	rule.Mark = uint32(fwmark)
	rule.Table = tableID
	rule.Family = family
	rule.Priority = 32000

	_ = netlink.RuleDel(rule)

	route := &netlink.Route{
		Gw:    ip,
		Table: tableID,
	}

	_ = netlink.RouteDel(route)
}

// DeleteRoute removes policy routing for a target
func (r *RoutingManager) DeleteRoute(fwmark int) error {
	info, exists := r.configuredFwmarks[fwmark]
	if !exists {
		return nil
	}

	// Skip actual deletion if disabled (for tests)
	if r.disabled {
		delete(r.configuredFwmarks, fwmark)
		return nil
	}

	// Delete using internal method
	r.deleteRouteInternal(fwmark, info.gateway)

	delete(r.configuredFwmarks, fwmark)
	return nil
}

// Cleanup removes all configured routes
func (r *RoutingManager) Cleanup() error {
	var lastErr error
	for fwmark := range r.configuredFwmarks {
		if err := r.DeleteRoute(fwmark); err != nil {
			lastErr = err
		}
	}
	return lastErr
}
