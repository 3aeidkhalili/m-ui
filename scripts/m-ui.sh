#!/usr/bin/env bash
###############################################################################
# m-ui — MultiVPN panel management menu (installed to /usr/local/bin/m-ui)
#
# A terminal control panel (similar to x-ui) for starting/stopping services,
# checking status, viewing logs, resetting the admin password, changing the
# panel port, rebuilding, and uninstalling.
###############################################################################
set -uo pipefail

CONF="/etc/multivpn/panel.conf"

# Defaults (overridden by $CONF, which install.sh writes).
REPO_DIR="/opt/multivpn"
BACKEND_DIR="/opt/multivpn/backend"
BIN="/usr/local/bin/multivpn"
WG_IF="wg0"
API_PORT="8443"
PANEL_URL="https://127.0.0.1:8443"
ADMIN_USER="admin"
# shellcheck source=/dev/null
[[ -f "$CONF" ]] && source "$CONF"

ENV_FILE="$BACKEND_DIR/.env"

c_reset=$'\033[0m'; c_b=$'\033[1m'; c_dim=$'\033[2m'
c_red=$'\033[1;31m'; c_grn=$'\033[1;32m'; c_yel=$'\033[1;33m'; c_blu=$'\033[1;34m'; c_cyn=$'\033[1;36m'

SERVICES=(multivpn-api xray openvpn@server "wg-quick@${WG_IF}" xl2tpd strongswan multivpn-tproxy)
PROTO_SERVICES=(xray openvpn@server "wg-quick@${WG_IF}" xl2tpd strongswan)

need_root() { [[ $EUID -eq 0 ]] || { echo "${c_red}Please run as root:${c_reset} sudo m-ui"; exit 1; }; }

pause() { echo; read -rp "Press Enter to continue..." _; }

svc_state() {
  local s; s="$(systemctl is-active "$1" 2>/dev/null)"
  case "$s" in
    active)   echo "${c_grn}● active${c_reset}" ;;
    inactive) echo "${c_dim}○ inactive${c_reset}" ;;
    failed)   echo "${c_red}✗ failed${c_reset}" ;;
    *)        echo "${c_yel}? ${s:-unknown}${c_reset}" ;;
  esac
}

show_status() {
  echo "${c_b}Service status:${c_reset}"
  for s in "${SERVICES[@]}"; do
    printf "  %-26s %s\n" "$s" "$(svc_state "$s")"
  done
  echo
  echo "  ${c_b}Panel:${c_reset} ${c_cyn}${PANEL_URL}${c_reset}"
  echo "  ${c_b}Admin:${c_reset} ${ADMIN_USER}"
}

do_for_all() {  # <action>
  local action="$1"
  for s in "${SERVICES[@]}"; do
    printf "  %-26s " "$s"
    if systemctl "$action" "$s" >/dev/null 2>&1; then echo "${c_grn}ok${c_reset}"; else echo "${c_yel}skipped${c_reset}"; fi
  done
}

restart_panel() {
  echo "Restarting the panel API..."
  systemctl restart multivpn-api && echo "${c_grn}done${c_reset}" || echo "${c_red}failed${c_reset}"
}

restart_protocol_menu() {
  echo "${c_b}Which protocol to restart?${c_reset}"
  local i=1
  for s in "${PROTO_SERVICES[@]}"; do echo "  $i) $s"; i=$((i+1)); done
  echo "  0) back"
  read -rp "> " ch
  [[ "$ch" == "0" || -z "$ch" ]] && return
  local idx=$((ch-1))
  local svc="${PROTO_SERVICES[$idx]:-}"
  [[ -z "$svc" ]] && { echo "invalid"; return; }
  systemctl restart "$svc" && echo "${c_grn}restarted $svc${c_reset}" || echo "${c_red}failed${c_reset}"
}

reset_password() {
  echo "Resetting the admin password..."
  ( cd "$BACKEND_DIR" && BASE_DIR="$BACKEND_DIR" "$BIN" passwd )
  echo "${c_dim}(existing sessions were revoked)${c_reset}"
}

change_port() {
  read -rp "New panel port [current: ${API_PORT}]: " newport
  [[ -z "$newport" ]] && { echo "unchanged"; return; }
  [[ "$newport" =~ ^[0-9]+$ ]] || { echo "${c_red}not a number${c_reset}"; return; }
  sed -i "s/^API_PORT=.*/API_PORT=${newport}/" "$ENV_FILE"
  sed -i "s|^PANEL_URL=.*|PANEL_URL=$(echo "$PANEL_URL" | sed "s/:${API_PORT}/:${newport}/")|" "$CONF" 2>/dev/null || true
  if command -v ufw >/dev/null && ufw status | grep -q "Status: active"; then
    ufw allow "${newport}/tcp" >/dev/null 2>&1 || true
  fi
  systemctl restart multivpn-api
  echo "${c_grn}port changed to ${newport} and panel restarted${c_reset}"
  echo "${c_yel}note:${c_reset} if you use nginx/a domain, update the proxy_pass port too."
}

