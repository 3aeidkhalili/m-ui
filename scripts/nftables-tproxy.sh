#!/usr/bin/env bash
# nftables-tproxy.sh
# Transparently redirects VPN-subnet traffic into Xray's TPROXY inbound and
# closes external access to the internal ports (TPROXY/Xray-API).
# Loop prevention: Xray's own egress traffic is marked mark=255 and skipped.
set -euo pipefail

TPROXY_PORT="${TPROXY_PORT:-12345}"
XRAY_API_PORT="$(printf '%s' "${XRAY_API_ADDR:-127.0.0.1:10085}" | awk -F: '{print $NF}')"
OVPN_SUBNET="${OVPN_SUBNET:-10.8.0.0/24}"
WG_SUBNET="${WG_SUBNET:-10.9.0.0/24}"
L2TP_SUBNET="${L2TP_SUBNET:-10.10.0.0/24}"

# Mark-based routing: packets marked mark=1 are delivered locally
ip rule del fwmark 1 lookup 100 2>/dev/null || true
ip rule add fwmark 1 lookup 100
ip route replace local 0.0.0.0/0 dev lo table 100

# Flush any previous table (idempotent)
if nft list table inet xray_tproxy >/dev/null 2>&1; then
  nft delete table inet xray_tproxy
fi

nft -f - <<EOF
table inet xray_tproxy {
  set vpn_nets {
    type ipv4_addr
    flags interval
    elements = { ${OVPN_SUBNET}, ${WG_SUBNET}, ${L2TP_SUBNET} }
  }

  chain divert {
    type filter hook prerouting priority mangle - 1; policy accept;
    meta l4proto tcp socket transparent 1 meta mark set 1 accept
  }

  chain prerouting {
    type filter hook prerouting priority mangle; policy accept;
    meta mark 255 return
    ip saddr @vpn_nets meta l4proto tcp tproxy ip to 127.0.0.1:${TPROXY_PORT} meta mark set 1 accept
    ip saddr @vpn_nets meta l4proto udp tproxy ip to 127.0.0.1:${TPROXY_PORT} meta mark set 1 accept
  }

  chain input {
    type filter hook input priority filter; policy accept;
    iif "lo" accept
    # TPROXY'd VPN traffic (mark=1) is allowed
    meta mark 1 accept
    # External access to the TPROXY port and Xray-API is blocked
    tcp dport { ${TPROXY_PORT}, ${XRAY_API_PORT} } drop
    udp dport ${TPROXY_PORT} drop
  }
}
EOF

echo "nftables TPROXY + input-guard configured (tproxy=${TPROXY_PORT}, api=${XRAY_API_PORT})"
