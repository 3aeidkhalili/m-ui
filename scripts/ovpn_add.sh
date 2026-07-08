#!/usr/bin/env bash
# ovpn_add.sh <username> <static_ip> [password]
# Builds a client certificate (kept for the CA/tls-crypt material and CCD lookup),
# pins the static IP, and — when a password is given — records a username/password
# credential so OpenVPN can authenticate with the same user/pass as L2TP/IPsec.
# Prints the raw material (ca/cert/key/tls_crypt) as JSON. Connection parameters
# (remote/port/proto/cipher) come from the panel's "protocol settings"; the final
# config is built on the fly by the backend.
set -euo pipefail
umask 077

USERNAME="$1"
STATIC_IP="$2"
PASSWORD="${3:-}"

# Reject any username with path/traversal/control characters — it becomes a file
# path (ccd) and an auth-store key here, so this blocks traversal/injection even
# if an upstream check is bypassed.
case "$USERNAME" in
  *[!A-Za-z0-9_.-]* | '' | . | ..) echo "invalid username" >&2; exit 1 ;;
esac

EASYRSA_DIR="${EASYRSA_DIR:-/etc/openvpn/easy-rsa}"
CCD_DIR="${OVPN_CCD_DIR:-/etc/openvpn/ccd}"
TLS_CRYPT="${OVPN_TLS_CRYPT:-/etc/openvpn/tls-crypt.key}"
AUTH_DB="${OVPN_AUTH_DB:-/etc/openvpn/ovpn-auth}"
NETMASK="255.255.255.0"

cd "$EASYRSA_DIR"

if [[ ! -f "pki/issued/${USERNAME}.crt" ]]; then
  ./easyrsa --batch build-client-full "$USERNAME" nopass >/dev/null 2>&1
fi

mkdir -p "$CCD_DIR"
echo "ifconfig-push ${STATIC_IP} ${NETMASK}" > "${CCD_DIR}/${USERNAME}"
# OpenVPN reads the ccd file AFTER dropping to user "nobody"; a 600 root file is
# unreadable there and the static IP is silently ignored (client falls back to the
# dynamic pool). Make it readable by the nogroup the daemon runs as.
chown root:nogroup "${CCD_DIR}/${USERNAME}" 2>/dev/null || true
chmod 640 "${CCD_DIR}/${USERNAME}"

# username/password credential (sha256; same password as L2TP). Readable by the
# unprivileged OpenVPN process (root:nogroup 640) which runs the verify script.
if [[ -n "$PASSWORD" ]]; then
  touch "$AUTH_DB"
  HASH="$(printf '%s' "$PASSWORD" | sha256sum | cut -d' ' -f1)"
  awk -F: -v u="$USERNAME" '$1 != u' "$AUTH_DB" > "${AUTH_DB}.tmp"
  printf '%s:%s\n' "$USERNAME" "$HASH" >> "${AUTH_DB}.tmp"
  mv "${AUTH_DB}.tmp" "$AUTH_DB"
  chown root:nogroup "$AUTH_DB" 2>/dev/null || true
  chmod 640 "$AUTH_DB"
fi

CA="$(cat pki/ca.crt)"
CRT="$(openssl x509 -in "pki/issued/${USERNAME}.crt")"
KEY="$(cat "pki/private/${USERNAME}.key")"
TC="$(cat "$TLS_CRYPT")"

jq -n --arg ca "$CA" --arg cert "$CRT" --arg key "$KEY" --arg tls_crypt "$TC" \
  '{ca:$ca,cert:$cert,key:$key,tls_crypt:$tls_crypt}'
