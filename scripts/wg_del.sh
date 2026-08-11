#!/usr/bin/env bash
# wg_del.sh <username> <public_key>
set -euo pipefail

PUB="${2:-}"
WG_IF="${WG_INTERFACE:-wg0}"

if [[ -n "$PUB" ]]; then
  wg set "$WG_IF" peer "$PUB" remove || true
  wg-quick save "$WG_IF" 2>/dev/null || true
fi
echo '{}'
