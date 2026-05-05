#!/bin/bash
# ============================================
# SSL 证书管理脚本
# ============================================
# 功能：
#   - 生成自签名证书（IP 地址部署）
#   - 申请 Let's Encrypt 证书（域名部署）
#   - 续期证书
# ============================================

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# 证书目录
SSL_DIR="./certs/ssl"
LETSENCRYPT_DIR="./certs/letsencrypt"
CERTBOT_DIR="./certs/certbot"

# 显示帮助
show_help() {
    echo "SSL 证书管理脚本"
    echo ""
    echo "用法: $0 <命令> [选项]"
    echo ""
    echo "命令:"
    echo "  self-signed <IP>     生成自签名证书（用于 IP 地址部署）"
    echo "  letsencrypt <域名>   申请 Let's Encrypt 证书（需要域名）"
    echo "  renew                续期 Let's Encrypt 证书"
    echo "  status               查看证书状态"
    echo "  help                 显示帮助信息"
    echo ""
    echo "示例:"
    echo "  $0 self-signed 192.168.1.100"
    echo "  $0 letsencrypt example.com admin@example.com"
    echo "  $0 renew"
    echo ""
}

# 生成自签名证书
generate_self_signed() {
    local SERVER_IP=$1

    if [ -z "$SERVER_IP" ]; then
        read -p "请输入服务器 IP 地址: " SERVER_IP
    fi

    log_info "正在生成自签名证书..."
    log_info "服务器 IP: $SERVER_IP"

    # 创建证书目录
    mkdir -p "$SSL_DIR"

    # 生成证书
    openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
        -keyout "$SSL_DIR/server.key" \
        -out "$SSL_DIR/server.crt" \
        -subj "/CN=${SERVER_IP}" \
        -addext "subjectAltName=DNS:${SERVER_IP},DNS:localhost,IP:${SERVER_IP},IP:127.0.0.1"

    # 设置权限
    chmod 600 "$SSL_DIR/server.key"
    chmod 644 "$SSL_DIR/server.crt"

    log_success "自签名证书生成完成！"
    echo ""
    echo "证书位置: $SSL_DIR/server.crt"
    echo "私钥位置: $SSL_DIR/server.key"
    echo "有效期: 365 天"
    echo ""
    log_warning "这是自签名证书，浏览器会显示安全警告"
    log_warning "客户端需要信任此证书或跳过验证"
}

# 申请 Let's Encrypt 证书
generate_letsencrypt() {
    local DOMAIN=$1
    local EMAIL=$2

    if [ -z "$DOMAIN" ]; then
        read -p "请输入域名: " DOMAIN
    fi

    if [ -z "$EMAIL" ]; then
        read -p "请输入邮箱（用于证书到期提醒）: " EMAIL
    fi

    log_info "正在申请 Let's Encrypt 证书..."
    log_info "域名: $DOMAIN"
    log_info "邮箱: $EMAIL"

    # 创建目录
    mkdir -p "$LETSENCRYPT_DIR" "$CERTBOT_DIR"

    # 检查 certbot 是否安装
    if ! command -v certbot &> /dev/null; then
        log_info "安装 certbot..."
        if command -v apt-get &> /dev/null; then
            apt-get update
            apt-get install -y certbot
        elif command -v yum &> /dev/null; then
            yum install -y certbot
        else
            log_error "请手动安装 certbot"
            exit 1
        fi
    fi

    # 确保 80 端口可用
    log_info "确保 80 端口可用（必要时临时停止 Docker/Nginx）..."
    docker compose down 2>/dev/null || true
    local nginx_was_active=false
    if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet nginx 2>/dev/null; then
        nginx_was_active=true
        systemctl stop nginx 2>/dev/null || true
    fi

    # 申请证书
    certbot certonly --standalone \
        -d "$DOMAIN" \
        --email "$EMAIL" \
        --agree-tos \
        --no-eff-email \
        --non-interactive

    if [ $? -eq 0 ]; then
        # 复制证书到项目目录
        mkdir -p "$SSL_DIR"
        cp "/etc/letsencrypt/live/$DOMAIN/fullchain.pem" "$SSL_DIR/server.crt"
        cp "/etc/letsencrypt/live/$DOMAIN/privkey.pem" "$SSL_DIR/server.key"
        chmod 600 "$SSL_DIR/server.key"
        chmod 644 "$SSL_DIR/server.crt"

        log_success "Let's Encrypt 证书申请成功！"
        echo ""
        echo "证书位置: $SSL_DIR/server.crt"
        echo "私钥位置: $SSL_DIR/server.key"
        echo "有效期: 90 天（自动续期）"
        echo ""
        log_info "证书会在到期前自动续期"
    else
        log_error "证书申请失败"
        if [ "$nginx_was_active" = true ]; then
            systemctl start nginx 2>/dev/null || true
        fi
        exit 1
    fi

    if [ "$nginx_was_active" = true ]; then
        systemctl start nginx 2>/dev/null || true
    fi
}

