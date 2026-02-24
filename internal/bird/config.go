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
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	meridio2v1alpha1 "github.com/nordix/meridio-2/api/v1alpha1"
)

const (
	defaultKernelTableID = 4096
	defaultLocalPort     = 179
	defaultRemotePort    = 179
)

func vipsConfig(vips []string) string {
	var ipv4, ipv6 string
	for _, vip := range vips {
		if isIPv6(vip) {
			ipv6 += fmt.Sprintf("\troute %s via \"lo\";\n", vip)
		} else {
			ipv4 += fmt.Sprintf("\troute %s via \"lo\";\n", vip)
		}
	}

	conf := ""
	if ipv4 != "" {
		conf += fmt.Sprintf("protocol static VIP4 {\n\tipv4 { preference 110; };\n%s}\n", ipv4)
	}
	if ipv6 != "" {
		conf += fmt.Sprintf("protocol static VIP6 {\n\tipv6 { preference 110; };\n%s}\n", ipv6)
	}
	return conf
}

func baseConfig() string {
	return `log stderr all;

protocol device {}

filter gateway_routes {
	if ( net ~ [ 0.0.0.0/0 ] ) then accept;
	if ( net ~ [ 0::/0 ] ) then accept;
	if source = RTS_BGP then accept;
	else reject;
}

filter announced_routes {
	if ( net ~ [ 0.0.0.0/0 ] ) then reject;
	if ( net ~ [ 0::/0 ] ) then reject;
	if source = RTS_STATIC && dest != RTD_BLACKHOLE then accept;
	else reject;
}

template bgp BGP_TEMPLATE {
	debug {events, states};
	direct;
	hold time 90;
	bfd on;
	graceful restart off;
	setkey off;
	ipv4 {
		import none;
		export none;
		next hop self;
	};
	ipv6 {
		import none;
		export none;
		next hop self;
	};
}

protocol kernel {
	ipv4 {
		import none;
		export filter gateway_routes;
	};
	kernel table ` + fmt.Sprintf("%d", defaultKernelTableID) + `;
	merge paths on;
}

protocol kernel {
	ipv6 {
		import none;
		export filter gateway_routes;
	};
	kernel table ` + fmt.Sprintf("%d", defaultKernelTableID) + `;
	merge paths on;
}

protocol bfd {
	interface "*" {};
}`
}

func routersConfig(routers []*meridio2v1alpha1.GatewayRouter) (string, error) {
	var conf strings.Builder
	for _, router := range routers {
		routerConf, err := routerConfig(router)
		if err != nil {
			return "", err
		}
		conf.WriteString(routerConf)
		conf.WriteString("\n\n")
	}
	return conf.String(), nil
}

func routerConfig(router *meridio2v1alpha1.GatewayRouter) (string, error) {
	localPort := defaultLocalPort
	if router.Spec.BGP.LocalPort != nil {
		localPort = int(*router.Spec.BGP.LocalPort)
	}
	remotePort := defaultRemotePort
	if router.Spec.BGP.RemotePort != nil {
		remotePort = int(*router.Spec.BGP.RemotePort)
	}

	holdTime := "90"
	if router.Spec.BGP.HoldTime != "" {
		t, err := time.ParseDuration(router.Spec.BGP.HoldTime)
		if err != nil {
			return "Couldn't parse holdTime: " + err.Error(), err
		}
		holdTime = strconv.Itoa(int(t.Seconds()))
	}

	bfd := "bfd off;"
	if router.Spec.BGP.BFD != nil && router.Spec.BGP.BFD.Switch != nil && *router.Spec.BGP.BFD.Switch {
		bfdConf := ""
		if router.Spec.BGP.BFD.MinRx != "" {
			bfdConf += fmt.Sprintf("\t\tmin rx interval %s;\n", router.Spec.BGP.BFD.MinRx)
		}
		if router.Spec.BGP.BFD.MinTx != "" {
			bfdConf += fmt.Sprintf("\t\tmin tx interval %s;\n", router.Spec.BGP.BFD.MinTx)
		}
		if router.Spec.BGP.BFD.Multiplier != nil {
			bfdConf += fmt.Sprintf("\t\tmultiplier %d;\n", *router.Spec.BGP.BFD.Multiplier)
		}
		if bfdConf != "" {
			bfd = fmt.Sprintf("bfd {\n%s\t};", bfdConf)
		} else {
			bfd = "bfd on;"
		}
	}

	ipFamily := "ipv4"
	if isIPv6(router.Spec.Address) {
		ipFamily = "ipv6"
	}

	return fmt.Sprintf(`protocol bgp 'NBR-%s' from BGP_TEMPLATE {
	interface "%s";
	local port %d as %d;
	neighbor %s port %d as %d;
	%s
	hold time %s;
	%s {
		import filter gateway_routes;
		export filter announced_routes;
	};
}`, router.Name, router.Spec.Interface, localPort, router.Spec.BGP.LocalASN,
		router.Spec.Address, remotePort, router.Spec.BGP.RemoteASN,
		bfd, holdTime, ipFamily), nil
}

func isIPv6(ipOrCIDR string) bool {
	ip, _, err := net.ParseCIDR(ipOrCIDR)
	if err != nil {
		ip = net.ParseIP(ipOrCIDR)
	}
	return ip != nil && ip.To4() == nil
}
