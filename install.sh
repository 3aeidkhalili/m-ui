#!/usr/bin/env bash
###############################################################################
# MultiVPN Panel — installer for Ubuntu 22.04 / 24.04
#
# Installs and configures: Xray-core + OpenVPN + WireGuard + L2TP/IPsec + TPROXY + panel.
# All traffic from the three protocols is routed through Xray and metered in a
# single unified counter.
###############################################################################
set -euo pipefail

log()  { echo -e "\n\033[1;34m==>\033[0m \033[1m$*\033[0m"; }
ok()   { echo -e "  \033[1;32m✓\033[0m $*"; }
warn() { echo -e "  \033[1;33m!\033[0m $*"; }
die()  { echo -e "\033[1;31m✗ $*\033[0m" >&2; exit 1; }

[[ $EUID -eq 0 ]] || die "This script must be run as root (sudo bash install.sh)"

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKEND_DIR="$REPO_DIR/backend"
FRONTEND_DIR="$REPO_DIR/frontend"
SCRIPTS_DIR="$REPO_DIR/scripts"
ENV_FILE="$BACKEND_DIR/.env"

OVPN_SUBNET="10.8.0.0/24";  OVPN_NET="10.8.0.0"
WG_SUBNET="10.9.0.0/24";    WG_SRV_IP="10.9.0.1"
L2TP_SUBNET="10.10.0.0/24"; L2TP_SRV_IP="10.10.0.1"
WG_IF="wg0"; WG_PORT="51820"; OVPN_PORT="${OVPN_PORT:-2096}"; TPROXY_PORT="12345"
XRAY_API_PORT="10085"
SSL_DIR="/etc/multivpn"

# GitHub proxy to bypass censorship (e.g. GITHUB_PROXY=https://ghproxy.com/)
GHPROXY="${GITHUB_PROXY:-}"

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "64" ;;
    aarch64|arm64) echo "arm64-v8a" ;;
    armv7l) echo "arm32-v7a" ;;
    *) echo "64" ;;
  esac
}

write_xray_service() {
  cat > /etc/systemd/system/xray.service <<'UNIT'
[Unit]
Description=Xray Service (MultiVPN)
After=network.target nss-lookup.target
[Service]
User=root
ExecStart=/usr/local/bin/xray run -config /usr/local/etc/xray/config.json
Restart=on-failure
RestartSec=3
LimitNOFILE=1048576
[Install]
WantedBy=multi-user.target
UNIT
  systemctl daemon-reload
}

install_xray_from_zip() {  # <zip>
  local zip="$1" tmp
  tmp="$(mktemp -d)"
  unzip -o "$zip" -d "$tmp" >/dev/null
  install -m 755 "$tmp/xray" /usr/local/bin/xray
  mkdir -p /usr/local/share/xray /usr/local/etc/xray
  [[ -f "$tmp/geoip.dat" ]]   && cp -f "$tmp/geoip.dat"   /usr/local/share/xray/ || true
  [[ -f "$tmp/geosite.dat" ]] && cp -f "$tmp/geosite.dat" /usr/local/share/xray/ || true
  rm -rf "$tmp"
  write_xray_service
}

install_xray() {
  if [[ -x /usr/local/bin/xray ]]; then
    ok "xray already installed"
    # Ensure the systemd unit exists even when the binary was pre-placed — else
    # the panel sees xray as 'inactive' and cannot (re)start it.
    [[ -f /etc/systemd/system/xray.service ]] || { write_xray_service; ok "xray.service unit created"; }
    return
  fi
  local arch bundle url tmpzip
  arch="$(detect_arch)"
  bundle="$REPO_DIR/vendor/xray/Xray-linux-${arch}.zip"
  if [[ -f "$bundle" ]]; then
    log "Installing Xray from the bundled local package (offline) — arch=${arch}"
    install_xray_from_zip "$bundle"
  elif [[ -n "$GHPROXY" ]]; then
    log "Installing Xray via mirror: ${GHPROXY}"
    url="${GHPROXY}https://github.com/XTLS/Xray-core/releases/latest/download/Xray-linux-${arch}.zip"
    tmpzip="$(mktemp).zip"
    if curl -fsSL "$url" -o "$tmpzip"; then install_xray_from_zip "$tmpzip"; else warn "Xray download from mirror failed"; fi
    rm -f "$tmpzip"
  else
    log "Installing Xray from GitHub (official)"
    bash -c "$(curl -fsSL "${GHPROXY}https://github.com/XTLS/Xray-install/raw/main/install-release.sh")" @ install >/dev/null \
      || warn "Xray installation failed — is GitHub unreachable? Set GITHUB_PROXY or place vendor/xray/."
  fi
}