# 续期证书
renew_certificates() {
    log_info "正在检查并续期证书..."

    if command -v certbot &> /dev/null; then
        local nginx_proxy_enabled="no"
        if [ -f ".env" ]; then
            nginx_proxy_enabled=$(grep -E '^NGINX_PROXY_ENABLED=' .env 2>/dev/null | tail -1 | cut -d= -f2- | tr -d '"' | tr -d '\r')
        fi

        if [ "$nginx_proxy_enabled" = "yes" ]; then
            systemctl stop nginx 2>/dev/null || true
        else
            # 临时停止服务以释放 80 端口
            docker compose down 2>/dev/null || true
        fi

        certbot renew

        # 复制更新后的证书
        for domain_dir in /etc/letsencrypt/live/*/; do
            domain=$(basename "$domain_dir")
            if [ -f "$domain_dir/fullchain.pem" ]; then
                mkdir -p "$SSL_DIR"
                cp "$domain_dir/fullchain.pem" "$SSL_DIR/server.crt"
                cp "$domain_dir/privkey.pem" "$SSL_DIR/server.key"
                chmod 600 "$SSL_DIR/server.key"
                chmod 644 "$SSL_DIR/server.crt"
                log_success "证书已更新: $domain"
            fi
        done

        if [ "$nginx_proxy_enabled" = "yes" ]; then
            systemctl start nginx 2>/dev/null || true
            systemctl reload nginx 2>/dev/null || true
        else
            # 重启服务
            docker compose -f docker-compose.https.yml up -d
        fi

        log_success "证书续期完成"
    else
        log_warning "未安装 certbot，跳过续期"
    fi
}

# 查看证书状态
show_status() {
    echo "=========================================="
    echo "          SSL 证书状态"
    echo "=========================================="
    echo ""

    # 检查自签名证书
    if [ -f "$SSL_DIR/server.crt" ]; then
        echo "证书文件: $SSL_DIR/server.crt"
        echo ""
        openssl x509 -in "$SSL_DIR/server.crt" -noout -subject -dates -issuer 2>/dev/null || echo "无法读取证书信息"
        echo ""

        # 检查过期时间
        EXPIRY=$(openssl x509 -in "$SSL_DIR/server.crt" -noout -enddate 2>/dev/null | cut -d= -f2)
        if [ -n "$EXPIRY" ]; then
            EXPIRY_EPOCH=$(date -d "$EXPIRY" +%s 2>/dev/null || date -j -f "%b %d %T %Y %Z" "$EXPIRY" +%s 2>/dev/null)
            NOW_EPOCH=$(date +%s)
            DAYS_LEFT=$(( (EXPIRY_EPOCH - NOW_EPOCH) / 86400 ))

            if [ $DAYS_LEFT -lt 0 ]; then
                log_error "证书已过期！"
            elif [ $DAYS_LEFT -lt 30 ]; then
                log_warning "证书将在 $DAYS_LEFT 天后过期"
            else
                log_success "证书有效，还有 $DAYS_LEFT 天过期"
            fi
        fi
    else
        log_warning "未找到 SSL 证书"
        echo "运行以下命令生成证书:"
        echo "  $0 self-signed <IP>        # 自签名证书"
        echo "  $0 letsencrypt <域名>      # Let's Encrypt 证书"
    fi

    echo ""
    echo "=========================================="
}

# 设置自动续期
setup_auto_renew() {
    log_info "设置自动续期定时任务..."

    # 创建续期脚本
    cat > /etc/cron.d/certbot-renew << EOF
# 每天凌晨 2 点检查并续期证书
0 2 * * * root cd $(pwd) && ./ssl-manager.sh renew >> /var/log/certbot-renew.log 2>&1
EOF

    log_success "自动续期已配置，每天凌晨 2 点执行"
}

# 主函数
main() {
    case "${1:-help}" in
        self-signed)
            generate_self_signed "$2"
            ;;
        letsencrypt)
            generate_letsencrypt "$2" "$3"
            ;;
        renew)
            renew_certificates
            ;;
        status)
            show_status
            ;;
        auto-renew)
            setup_auto_renew
            ;;
        help|--help|-h)
            show_help
            ;;
        *)
            log_error "未知命令: $1"
            show_help
            exit 1
            ;;
    esac
}

main "$@"
