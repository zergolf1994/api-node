#!/bin/bash

# Server API Installation Script
# Usage: curl -fsSL https://raw.githubusercontent.com/zergolf1994/api-node/main/install.sh | sudo -E bash -s -- [OPTIONS]

set -e

# ─── Colors ────────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# ─── Defaults ──────────────────────────────────────────────────────────────────
PORT="8081"
MONGODB_URI=""
DOMAINS=()           # multi-domain: --domain api.example.com api2.example.com ...
INSTALL_APP=false
INSTALL_NGINX=false
UNINSTALL=false

APP_NAME="api-node"
APP_DIR="/opt/$APP_NAME"
SERVICE_NAME="api-node"
GITHUB_REPO="zergolf1994/api-node"
RELEASES_URL="https://github.com/$GITHUB_REPO/releases/latest/download"

# ─── Helpers ───────────────────────────────────────────────────────────────────
print_status()  { echo -e "${GREEN}[INFO]${NC}    $1"; }
print_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
print_error()   { echo -e "${RED}[ERROR]${NC}   $1"; }
print_section() { echo -e "\n${BLUE}══════════════════════════════════════════${NC}"; echo -e "${BLUE}  $1${NC}"; echo -e "${BLUE}══════════════════════════════════════════${NC}"; }

# ─── Argument Parsing ──────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
    case $1 in
        --uninstall)
            UNINSTALL=true
            shift
            ;;
        --app)
            INSTALL_APP=true
            shift
            ;;
        --nginx)
            INSTALL_NGINX=true
            shift
            ;;
        -p|--port)
            PORT="$2"
            shift 2
            ;;
        --mongodb-uri)
            MONGODB_URI="$2"
            shift 2
            ;;
        -d|--domain)
            # Consume all following non-flag args as domain names
            shift
            while [[ $# -gt 0 && ! "$1" =~ ^-- && ! "$1" =~ ^-[a-zA-Z] ]]; do
                DOMAINS+=("$1")
                shift
            done
            ;;
        -h|--help)
            echo ""
            echo "  Server API Installer — zergolf1994/api-node"
            echo ""
            echo "  Usage:"
            echo "    curl -fsSL https://raw.githubusercontent.com/$GITHUB_REPO/main/install.sh | sudo -E bash -s -- [OPTIONS]"
            echo ""
            echo "  Components (default: both):"
            echo "    --app              Install/update application binary only"
            echo "    --nginx            Install/update Nginx config only"
            echo "    --uninstall        Remove everything"
            echo ""
            echo "  Configuration:"
            echo "    -p, --port PORT           HTTP port (default: 8081)"
            echo "    -d, --domain D1 D2 ...    One or more domain names for Nginx"
            echo "    --mongodb-uri URI         MongoDB connection string"
            echo ""
            echo "  Examples:"
            echo ""
            echo "    # Install with 1 domain"
            echo "    curl -fsSL ... | sudo -E bash -s -- \\"
            echo "        --port 8081 \\"
            echo "        --domain api.vdohide.com \\"
            echo "        --mongodb-uri \"mongodb+srv://user:pass@host/db\""
            echo ""
            echo "    # Install with multiple domains"
            echo "    curl -fsSL ... | sudo -E bash -s -- \\"
            echo "        --port 8081 \\"
            echo "        --domain api.vdohide.com api2.vdohide.com \\"
            echo "        --mongodb-uri \"mongodb+srv://user:pass@host/db\""
            echo ""
            echo "    # App only (skip Nginx)"
            echo "    curl -fsSL ... | sudo -E bash -s -- --app --port 8081 --mongodb-uri \"...\""
            echo ""
            echo "    # Nginx only (add domain to existing install)"
            echo "    curl -fsSL ... | sudo -E bash -s -- --nginx --domain api2.example.com"
            echo ""
            echo "    # Uninstall"
            echo "    curl -fsSL ... | sudo bash -s -- --uninstall"
            echo ""
            exit 0
            ;;
        *)
            print_error "Unknown option: $1"
            exit 1
            ;;
    esac
