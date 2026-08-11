#!/usr/bin/env bash
# bw_apply.sh <iface> [ip:mbit ...]
# Per-user DOWNLOAD speed shaping on a VPN interface via tc HTB. Egress packets
# on <iface> go toward the client, so matching on destination IP caps a user's
# download rate. The root qdisc is rebuilt idempotently on every call.
#
# Safety: unclassified traffic (users without a cap) falls into a 10gbit default
# class, so shaping never throttles anyone who isn't explicitly listed. With no
# ip:mbit pairs the interface is returned to its default (unshaped) qdisc.
set -euo pipefail

IFACE="${1:?usage: bw_apply.sh <iface> [ip:mbit ...]}"
shift || true

[ -e "/sys/class/net/$IFACE" ] || exit 0   # interface gone -> nothing to do

# tear down any existing root qdisc (harmless if none)
tc qdisc del dev "$IFACE" root 2>/dev/null || true

# no capped users on this interface -> leave it unshaped
[ "$#" -gt 0 ] || exit 0

# HTB root; unclassified -> class 1:999 (effectively unlimited)
tc qdisc add dev "$IFACE" root handle 1: htb default 999
tc class add dev "$IFACE" parent 1: classid 1:999 htb rate 10000mbit ceil 10000mbit

i=0
for pair in "$@"; do
  ip="${pair%%:*}"
  mbit="${pair##*:}"
  case "$ip" in *[!0-9.]* | '') continue ;; esac
  case "$mbit" in *[!0-9]* | '' | 0) continue ;; esac
  i=$((i + 1))
  minor=$((100 + i))
  cid="1:${minor}"
  tc class add dev "$IFACE" parent 1: classid "$cid" htb rate "${mbit}mbit" ceil "${mbit}mbit" burst 32k 2>/dev/null || continue
  tc qdisc add dev "$IFACE" parent "$cid" handle "${minor}:" fq_codel 2>/dev/null || true
  tc filter add dev "$IFACE" protocol ip parent 1: prio 1 u32 match ip dst "${ip}/32" flowid "$cid"
done
exit 0
