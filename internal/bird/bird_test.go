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
	"strings"
	"testing"
	"text/template"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meridio2v1alpha1 "github.com/nordix/meridio-2/api/v1alpha1"
)

// wantConfigParams holds the variable parts of a reference bird config.
type wantConfigParams struct {
	BGPInterfaces []string
	IPv4VIPs      []string
	IPv6VIPs      []string
	Routers       []wantRouter
}

type wantRouter struct {
	Name       string
	Interface  string
	LocalPort  int
	LocalASN   uint32
	Address    string
	RemotePort int
	RemoteASN  uint32
	BFD        string
	HoldTime   string
	IPFamily   string
}

var wantConfigTmpl = template.Must(template.New("want").Parse(`log stderr all;

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
	bfd off;
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
	kernel table 4096;
	merge paths on;
}

protocol kernel {
	ipv6 {
		import none;
		export filter gateway_routes;
	};
	kernel table 4096;
	merge paths on;
}

protocol bfd {
{{- range .BGPInterfaces}}
	interface "{{.}}" {};
{{- end}}
}
{{- if .IPv4VIPs}}

protocol static VIP4 {
	ipv4 { preference 110; };
{{- range .IPv4VIPs}}
	route {{.}} via "lo";
{{- end}}
}
{{- end}}
{{- if .IPv6VIPs}}

protocol static VIP6 {
	ipv6 { preference 110; };
{{- range .IPv6VIPs}}
	route {{.}} via "lo";
{{- end}}
}
{{- end}}
{{- range .Routers}}

protocol bgp 'NBR-{{.Name}}' from BGP_TEMPLATE {
	interface "{{.Interface}}";
	local port {{.LocalPort}} as {{.LocalASN}};
	neighbor {{.Address}} port {{.RemotePort}} as {{.RemoteASN}};
	{{.BFD}}
	hold time {{.HoldTime}};
	{{.IPFamily}} {
		import filter gateway_routes;
		export filter announced_routes;
	};
}
{{- end}}`))

func buildWantConfig(t *testing.T, p wantConfigParams) string {
	t.Helper()
	var buf strings.Builder
	if err := wantConfigTmpl.Execute(&buf, p); err != nil {
		t.Fatalf("failed to build want config: %v", err)
	}
	return buf.String()
}

