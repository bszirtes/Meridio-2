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

package bird

import (
	"errors"
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

// setPolicyRoutes creates source-based routing rules for VIPs.
// Traffic from VIP addresses will use the BIRD routing table.
func setPolicyRoutes(vips []string) error {
	rules, err := netlink.RuleListFiltered(netlink.FAMILY_ALL, &netlink.Rule{
		Table: defaultKernelTableID,
	}, netlink.RT_FILTER_TABLE)
	if err != nil {
		return fmt.Errorf("failed to list rules: %w", err)
	}

	vipMap := make(map[string]*net.IPNet)
	for _, vip := range vips {
		_, ipNet, err := net.ParseCIDR(vip)
		if err != nil {
			continue
		}
		vipMap[ipNet.String()] = ipNet
	}

	var errFinal error
	for _, rule := range rules {
		if _, exists := vipMap[rule.Src.String()]; !exists {
			if err := netlink.RuleDel(&rule); err != nil {
				errFinal = errors.Join(errFinal, err)
			}
		} else {
			delete(vipMap, rule.Src.String())
		}
	}

	for _, ipNet := range vipMap {
		rule := netlink.NewRule()
		rule.Priority = 100
		rule.Table = defaultKernelTableID
		rule.Src = ipNet
		if err := netlink.RuleAdd(rule); err != nil {
			errFinal = errors.Join(errFinal, err)
		}
	}

	return errFinal
}
