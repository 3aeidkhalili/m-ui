#!/usr/bin/env bash
# Installed in /etc/ppp/ip-up.d/ — locks each L2TP session to its dedicated IP.
# pppd passes: $1=iface $2=tty $3=speed $4=local-ip $5=remote-ip
# Without this lock an L2TP user could spoof another user's source-IP and spend their quota.
IFACE="${1:-${PPP_IFACE:-}}"
REMOTE="${5:-${PPP_REMOTE:-}}"
[ -n "$IFACE" ] && [ -n "$REMOTE" ] || exit 0

# In raw/PREROUTING (before mangle/TPROXY) drop any packet whose source is not the dedicated IP
iptables -t raw -C PREROUTING -i "$IFACE" ! -s "$REMOTE" -j DROP 2>/dev/null \
  || iptables -t raw -A PREROUTING -i "$IFACE" ! -s "$REMOTE" -j DROP

# Record this session's public client IP for the panel's per-user IP-limit check.
# pppd's ip-up env has no outer/public IP, so we correlate via the pppd ancestor
# PID with xl2tpd's "Call established with <PUBIP>, PID: <pid>" log line, then
# write /run/multivpn/l2tp/<internalIP> = <publicIP> (removed again in ip-down).
{
  pppd_pid=""; pid=$$
  while [ -n "$pid" ] && [ "$pid" != "1" ]; do
    [ "$(cat /proc/$pid/comm 2>/dev/null)" = "pppd" ] && { pppd_pid="$pid"; break; }
    pid="$(awk '{print $4}' /proc/$pid/stat 2>/dev/null)"
  done
  pub=""
  if [ -n "$pppd_pid" ]; then
    pub="$(journalctl -u xl2tpd --since '-3 min' -o cat 2>/dev/null \
      | grep -oP "Call established with \K[0-9.]+(?=, PID: ${pppd_pid},)" | tail -1)"
  fi
  if [ -n "$pub" ]; then
    mkdir -p /run/multivpn/l2tp
    printf '%s\n' "$pub" > "/run/multivpn/l2tp/${REMOTE}"
  fi
} 2>/dev/null || true

# Apply this user's download speed cap (Mbps) on the fresh ppp interface (inline
# tc, so the hook has no dependency on the panel's scripts dir). The backend
# writes /run/multivpn/bw/<internalIP> = <mbit> for capped users.
{
  mbit="$(cat "/run/multivpn/bw/${REMOTE}" 2>/dev/null || true)"
  if [ -n "$mbit" ] && [ "$mbit" -gt 0 ] 2>/dev/null; then
    tc qdisc del dev "$IFACE" root 2>/dev/null || true
    tc qdisc add dev "$IFACE" root handle 1: htb default 999
    tc class add dev "$IFACE" parent 1: classid 1:999 htb rate 10000mbit ceil 10000mbit
    tc class add dev "$IFACE" parent 1: classid 1:100 htb rate "${mbit}mbit" ceil "${mbit}mbit" burst 32k
    tc filter add dev "$IFACE" protocol ip parent 1: prio 1 u32 match ip dst "${REMOTE}/32" flowid 1:100
  fi
} 2>/dev/null || true
exit 0
