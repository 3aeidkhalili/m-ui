#!/usr/bin/env bash
# l2tp_add.sh <username> <password> <static_ip>
# Adds a chap-secrets line with a static IP (the fourth column).
set -euo pipefail
umask 077

USERNAME="$1"
PASSWORD="$2"
STATIC_IP="$3"
CHAP="${L2TP_CHAP_SECRETS:-/etc/ppp/chap-secrets}"

# Reject usernames with path/traversal/control characters (this value is written
# into chap-secrets; a newline would inject extra credential lines).
case "$USERNAME" in
  *[!A-Za-z0-9_.-]* | '' | . | ..) echo "invalid username" >&2; exit 1 ;;
esac

touch "$CHAP"
# Remove this user's previous line by exact first-field match (awk; no regex on the username)
awk -v u="$USERNAME" '$1 != u' "$CHAP" > "${CHAP}.tmp" && mv "${CHAP}.tmp" "$CHAP"
printf '%s * %s %s\n' "$USERNAME" "$PASSWORD" "$STATIC_IP" >> "$CHAP"
chmod 600 "$CHAP"

jq -n --arg username "$USERNAME" --arg ip "$STATIC_IP" '{username:$username,ip:$ip}'