rebuild_panel() {
  if ! command -v go >/dev/null && [[ ! -x /usr/local/go/bin/go ]]; then
    echo "${c_red}Go toolchain not found; cannot rebuild here.${c_reset}"; return
  fi
  export PATH="$PATH:/usr/local/go/bin"
  echo "Rebuilding the Go binary..."
  if ( cd "$BACKEND_DIR" && go build -mod=vendor -o "$BIN" ./cmd/multivpn ); then
    systemctl restart multivpn-api
    echo "${c_grn}rebuilt and restarted${c_reset}"
  else
    echo "${c_red}build failed${c_reset}"
  fi
}

optimize_speed() {
  echo "${c_b}Outbound network optimization (BBR + real-throughput benchmark)…${c_reset}"
  read -rp "Activate the fastest relays automatically? [y/N]: " ap
  local flag=""; [[ "$ap" =~ ^[Yy]$ ]] && flag="--apply"
  BASE_DIR="$BACKEND_DIR" "$BIN" optimize $flag
}

uninstall() {
  echo "${c_red}${c_b}This will stop and remove the MultiVPN panel services.${c_reset}"
  read -rp "Type 'yes' to confirm: " ans
  [[ "$ans" == "yes" ]] || { echo "cancelled"; return; }
  for s in multivpn-api multivpn-tproxy xray; do
    systemctl disable --now "$s" >/dev/null 2>&1 || true
  done
  rm -f /etc/systemd/system/multivpn-api.service /etc/systemd/system/multivpn-tproxy.service
  rm -f /etc/systemd/system/xray.service
  rm -rf /etc/systemd/system/xray.service.d
  systemctl daemon-reload
  rm -f /usr/local/bin/m-ui /usr/local/bin/multivpn
  rm -rf /etc/multivpn
  echo "${c_grn}Panel services removed.${c_reset}"
  echo "${c_dim}VPN protocol services (openvpn/wireguard/xl2tpd/strongswan) and $REPO_DIR were left in place.${c_reset}"
  echo "${c_dim}Remove them manually if desired.${c_reset}"
  exit 0
}

menu() {
  clear
  echo "${c_blu}${c_b}┌────────────────────────────────────────────┐${c_reset}"
  echo "${c_blu}${c_b}│            MultiVPN  ·  m-ui menu           │${c_reset}"
  echo "${c_blu}${c_b}└────────────────────────────────────────────┘${c_reset}"
  echo
  show_status
  echo
  echo "  ${c_b}1)${c_reset} Start all services"
  echo "  ${c_b}2)${c_reset} Stop all services"
  echo "  ${c_b}3)${c_reset} Restart all services"
  echo "  ${c_b}4)${c_reset} Restart panel (API) only"
  echo "  ${c_b}5)${c_reset} Restart a protocol"
  echo "  ${c_b}6)${c_reset} View panel logs (live)"
  echo "  ${c_b}7)${c_reset} Reset admin password"
  echo "  ${c_b}8)${c_reset} Change panel port"
  echo "  ${c_b}9)${c_reset} Rebuild panel binary"
  echo "  ${c_b}10)${c_reset} Show login info"
  echo "  ${c_b}11)${c_reset} Optimize outbound speed (BBR + benchmark)"
  echo "  ${c_red}u)${c_reset} Uninstall panel"
  echo "  ${c_b}0)${c_reset} Exit"
  echo
  read -rp "Choose: " choice
  case "$choice" in
    1) do_for_all start; pause ;;
    2) do_for_all stop; pause ;;
    3) do_for_all restart; pause ;;
    4) restart_panel; pause ;;
    5) restart_protocol_menu; pause ;;
    6) echo "${c_dim}Ctrl+C to exit logs${c_reset}"; journalctl -u multivpn-api -f ;;
    7) reset_password; pause ;;
    8) change_port; pause ;;
    9) rebuild_panel; pause ;;
    10) echo "  Panel: ${c_cyn}${PANEL_URL}${c_reset}"; echo "  Admin: ${ADMIN_USER}"; echo "  ${c_dim}Reset the password with option 7 if you forgot it.${c_reset}"; pause ;;
    11) optimize_speed; pause ;;
    u|U) uninstall ;;
    0) exit 0 ;;
    *) ;;
  esac
}

need_root
# One-shot subcommands: `m-ui status`, `m-ui restart`, etc.
if [[ $# -gt 0 ]]; then
  case "$1" in
    status)  show_status ;;
    start)   do_for_all start ;;
    stop)    do_for_all stop ;;
    restart) do_for_all restart ;;
    logs)    journalctl -u multivpn-api -f ;;
    passwd)  reset_password ;;
    optimize) BASE_DIR="$BACKEND_DIR" "$BIN" optimize "${2:-}" ;;
    *)       echo "usage: m-ui [status|start|stop|restart|logs|passwd|optimize]  (no arg = menu)" ;;
  esac
  exit 0
fi

while true; do menu; done