GO_VERSION="1.23.4"
go_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    armv7l|armv6l) echo "armv6l" ;;
    *) echo "amd64" ;;
  esac
}

# Minimum Go the build needs — must match the `go` directive in backend/go.mod,
# otherwise `go build` (with GOTOOLCHAIN=local) fails on an older pre-installed Go.
GO_MIN_MINOR=23

# Resolve a usable `go` binary into $GO. Priority: existing go (>=1.$GO_MIN_MINOR)
# -> bundled local tarball (vendor/go/, offline) -> mirror/official download.
ensure_go() {
  GO=""
  if command -v go >/dev/null; then
    local maj min
    maj="$(go env GOVERSION 2>/dev/null | sed -E 's/go([0-9]+)\.([0-9]+).*/\1/')"
    min="$(go env GOVERSION 2>/dev/null | sed -E 's/go([0-9]+)\.([0-9]+).*/\2/')"
    if [[ "${maj:-0}" -gt 1 || ( "${maj:-0}" -eq 1 && "${min:-0}" -ge $GO_MIN_MINOR ) ]]; then
      GO="$(command -v go)"; ok "using existing $(go version)"; return
    fi
    warn "existing $(go version) is older than go1.${GO_MIN_MINOR}; installing a newer Go"
  fi
  [[ -x /usr/local/go/bin/go ]] && { GO=/usr/local/go/bin/go; ok "using $(/usr/local/go/bin/go version)"; return; }

  local arch tgz; arch="$(go_arch)"
  local bundle="$REPO_DIR/vendor/go/go${GO_VERSION}.linux-${arch}.tar.gz"
  if [[ -f "$bundle" ]]; then
    log "Installing Go from the bundled local package (offline)"
    rm -rf /usr/local/go && tar -C /usr/local -xzf "$bundle" && GO=/usr/local/go/bin/go
  else
    log "Installing Go ${GO_VERSION} (${arch})"
    tgz="$(mktemp).tgz"
    local ok_dl=0
    for base in "${GHPROXY}https://dl.google.com/go" "https://mirrors.aliyun.com/golang" "https://golang.google.cn/dl" "https://go.dev/dl"; do
      if curl -fL --retry 3 --connect-timeout 15 --max-time 600 -s "${base}/go${GO_VERSION}.linux-${arch}.tar.gz" -o "$tgz" \
         && [[ "$(stat -c%s "$tgz" 2>/dev/null || echo 0)" -gt 40000000 ]]; then
        rm -rf /usr/local/go && tar -C /usr/local -xzf "$tgz" && GO=/usr/local/go/bin/go && ok_dl=1 && break
      fi
    done
    rm -f "$tgz"
    [[ "$ok_dl" -eq 1 ]] || die "Go download failed. Set GITHUB_PROXY or place a tarball at vendor/go/go${GO_VERSION}.linux-${arch}.tar.gz"
  fi
  [[ -n "$GO" ]] && ok "$("$GO" version)"
}

# ---------------------------------------------------------------------------
# Interactive input
# ---------------------------------------------------------------------------
log "Initial configuration"
DEFAULT_IP="$(curl -fsS4 https://api.ipify.org 2>/dev/null || hostname -I | awk '{print $1}')"
read -rp "Server public IP [$DEFAULT_IP]: " SERVER_IP; SERVER_IP="${SERVER_IP:-$DEFAULT_IP}"
read -rp "Panel domain (empty = self-signed HTTPS on IP): " PANEL_DOMAIN
read -rp "Admin username [admin]: " ADMIN_USER; ADMIN_USER="${ADMIN_USER:-admin}"
read -rsp "Admin password (empty = auto-generate): " ADMIN_PASS; echo
read -rp "Panel port [8443]: " API_PORT; API_PORT="${API_PORT:-8443}"

if [[ -n "$PANEL_DOMAIN" ]]; then
  API_HOST="127.0.0.1"; TRUSTED_PROXY="true"   # behind nginx -> X-Forwarded-For is trusted
