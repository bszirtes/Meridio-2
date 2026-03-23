#!/bin/sh

sysctl -w net.ipv4.conf.all.forwarding=1
sysctl -w net.ipv6.conf.all.forwarding=1
sysctl -w net.ipv6.conf.all.accept_dad=0
sysctl -w net.ipv6.conf.all.disable_ipv6=0

# VLAN 100 — ns-a external peering
ip link add link eth0 name vlan1 type vlan id 100
ip link set vlan1 up
ip addr add 169.254.100.150/24 dev vlan1
ip addr add fd00:100::150/64 dev vlan1

# VLAN 200 — ns-b external peering
ip link add link eth0 name vlan2 type vlan id 200
ip link set vlan2 up
ip addr add 169.254.200.150/24 dev vlan2
ip addr add fd00:200::150/64 dev vlan2

echo "VPN Gateway ready on VLAN 100 and VLAN 200"

/usr/sbin/bird -d -c /etc/bird/bird.conf
