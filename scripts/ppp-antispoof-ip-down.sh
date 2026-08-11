#!/usr/bin/env bash
# Installed in /etc/ppp/ip-down.d/ — removes the anti-spoof rule of the ended session.
IFACE="${1:-${PPP_IFACE:-}}"
REMOTE="${5:-${PPP_REMOTE:-}}"
[ -n "$IFACE" ] && [ -n "$REMOTE" ] || exit 0

iptables -t raw -D PREROUTING -i "$IFACE" ! -s "$REMOTE" -j DROP 2>/dev/null || true

# Drop the recorded public-IP mapping used by the per-user IP-limit check.
rm -f "/run/multivpn/l2tp/${REMOTE}" 2>/dev/null || true
exit 0
