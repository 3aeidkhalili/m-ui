#!/usr/bin/env bash
# wg_add.sh <username> <client_ip>
# Generates keys, adds the peer live and persists it, and prints the keys +
# client config as JSON.
set -euo pipefail

USERNAME="$1"
CLIENT_IP="$2"

WG_IF="${WG_INTERFACE:-wg0}"
WG_PORT="${WG_LISTEN_PORT:-51820}"
SERVER_IP="${SERVER_PUBLIC_IP:?SERVER_PUBLIC_IP not set}"
SERVER_PUB="${WG_SERVER_PUBLIC_KEY:?WG_SERVER_PUBLIC_KEY not set}"

PRIV="$(wg genkey)"
PUB="$(printf '%s' "$PRIV" | wg pubkey)"
PSK="$(wg genpsk)"

# Remove any orphan peer that currently holds this IP (re-allocating a freed username)
for oldpub in $(wg show "$WG_IF" allowed-ips 2>/dev/null | awk -v ip="${CLIENT_IP}/32" 'index($0, ip){print $1}'); do
  if [[ "$oldpub" != "$PUB" ]]; then
    wg set "$WG_IF" peer "$oldpub" remove || true
  fi
done

# Add the peer live and persist it to the config
wg set "$WG_IF" peer "$PUB" preshared-key <(printf '%s' "$PSK") allowed-ips "${CLIENT_IP}/32"
wg-quick save "$WG_IF" 2>/dev/null || true

read -r -d '' CONF <<EOF || true
[Interface]
PrivateKey = ${PRIV}
Address = ${CLIENT_IP}/32
DNS = 1.1.1.1

[Peer]
PublicKey = ${SERVER_PUB}
PresharedKey = ${PSK}
Endpoint = ${SERVER_IP}:${WG_PORT}
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25
EOF

jq -n --arg private_key "$PRIV" --arg public_key "$PUB" --arg preshared_key "$PSK" --arg config "$CONF" \
  '{private_key:$private_key,public_key:$public_key,preshared_key:$preshared_key,config:$config}'
