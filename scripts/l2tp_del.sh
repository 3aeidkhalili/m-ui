#!/usr/bin/env bash
# l2tp_del.sh <username>
set -euo pipefail
umask 077

USERNAME="$1"
CHAP="${L2TP_CHAP_SECRETS:-/etc/ppp/chap-secrets}"

if [[ -f "$CHAP" ]]; then
  # Exact first-field match (awk; no regex interpretation on the username)
  awk -v u="$USERNAME" '$1 != u' "$CHAP" > "${CHAP}.tmp" && mv "${CHAP}.tmp" "$CHAP"
  chmod 600 "$CHAP"
fi
echo '{}'