else
  API_HOST="0.0.0.0"; TRUSTED_PROXY="false"    # direct -> XFF not trusted
fi

ADMIN_PASS_GENERATED=0
if [[ -z "$ADMIN_PASS" ]]; then
  ADMIN_PASS="$(openssl rand -base64 18 | tr -dc 'A-Za-z0-9' | cut -c1-18)"
  ADMIN_PASS_GENERATED=1
fi

# ---------------------------------------------------------------------------
# 1) Packages
# ---------------------------------------------------------------------------
log "Installing system packages"
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y \
  curl unzip jq openssl ca-certificates iproute2 iptables \
  nftables \
  openvpn easy-rsa \
  wireguard wireguard-tools \
  strongswan strongswan-starter xl2tpd ppp \
  tar
ok "Packages installed"

# Node is only needed if a prebuilt frontend (dist) is not present
if [[ ! -d "$FRONTEND_DIR/dist" ]]; then
  log "Installing Node.js 20 (to build the frontend)"
  NODE_MAJOR="$(node -v 2>/dev/null | sed 's/^v//; s/\..*//')"
  if [[ -z "$NODE_MAJOR" ]] || [[ "$NODE_MAJOR" -lt 18 ]]; then
    curl -fsSL https://deb.nodesource.com/setup_20.x | bash - >/dev/null 2>&1 || warn "nodesource failed (censorship?) — prebuild dist beforehand"
    apt-get install -y nodejs || warn "nodejs installation failed"
  fi
  command -v node >/dev/null && ok "node $(node -v)"
fi

log "Installing Xray-core"
install_xray
[[ -x /usr/local/bin/xray ]] && ok "xray: $(/usr/local/bin/xray version 2>/dev/null | head -1)" || warn "xray was not installed"

# ---------------------------------------------------------------------------
# 2) Generate keys/secrets
# ---------------------------------------------------------------------------
log "Generating keys and secrets"
SECRET_KEY="$(openssl rand -hex 32)"
L2TP_PSK="$(openssl rand -hex 16)"
WG_SRV_PRIV="$(wg genkey)"
WG_SRV_PUB="$(printf '%s' "$WG_SRV_PRIV" | wg pubkey)"
ok "WireGuard keys and PSK generated"

# ---------------------------------------------------------------------------
# 3) OpenVPN (PKI via easy-rsa, EC algorithm)
# ---------------------------------------------------------------------------
log "Configuring OpenVPN"
EASYRSA_DIR="/etc/openvpn/easy-rsa"
if [[ ! -f "$EASYRSA_DIR/pki/issued/server.crt" ]]; then
  rm -rf "$EASYRSA_DIR"; make-cadir "$EASYRSA_DIR"
  pushd "$EASYRSA_DIR" >/dev/null
    export EASYRSA_BATCH=1 EASYRSA_ALGO=ec EASYRSA_CURVE=prime256v1
    ./easyrsa init-pki >/dev/null
    ./easyrsa build-ca nopass >/dev/null
    ./easyrsa build-server-full server nopass >/dev/null
    EASYRSA_CRL_DAYS=3650 ./easyrsa gen-crl >/dev/null
  popd >/dev/null
fi
cp -f "$EASYRSA_DIR/pki/crl.pem" /etc/openvpn/crl.pem; chmod 644 /etc/openvpn/crl.pem
[[ -f /etc/openvpn/tls-crypt.key ]] || openvpn --genkey secret /etc/openvpn/tls-crypt.key
mkdir -p /etc/openvpn/ccd

# Username/password auth (same credential as L2TP/IPsec): OpenVPN authenticates
# via this verify script instead of per-user client certs. The script runs as the
# unprivileged "nobody" user, so the credential store is root:nogroup 640.
install -m 755 "$SCRIPTS_DIR/ovpn_authpw.sh" /etc/openvpn/auth-user.sh
touch /etc/openvpn/ovpn-auth
chown root:nogroup /etc/openvpn/ovpn-auth
chmod 640 /etc/openvpn/ovpn-auth