done

# If no component selected → install both
if [ "$INSTALL_APP" = false ] && [ "$INSTALL_NGINX" = false ]; then
    INSTALL_APP=true
    INSTALL_NGINX=true
fi

# ═══════════════════════════════════════════════════════════════════════════════
# Uninstall
# ═══════════════════════════════════════════════════════════════════════════════
if [ "$UNINSTALL" = true ]; then
    print_section "Uninstalling $APP_NAME"

    systemctl stop  $SERVICE_NAME 2>/dev/null || true
    systemctl disable $SERVICE_NAME 2>/dev/null || true

    [ -f "/etc/systemd/system/$SERVICE_NAME.service" ] && {
        rm "/etc/systemd/system/$SERVICE_NAME.service"
        systemctl daemon-reload
        print_status "Systemd service removed."
    }

    [ -d "$APP_DIR" ] && {
        rm -rf "$APP_DIR"
        print_status "Application directory removed."
    }

    [ -f "/etc/nginx/sites-available/$APP_NAME" ] && {
        rm "/etc/nginx/sites-available/$APP_NAME"
        rm -f "/etc/nginx/sites-enabled/$APP_NAME"
        command -v nginx &>/dev/null && nginx -t && systemctl reload nginx
        print_status "Nginx configuration removed."
    }

    print_status "✅ Uninstallation complete."
    exit 0
fi

# Root check
if [ "$(id -u)" -ne 0 ]; then
    print_error "This script must be run as root (use sudo)."
    exit 1
fi

# ═══════════════════════════════════════════════════════════════════════════════
# System Dependencies
# ═══════════════════════════════════════════════════════════════════════════════
print_section "System Dependencies"
print_status "Installing system dependencies (curl, chromium)..."
if command -v apt-get &>/dev/null; then
    apt-get update -qq
    apt-get install -y -qq curl chromium-browser 2>/dev/null || apt-get install -y -qq curl chromium 2>/dev/null || true
elif command -v yum &>/dev/null; then
    yum install -y curl chromium
elif command -v dnf &>/dev/null; then
    dnf install -y curl chromium
fi
print_status "Dependencies ready."

# ═══════════════════════════════════════════════════════════════════════════════
# Application Install
# ═══════════════════════════════════════════════════════════════════════════════
if [ "$INSTALL_APP" = true ]; then
    print_section "Installing Application"

    systemctl stop $SERVICE_NAME 2>/dev/null || true

    mkdir -p "$APP_DIR"

    # ── Architecture ────────────────────────────────────────────────────────────
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64)        BINARY="linux" ;;
        aarch64|arm64) BINARY="linux-arm64" ;;
        *)
            print_error "Unsupported architecture: $ARCH"
            exit 1
            ;;
    esac

    # ── Download binary ─────────────────────────────────────────────────────────
    print_status "Downloading binary ($BINARY) from latest release..."
    curl -fsSL "$RELEASES_URL/$BINARY" -o "$APP_DIR/$APP_NAME"
    chmod +x "$APP_DIR/$APP_NAME"
    print_status "Binary ready: $APP_DIR/$APP_NAME"

    # ── Write .env ──────────────────────────────────────────────────────────────
    if [ -f "$APP_DIR/.env" ] && [ -z "$MONGODB_URI" ]; then
        # Update only PORT in existing config
        print_status "Preserving existing .env — updating PORT..."
        if grep -q "^PORT=" "$APP_DIR/.env"; then
            sed -i "s/^PORT=.*/PORT=$PORT/" "$APP_DIR/.env"
        else
            echo "PORT=$PORT" >> "$APP_DIR/.env"
        fi
    else
        print_status "Writing .env..."
        cat > "$APP_DIR/.env" <<EOF
