#!/usr/bin/env bash
# ovpn_del.sh <username>
# Revokes the certificate, regenerates the CRL, removes the ccd file and the
# username/password credential.
set -euo pipefail

USERNAME="$1"
EASYRSA_DIR="${EASYRSA_DIR:-/etc/openvpn/easy-rsa}"
CCD_DIR="${OVPN_CCD_DIR:-/etc/openvpn/ccd}"
AUTH_DB="${OVPN_AUTH_DB:-/etc/openvpn/ovpn-auth}"

cd "$EASYRSA_DIR"
if [[ -f "pki/issued/${USERNAME}.crt" ]]; then
  ./easyrsa --batch revoke "$USERNAME" >/dev/null 2>&1 || true
  EASYRSA_CRL_DAYS=3650 ./easyrsa gen-crl >/dev/null 2>&1 || true
  if [[ -f pki/crl.pem ]]; then
    cp -f pki/crl.pem /etc/openvpn/crl.pem
    chmod 644 /etc/openvpn/crl.pem
  fi
  # Remove the PKI files (after revoke+gen-crl) so re-creating the same name gets a
  # fresh, non-revoked cert. easy-rsa 3.0.x does not delete these itself, which would
  # otherwise embed a revoked certificate.
  rm -f "pki/issued/${USERNAME}.crt" "pki/private/${USERNAME}.key" "pki/reqs/${USERNAME}.req"
fi
rm -f "${CCD_DIR}/${USERNAME}"

# Drop the username/password credential (exact first-field match).
if [[ -f "$AUTH_DB" ]]; then
  awk -F: -v u="$USERNAME" '$1 != u' "$AUTH_DB" > "${AUTH_DB}.tmp" && mv "${AUTH_DB}.tmp" "$AUTH_DB"
  chown root:nogroup "$AUTH_DB" 2>/dev/null || true
  chmod 640 "$AUTH_DB"
fi
echo '{}'