cat > /etc/openvpn/server.conf <<EOF
port $OVPN_PORT
proto udp
dev tun
ca $EASYRSA_DIR/pki/ca.crt
cert $EASYRSA_DIR/pki/issued/server.crt
key $EASYRSA_DIR/pki/private/server.key
dh none
tls-crypt /etc/openvpn/tls-crypt.key
topology subnet
server $OVPN_NET 255.255.255.0
client-config-dir /etc/openvpn/ccd
push "redirect-gateway def1 bypass-dhcp"
push "dhcp-option DNS 1.1.1.1"
keepalive 10 120
data-ciphers AES-256-GCM:AES-128-GCM
auth SHA256
# Password-based auth: no per-user client certificate; the username becomes the
# CN used for the client-config-dir static-IP lookup.
verify-client-cert none
username-as-common-name
script-security 2
auth-user-pass-verify /etc/openvpn/auth-user.sh via-file
user nobody
group nogroup
persist-key
persist-tun
status /var/log/openvpn-status.log
verb 3
explicit-exit-notify 1
EOF
ok "server.conf written"

# ---------------------------------------------------------------------------
# 4) WireGuard
# ---------------------------------------------------------------------------
log "Configuring WireGuard"
mkdir -p /etc/wireguard
if [[ ! -f /etc/wireguard/$WG_IF.conf ]]; then
cat > /etc/wireguard/$WG_IF.conf <<EOF
[Interface]
Address = $WG_SRV_IP/24
ListenPort = $WG_PORT
PrivateKey = $WG_SRV_PRIV
# NAT not needed: egress is handled by Xray
EOF
  chmod 600 /etc/wireguard/$WG_IF.conf
fi
ok "$WG_IF.conf written"

# ---------------------------------------------------------------------------
# 5) L2TP/IPsec (strongSwan + xl2tpd) — strong crypto
# ---------------------------------------------------------------------------
log "Configuring L2TP/IPsec"
cat > /etc/ipsec.conf <<EOF
config setup
  uniqueids=no

conn L2TP-PSK
  auto=add
  keyexchange=ikev1
  authby=secret
  type=transport
  leftprotoport=17/1701
  # Clients behind NAT have their L2TP source port rewritten away from 1701,
  # so pin only the server side and accept any client port (fixes the
  # "no matching CHILD_SA config" QUICK_MODE failure for NATed clients).
  rightprotoport=17/%any
  left=%any
  right=%any
  rekey=no
  ike=aes256-sha256-modp2048!
  esp=aes256-sha256!
EOF
echo ": PSK \"$L2TP_PSK\"" > /etc/ipsec.secrets
chmod 600 /etc/ipsec.secrets

# Dynamic range outside the users' static-IP range (which starts at index=11)
cat > /etc/xl2tpd/xl2tpd.conf <<EOF
[global]
port = 1701

[lns default]
ip range = 10.10.0.2-10.10.0.9
local ip = $L2TP_SRV_IP
require chap = yes
refuse pap = yes
require authentication = yes
name = l2tpd
pppoptfile = /etc/ppp/options.xl2tpd
length bit = yes
EOF

# NOTE: no serial/modem-era options (crtscts/modem/lock/asyncmap) — pppd aborts
# on them under the pppol2tp plugin (no real tty), which kills every L2TP call
# right after IPsec comes up.
cat > /etc/ppp/options.xl2tpd <<EOF
require-mschap-v2
ms-dns 1.1.1.1
auth
hide-password
proxyarp
lcp-echo-interval 30
lcp-echo-failure 4
mtu 1400
mru 1400
EOF
touch /etc/ppp/chap-secrets; chmod 600 /etc/ppp/chap-secrets

# Anti source-IP-spoofing scripts for each L2TP session (dedicated-IP lock).
# These also record the client's public IP (per-user IP-limit) and apply the
# per-user download speed cap (bandwidth) on the fresh ppp interface.
install -m 755 "$SCRIPTS_DIR/ppp-antispoof-ip-up.sh"   /etc/ppp/ip-up.d/50-multivpn-antispoof
install -m 755 "$SCRIPTS_DIR/ppp-antispoof-ip-down.sh" /etc/ppp/ip-down.d/50-multivpn-antispoof

# Runtime dirs used by the panel: L2TP public-IP capture and per-user bandwidth
# caps. /run is tmpfs, so a tmpfiles rule recreates them on every boot.
cat > /etc/tmpfiles.d/multivpn.conf <<'EOF'
d /run/multivpn 0755 root root -
d /run/multivpn/l2tp 0755 root root -
d /run/multivpn/bw 0755 root root -
EOF
systemd-tmpfiles --create /etc/tmpfiles.d/multivpn.conf >/dev/null 2>&1 || mkdir -p /run/multivpn/l2tp /run/multivpn/bw
ok "L2TP configured (PSK in the panel / /etc/ipsec.secrets)"