func TestGenerateConfig(t *testing.T) {
	b := New()

	t.Run("empty config", func(t *testing.T) {
		conf, err := b.generateConfig([]string{}, []*meridio2v1alpha1.GatewayRouter{})
		if err != nil {
			t.Fatalf("generateConfig() error = %v", err)
		}
		if !strings.Contains(conf, "protocol device") {
			t.Error("missing base config")
		}
	})

	t.Run("with vips", func(t *testing.T) {
		vips := []string{"20.0.0.1/32", "2001:db8::1/128"}
		conf, err := b.generateConfig(vips, []*meridio2v1alpha1.GatewayRouter{})
		if err != nil {
			t.Fatalf("generateConfig() error = %v", err)
		}
		if !strings.Contains(conf, "protocol static VIP4") {
			t.Error("missing VIP4 config")
		}
		if !strings.Contains(conf, "protocol static VIP6") {
			t.Error("missing VIP6 config")
		}
	})

	t.Run("with router", func(t *testing.T) {
		router := &meridio2v1alpha1.GatewayRouter{
			ObjectMeta: metav1.ObjectMeta{Name: "test-router"},
			Spec: meridio2v1alpha1.GatewayRouterSpec{
				Address: "192.168.1.1",
				BGP: meridio2v1alpha1.BgpSpec{
					RemoteASN: 65000,
					LocalASN:  65001,
				},
			},
		}
		conf, err := b.generateConfig([]string{}, []*meridio2v1alpha1.GatewayRouter{router})
		if err != nil {
			t.Fatalf("generateConfig() error = %v", err)
		}
		if !strings.Contains(conf, "protocol bgp 'NBR-test-router'") {
			t.Error("missing BGP protocol with NBR- prefix")
		}
		if !strings.Contains(conf, "neighbor 192.168.1.1") {
			t.Error("missing neighbor address")
		}
	})

	t.Run("matches reference config", func(t *testing.T) {
		router := &meridio2v1alpha1.GatewayRouter{
			ObjectMeta: metav1.ObjectMeta{Name: "gatewayrouter-sample"},
			Spec: meridio2v1alpha1.GatewayRouterSpec{
				Interface: "vlan-100",
				Address:   "169.254.100.150",
				BGP: meridio2v1alpha1.BgpSpec{
					RemoteASN:  4248829953,
					LocalASN:   8103,
					LocalPort:  uint16Ptr(10179),
					RemotePort: uint16Ptr(10179),
					HoldTime:   "3s",
					BFD: &meridio2v1alpha1.BfdSpec{
						Switch:     boolPtr(true),
						MinRx:      "300ms",
						MinTx:      "300ms",
						Multiplier: uint16Ptr(3),
					},
				},
			},
		}

		got, err := b.generateConfig([]string{"20.0.0.1/32"}, []*meridio2v1alpha1.GatewayRouter{router})
		if err != nil {
			t.Fatalf("generateConfig() error = %v", err)
		}

		want := buildWantConfig(t, wantConfigParams{
			BGPInterfaces: []string{"vlan-100"},
			IPv4VIPs:      []string{"20.0.0.1/32"},
			Routers: []wantRouter{{
				Name: "gatewayrouter-sample", Interface: "vlan-100",
				LocalPort: 10179, LocalASN: 8103,
				Address: "169.254.100.150", RemotePort: 10179, RemoteASN: 4248829953,
				BFD:      "bfd {\n\t\tmin rx interval 300ms;\n\t\tmin tx interval 300ms;\n\t\tmultiplier 3;\n\t};",
				HoldTime: "3", IPFamily: "ipv4",
			}},
		})

		if normalizeWhitespace(got) != normalizeWhitespace(want) {
			t.Errorf("config mismatch\nGot:\n%s\n\nWant:\n%s", got, want)
		}
	})

	t.Run("bfd interfaces from multiple routers", func(t *testing.T) {
		routerV4 := &meridio2v1alpha1.GatewayRouter{
			ObjectMeta: metav1.ObjectMeta{Name: "gw-v4"},
			Spec: meridio2v1alpha1.GatewayRouterSpec{
				Interface: "net1",
				Address:   "192.168.1.1",
				BGP:       meridio2v1alpha1.BgpSpec{RemoteASN: 65000, LocalASN: 65001},
			},
		}
		routerV6 := &meridio2v1alpha1.GatewayRouter{
			ObjectMeta: metav1.ObjectMeta{Name: "gw-v6"},
			Spec: meridio2v1alpha1.GatewayRouterSpec{
				Interface: "net2",
				Address:   "fd00::1",
				BGP:       meridio2v1alpha1.BgpSpec{RemoteASN: 65000, LocalASN: 65001},
			},
		}

		got, err := b.generateConfig([]string{}, []*meridio2v1alpha1.GatewayRouter{routerV4, routerV6})
		if err != nil {
			t.Fatalf("generateConfig() error = %v", err)
		}

		want := buildWantConfig(t, wantConfigParams{
			BGPInterfaces: []string{"net1", "net2"},
			Routers: []wantRouter{
				{
					Name: "gw-v4", Interface: "net1",
					LocalPort: 179, LocalASN: 65001,
					Address: "192.168.1.1", RemotePort: 179, RemoteASN: 65000,
					BFD: "bfd off;", HoldTime: "90", IPFamily: "ipv4",
				},
				{
					Name: "gw-v6", Interface: "net2",
					LocalPort: 179, LocalASN: 65001,
					Address: "fd00::1", RemotePort: 179, RemoteASN: 65000,
					BFD: "bfd off;", HoldTime: "90", IPFamily: "ipv6",
				},
			},
		})

		if normalizeWhitespace(got) != normalizeWhitespace(want) {
			t.Errorf("config mismatch\nGot:\n%s\n\nWant:\n%s", got, want)
		}
	})

	t.Run("bfd on without custom timers", func(t *testing.T) {
		router := &meridio2v1alpha1.GatewayRouter{
			ObjectMeta: metav1.ObjectMeta{Name: "gw-bfd-on"},
			Spec: meridio2v1alpha1.GatewayRouterSpec{
				Interface: "net1",
				Address:   "192.168.1.1",
				BGP: meridio2v1alpha1.BgpSpec{
					RemoteASN: 65000,
					LocalASN:  65001,
					BFD:       &meridio2v1alpha1.BfdSpec{Switch: boolPtr(true)},
				},
			},
		}

		got, err := b.generateConfig([]string{}, []*meridio2v1alpha1.GatewayRouter{router})
		if err != nil {
			t.Fatalf("generateConfig() error = %v", err)
		}

		want := buildWantConfig(t, wantConfigParams{
			BGPInterfaces: []string{"net1"},
			Routers: []wantRouter{{
				Name: "gw-bfd-on", Interface: "net1",
				LocalPort: 179, LocalASN: 65001,
				Address: "192.168.1.1", RemotePort: 179, RemoteASN: 65000,
				BFD: "bfd on;", HoldTime: "90", IPFamily: "ipv4",
			}},
		})

		if normalizeWhitespace(got) != normalizeWhitespace(want) {
			t.Errorf("config mismatch\nGot:\n%s\n\nWant:\n%s", got, want)
		}
	})
}

func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func uint16Ptr(i uint16) *uint16 {
	return &i
}

func boolPtr(b bool) *bool {
	return &b
}