# Server API Configuration
PORT=$PORT
MONGODB_URI=$MONGODB_URI
EOF
        if [ -z "$MONGODB_URI" ]; then
            print_warning "MONGODB_URI is not set — edit $APP_DIR/.env before starting."
        fi
    fi

    # ── Systemd service ─────────────────────────────────────────────────────────
    print_status "Creating systemd service..."
    cat > /etc/systemd/system/${SERVICE_NAME}.service <<EOF
[Unit]
Description=API Node (VdoHide)
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=$APP_DIR
ExecStart=$APP_DIR/$APP_NAME
Restart=always
RestartSec=5
EnvironmentFile=$APP_DIR/.env
Environment=HOME=/root

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable $SERVICE_NAME
    systemctl start  $SERVICE_NAME

    sleep 2
    if systemctl is-active --quiet $SERVICE_NAME; then
        print_status "✅ Application running."
    else
        print_error "❌ Application failed to start. Run: journalctl -u $SERVICE_NAME -e"
        exit 1
    fi
fi

# ═══════════════════════════════════════════════════════════════════════════════
# Nginx Install / Configure
# ═══════════════════════════════════════════════════════════════════════════════
if [ "$INSTALL_NGINX" = true ]; then
    print_section "Configuring Nginx"

    if ! command -v nginx &>/dev/null; then
        print_status "Installing Nginx..."
        apt-get update -qq
        apt-get install -y nginx
        systemctl enable nginx
        systemctl start  nginx
    fi

    if [ "${#DOMAINS[@]}" -eq 0 ]; then
        print_warning "No --domain specified — skipping Nginx vhost configuration."
    else
        # Build server_name value: "api1.example.com api2.example.com ..."
        SERVER_NAMES="${DOMAINS[*]}"

        print_status "Configuring Nginx for: $SERVER_NAMES"
        cat > /etc/nginx/sites-available/$APP_NAME <<EOF
server {
    listen 80;
    server_name $SERVER_NAMES;

    # Proxy settings
    proxy_buffering          off;
    proxy_request_buffering  off;

    location / {
        proxy_pass         http://127.0.0.1:$PORT;
        proxy_http_version 1.1;
        proxy_set_header   Upgrade           \$http_upgrade;
        proxy_set_header   Connection        'upgrade';
        proxy_set_header   Host              \$host;
        proxy_set_header   X-Real-IP         \$remote_addr;
        proxy_set_header   X-Forwarded-For   \$proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto \$scheme;
        proxy_read_timeout 300s;
        proxy_send_timeout 300s;
    }
}
EOF
        ln -sf /etc/nginx/sites-available/$APP_NAME /etc/nginx/sites-enabled/

        if nginx -t; then
            systemctl reload nginx
            print_status "✅ Nginx configured for: $SERVER_NAMES"
        else
            print_error "❌ Nginx configuration test failed."
            exit 1
        fi
    fi
fi

# ═══════════════════════════════════════════════════════════════════════════════
# Done
# ═══════════════════════════════════════════════════════════════════════════════
echo ""
echo "════════════════════════════════════════════"
print_status "🎉 Installation complete!"
echo "════════════════════════════════════════════"
echo "  Service:  $SERVICE_NAME"
echo "  Port:     $PORT"
if [ "${#DOMAINS[@]}" -gt 0 ]; then
    echo "  Domains:"
    for d in "${DOMAINS[@]}"; do
        echo "    • http://$d"
    done
fi
echo ""
echo "  Health:   http://localhost:$PORT/health"
echo "  Remote:   http://localhost:$PORT/remote"
echo "  Scraper:  http://localhost:$PORT/scraper?url=<URL>"
echo "  Parsers:  http://localhost:$PORT/parsers"
echo ""
echo "  Commands:"
echo "    systemctl status  $SERVICE_NAME"
echo "    systemctl restart $SERVICE_NAME"
echo "    journalctl -u $SERVICE_NAME -f"
echo "    Uninstall: curl -fsSL https://raw.githubusercontent.com/$GITHUB_REPO/main/install.sh | sudo bash -s -- --uninstall"
echo "════════════════════════════════════════════"