# ---------------------------------------------------------------------------
# 6) sysctl + Xray bootstrap
# ---------------------------------------------------------------------------
log "Kernel settings (forwarding / rp_filter=loose / BBR speed tuning)"
modprobe tcp_bbr 2>/dev/null || true
cat > /etc/sysctl.d/99-multivpn.conf <<EOF
net.ipv4.ip_forward=1
net.ipv4.conf.all.rp_filter=2
net.ipv4.conf.default.rp_filter=2
# Network speed tuning for fast long-haul VPN egress (BBR + FQ + large buffers).
# Re-runnable any time via:  multivpn optimize
net.core.default_qdisc=fq
net.ipv4.tcp_congestion_control=bbr
net.ipv4.tcp_fastopen=3
net.ipv4.tcp_mtu_probing=1
net.ipv4.tcp_slow_start_after_idle=0
net.core.rmem_max=16777216
net.core.wmem_max=16777216
net.ipv4.tcp_rmem=4096 87380 16777216
net.ipv4.tcp_wmem=4096 65536 16777216
net.core.netdev_max_backlog=16384
net.ipv4.tcp_notsent_lowat=16384
EOF
sysctl --system >/dev/null
ok "sysctl applied (congestion=$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null))"

log "Initial Xray config (the backend rewrites it at runtime)"
mkdir -p /usr/local/etc/xray
cat > /usr/local/etc/xray/config.json <<EOF
{
  "log": { "loglevel": "warning" },
  "api": { "tag": "api", "services": ["HandlerService","StatsService","LoggerService"] },
  "stats": {},
  "policy": { "system": { "statsOutboundUplink": true, "statsOutboundDownlink": true } },
  "inbounds": [
    { "tag": "api", "listen": "127.0.0.1", "port": $XRAY_API_PORT,
      "protocol": "dokodemo-door", "settings": { "address": "127.0.0.1" } },
    { "tag": "tproxy-in", "listen": "0.0.0.0", "port": $TPROXY_PORT,
      "protocol": "dokodemo-door",
      "settings": { "network": "tcp,udp", "followRedirect": true },
      "streamSettings": { "sockopt": { "tproxy": "tproxy" } },
      "sniffing": { "enabled": true, "destOverride": ["http","tls","quic"] } }
  ],
  "outbounds": [
    { "tag": "direct", "protocol": "freedom", "settings": {"domainStrategy":"UseIP"},
      "streamSettings": { "sockopt": { "mark": 255 } } },
    { "tag": "block", "protocol": "blackhole", "settings": {} }
  ],
  "routing": { "rules": [
    { "type": "field", "inboundTag": ["api"], "outboundTag": "api" },
    { "type": "field", "inboundTag": ["tproxy-in"], "outboundTag": "block" }
  ] }
}
EOF

mkdir -p /etc/systemd/system/xray.service.d
cat > /etc/systemd/system/xray.service.d/override.conf <<EOF
[Service]
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_BIND_SERVICE
EOF
ok "config.json written"

# ---------------------------------------------------------------------------
# 7) Self-signed TLS certificate (no-domain path)
# ---------------------------------------------------------------------------
TLS_CERT=""; TLS_KEY=""
if [[ -z "$PANEL_DOMAIN" ]]; then
  log "Generating self-signed TLS certificate for the panel"
  mkdir -p "$SSL_DIR"
  if [[ ! -f "$SSL_DIR/panel.crt" ]]; then
    openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
      -keyout "$SSL_DIR/panel.key" -out "$SSL_DIR/panel.crt" \
      -subj "/CN=$SERVER_IP" -addext "subjectAltName=IP:$SERVER_IP" >/dev/null 2>&1
    chmod 600 "$SSL_DIR/panel.key"
  fi
  TLS_CERT="$SSL_DIR/panel.crt"; TLS_KEY="$SSL_DIR/panel.key"
  ok "Self-signed certificate created (a browser warning is expected)"
fi

