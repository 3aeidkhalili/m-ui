#!/usr/bin/env bash
# ovpn_authpw.sh <cred-file>
# OpenVPN `auth-user-pass-verify ... via-file` handler. OpenVPN writes the
# client-supplied username (line 1) and password (line 2) to <cred-file> and
# runs this script; exit 0 accepts, non-zero rejects. It runs AFTER OpenVPN
# drops to user "nobody", so it reads the shared credential store
# /etc/openvpn/ovpn-auth (root:nogroup 640, "username:sha256hex" lines) which
# holds the same password as the user's L2TP/IPsec login.
set -euo pipefail

CRED="${1:?usage: ovpn_authpw.sh <cred-file>}"
AUTH_DB="${OVPN_AUTH_DB:-/etc/openvpn/ovpn-auth}"

user="$(sed -n 1p "$CRED")"
pass="$(sed -n 2p "$CRED")"

[[ -n "$user" ]] || exit 1
# username must be a single safe token (defends the awk match / CN lookup)
case "$user" in
  *[!A-Za-z0-9_.-]* | '') exit 1 ;;
esac

expected="$(awk -F: -v u="$user" '$1 == u { print $2; exit }' "$AUTH_DB" 2>/dev/null || true)"
[[ -n "$expected" ]] || exit 1

got="$(printf '%s' "$pass" | sha256sum | cut -d' ' -f1)"
[[ "$got" == "$expected" ]] || exit 1
exit 0