# ---------------------------------------------------------------------------
# 8) Backend .env file
# ---------------------------------------------------------------------------
log "Writing backend/.env"
cat > "$ENV_FILE" <<EOF
PANEL_DOMAIN=${PANEL_DOMAIN:-$SERVER_IP}
SERVER_PUBLIC_IP=$SERVER_IP
API_HOST=$API_HOST
API_PORT=$API_PORT
SECRET_KEY=$SECRET_KEY
ACCESS_TOKEN_EXPIRE_MINUTES=480
CORS_ORIGINS=
TRUSTED_PROXY=$TRUSTED_PROXY
ADMIN_USERNAME=$ADMIN_USER
ADMIN_PASSWORD=$ADMIN_PASS
DATABASE_URL=sqlite:///./multivpn.db
XRAY_BIN=/usr/local/bin/xray
XRAY_CONFIG=/usr/local/etc/xray/config.json
XRAY_API_ADDR=127.0.0.1:$XRAY_API_PORT
TPROXY_PORT=$TPROXY_PORT
TRAFFIC_JOB_INTERVAL=30
OVPN_SUBNET=$OVPN_SUBNET
WG_SUBNET=$WG_SUBNET
L2TP_SUBNET=$L2TP_SUBNET
USER_INDEX_START=11
WG_INTERFACE=$WG_IF
WG_LISTEN_PORT=$WG_PORT
WG_SERVER_PRIVATE_KEY=$WG_SRV_PRIV
WG_SERVER_PUBLIC_KEY=$WG_SRV_PUB
OVPN_PORT=$OVPN_PORT
OVPN_PROTO=udp
EASYRSA_DIR=$EASYRSA_DIR
OVPN_CCD_DIR=/etc/openvpn/ccd
OVPN_TLS_CRYPT=/etc/openvpn/tls-crypt.key
L2TP_PSK=$L2TP_PSK
L2TP_CHAP_SECRETS=/etc/ppp/chap-secrets
SCRIPTS_DIR=$SCRIPTS_DIR
PROVISIONING_ENABLED=true
EOF
chmod 600 "$ENV_FILE"
chmod +x "$SCRIPTS_DIR"/*.sh
ok ".env written"

# ---------------------------------------------------------------------------
# 9) Backend (build the Go binary + seed) — DB with restricted permissions
# ---------------------------------------------------------------------------
log "Building the Go backend"
ensure_go
PANEL_BIN="/usr/local/bin/multivpn"
# Prefer the vendored deps (offline); fall back to the module proxy if needed.
GOFLAGS_MODE="-mod=vendor"
[[ -d "$BACKEND_DIR/vendor" ]] || GOFLAGS_MODE="-mod=mod"
( cd "$BACKEND_DIR" && GOFLAGS="$GOFLAGS_MODE" GOTOOLCHAIN=local "$GO" build -o "$PANEL_BIN" ./cmd/multivpn ) \
  || die "Go build failed. Ensure vendor/ is present (offline) or the network can reach the Go module proxy."
ok "panel binary: $("$PANEL_BIN" version 2>/dev/null || echo built)"

# Offline GeoIP database (DB-IP City Lite) for the subscription page's connection map.
# Downloaded once at install time; there is no external request at runtime.
GEOIP_DIR="$BACKEND_DIR/assets/geoip"
if [[ ! -f "$GEOIP_DIR/dbip-city.mmdb" ]]; then
  mkdir -p "$GEOIP_DIR"
  GEO_MONTH="$(date +%Y-%m)"
  if curl -fsSL --max-time 180 "https://download.db-ip.com/free/dbip-city-lite-${GEO_MONTH}.mmdb.gz" -o "$GEOIP_DIR/dbip-city.mmdb.gz" 2>/dev/null; then
    gunzip -f "$GEOIP_DIR/dbip-city.mmdb.gz" && ok "Offline GeoIP database installed"
  else
    warn "GeoIP download failed; the connection map will not work without it (the rest of the panel is fine)."
  fi
fi

# Create the initial admin (prints a generated password once).
( cd "$BACKEND_DIR" && umask 077 && BASE_DIR="$BACKEND_DIR" "$PANEL_BIN" seed )
# Hardening: the DB holds client keys -> root-only
[[ -f "$BACKEND_DIR/multivpn.db" ]] && chmod 600 "$BACKEND_DIR/multivpn.db"
# Remove the admin password from .env after seeding (its hash is in the DB)
sed -i 's/^ADMIN_PASSWORD=.*/ADMIN_PASSWORD=/' "$ENV_FILE"
ok "Backend and admin ready"

# ---------------------------------------------------------------------------
# 10) Frontend
# ---------------------------------------------------------------------------
log "Preparing the frontend"
if [[ -d "$FRONTEND_DIR/dist" ]]; then
  # Prebuilt (offline mode) — built on an internet-connected machine and copied here
  rm -rf "$BACKEND_DIR/static"; cp -r "$FRONTEND_DIR/dist" "$BACKEND_DIR/static"
  ok "Using the prebuilt frontend (dist)"
elif command -v npm >/dev/null; then
  ( cd "$FRONTEND_DIR" && npm install --no-audit --no-fund >/dev/null 2>&1 && npm run build >/dev/null 2>&1 ) \
    && { rm -rf "$BACKEND_DIR/static"; cp -r "$FRONTEND_DIR/dist" "$BACKEND_DIR/static"; ok "Frontend built"; } \
    || warn "Frontend build failed; the API works. Run 'npm run build' on an internet-connected machine and copy dist."
else
  warn "npm is missing and no prebuilt dist exists; build the frontend elsewhere and copy the frontend/dist folder."
fi

# ---------------------------------------------------------------------------
# 11) systemd services
# ---------------------------------------------------------------------------
log "Creating the TPROXY service"
cat > /etc/systemd/system/multivpn-tproxy.service <<EOF
[Unit]
Description=MultiVPN nftables TPROXY rules
After=network-online.target xray.service
Wants=network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
EnvironmentFile=$ENV_FILE
ExecStart=/usr/bin/env bash $SCRIPTS_DIR/nftables-tproxy.sh

[Install]
WantedBy=multi-user.target
EOF

log "Creating the API service"
cat > /etc/systemd/system/multivpn-api.service <<EOF
[Unit]
Description=MultiVPN Panel API
After=network.target xray.service multivpn-tproxy.service

[Service]
Type=simple
User=root
UMask=0077
WorkingDirectory=$BACKEND_DIR
EnvironmentFile=$ENV_FILE
Environment=BASE_DIR=$BACKEND_DIR
Environment=PANEL_TLS_CERT=$TLS_CERT
Environment=PANEL_TLS_KEY=$TLS_KEY
ExecStart=/usr/local/bin/multivpn
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF

# ---------------------------------------------------------------------------
# 12) Enable services
# ---------------------------------------------------------------------------
log "Enabling and starting services"
systemctl daemon-reload
systemctl enable --now nftables >/dev/null 2>&1 || true
systemctl enable --now xray >/dev/null 2>&1 || warn "check xray"
systemctl enable --now openvpn@server >/dev/null 2>&1 || warn "check openvpn@server"
systemctl enable --now wg-quick@$WG_IF >/dev/null 2>&1 || warn "check wg-quick"
# strongswan/xl2tpd are auto-started by apt with default configs BEFORE we
# overwrite ipsec.conf/xl2tpd.conf, and `enable --now` won't reload a service
# that is already running. Enable + explicit restart so the new config is read.
if systemctl list-unit-files | grep -q '^strongswan-starter\.service'; then
  systemctl enable strongswan-starter >/dev/null 2>&1 || true
  systemctl restart strongswan-starter >/dev/null 2>&1 || warn "check strongswan"
else
  systemctl enable strongswan >/dev/null 2>&1 || true
  systemctl restart strongswan >/dev/null 2>&1 || warn "check strongswan"
fi
systemctl enable xl2tpd >/dev/null 2>&1 || true
systemctl restart xl2tpd >/dev/null 2>&1 || warn "check xl2tpd"
systemctl enable --now multivpn-tproxy >/dev/null 2>&1 || warn "check tproxy"
systemctl enable --now multivpn-api >/dev/null 2>&1 || warn "check api"
ok "Services enabled"

# ---------------------------------------------------------------------------
# 13) Firewall (only if ufw is active — no risk of cutting SSH)
#     Internal ports 12345/10085 are closed by nftables (input chain).
# ---------------------------------------------------------------------------
if command -v ufw >/dev/null && ufw status | grep -q "Status: active"; then
  log "Adding ufw rules"
  ufw allow OpenSSH               >/dev/null 2>&1 || true
  ufw allow $OVPN_PORT/udp        >/dev/null
  ufw allow $WG_PORT/udp          >/dev/null
  ufw allow 500,4500/udp          >/dev/null
  ufw allow 1701/udp              >/dev/null
  # Allow TPROXY traffic only from the VPN subnets (not the internet) so it works with default-deny
  for net in "$OVPN_SUBNET" "$WG_SUBNET" "$L2TP_SUBNET"; do
    ufw allow from "$net" to any port $TPROXY_PORT >/dev/null 2>&1 || true
  done
  if [[ -n "$PANEL_DOMAIN" ]]; then
    ufw allow 80/tcp >/dev/null; ufw allow 443/tcp >/dev/null
  else
    ufw allow "$API_PORT"/tcp >/dev/null
  fi
  ok "ufw rules added"
else
  warn "ufw is not active; internal ports are protected by nftables. Enable ufw for extra defense."
fi

# ---------------------------------------------------------------------------
# 14) nginx + real HTTPS (if a domain is provided)
# ---------------------------------------------------------------------------
if [[ -n "$PANEL_DOMAIN" ]]; then
  log "Installing nginx + Let's Encrypt for $PANEL_DOMAIN"
  apt-get install -y nginx certbot python3-certbot-nginx >/dev/null
  cat > /etc/nginx/sites-available/multivpn <<EOF
server {
    listen 80;
    server_name $PANEL_DOMAIN;
    location / {
        proxy_pass http://127.0.0.1:$API_PORT;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
EOF
  ln -sf /etc/nginx/sites-available/multivpn /etc/nginx/sites-enabled/multivpn
  rm -f /etc/nginx/sites-enabled/default
  { nginx -t && systemctl reload nginx; } || warn "check the nginx configuration"
  certbot --nginx -d "$PANEL_DOMAIN" --non-interactive --agree-tos -m "admin@$PANEL_DOMAIN" --redirect \
    && ok "HTTPS enabled" || warn "certbot failed — check the domain DNS and re-run"
fi

# ---------------------------------------------------------------------------
# 15) m-ui management menu + panel.conf
# ---------------------------------------------------------------------------
log "Installing the m-ui management command"
install -m 755 "$SCRIPTS_DIR/m-ui.sh" /usr/local/bin/m-ui
if [[ -n "$PANEL_DOMAIN" ]]; then PANEL_URL="https://$PANEL_DOMAIN"; else PANEL_URL="https://$SERVER_IP:$API_PORT"; fi
mkdir -p /etc/multivpn
cat > /etc/multivpn/panel.conf <<EOF
REPO_DIR=$REPO_DIR
BACKEND_DIR=$BACKEND_DIR
BIN=/usr/local/bin/multivpn
WG_IF=$WG_IF
API_PORT=$API_PORT
PANEL_URL=$PANEL_URL
ADMIN_USER=$ADMIN_USER
EOF
chmod 600 /etc/multivpn/panel.conf
ok "m-ui installed — run 'm-ui' to manage the panel"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
log "Installation complete ✅"
echo "────────────────────────────────────────────────────────"
if [[ -n "$PANEL_DOMAIN" ]]; then
  echo "  Panel:    https://$PANEL_DOMAIN"
else
  echo "  Panel:    https://$SERVER_IP:$API_PORT  (self-signed cert — accept the browser warning)"
fi
echo "  Admin:    $ADMIN_USER"
if [[ "$ADMIN_PASS_GENERATED" -eq 1 ]]; then
  echo "  Password: $ADMIN_PASS   <- save it now (shown only once)"
else
  echo "  Password: (the one you entered)"
fi
echo ""
echo "  Protocols: OpenVPN(udp/$OVPN_PORT) · WireGuard(udp/$WG_PORT) · L2TP/IPsec"
echo "  L2TP PSK:  in the panel -> each user's \"Config\" section (or /etc/ipsec.secrets)"
echo ""
echo "  Manage:    m-ui            <- terminal menu (start/stop/logs/password/uninstall)"
echo ""
echo "  Security tip: after logging in, change the password via the panel; for a real domain, re-run the installer with a domain."
echo "  Check status: systemctl status multivpn-api xray   (or: m-ui status)"
echo "  API logs:     journalctl -u multivpn-api -f         (or: m-ui logs)"
echo "────────────────────────────────────────────────────────"
