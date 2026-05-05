#!/bin/bash
# ============================================
# License Server 一键安装脚本（核心逻辑）
# ============================================
# 说明：
#   - 负责配置生成、证书处理、Docker 启动、健康检查、管理员初始化
#   - 支持交互/非交互
# ============================================

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

# 默认配置
SSL_MODE="" # self-signed / letsencrypt / http / custom
DOMAIN=""
SSL_EMAIL=""
SERVER_IP=""
INSTANCE_NAME="license-server-ai"
INSTANCE_NAME_SET=false

HTTP_PORT="80"
HTTPS_PORT="443"
BACKEND_PORT="8080"
FRONTEND_PORT="80"
MYSQL_PORT="3306"
REDIS_PORT="6379"
IMAGE_TAG="main"

# 上传限制（MB）
MAX_RELEASE_UPLOAD_MB="500"
MAX_REQUEST_BODY_MB="1024"
MULTIPART_MEMORY_MB="32"
MAX_SCRIPT_UPLOAD_MB="20"
MAX_SECURE_SCRIPT_UPLOAD_MB="20"

ADMIN_EMAIL="admin@example.com"
ADMIN_PASSWORD=""
LICENSE_MASTER_KEY=""
DOWNLOAD_TOKEN_SECRET=""
CLIENT_ACCESS_TOKEN_SECRET=""

CUSTOM_CERT_PATH=""
CUSTOM_KEY_PATH=""

NON_INTERACTIVE=false
YES=false
UPDATE_ONLY=false
UPDATE_VERSION=""
UPDATE_FORCE=false
FORCE_REINSTALL=false
REINSTALL_DB_MODE=""
SKIP_FIREWALL=false
NO_INIT_ADMIN=false
BUILD_NO_CACHE=true
NO_BUILD=true
ENABLE_NGINX_PROXY="no"
RESET_VOLUMES=false
REUSE_SECRETS=false

# 私有仓库 Token（仅用于 update）
GIT_TOKEN="${GIT_TOKEN:-}"

usage() {
    cat <<'EOF'
用法:
  ./scripts/install-core.sh [选项]

SSL & 端口:
  --ssl <mode>              self-signed / letsencrypt / http / custom
  --domain <domain>         域名（Let's Encrypt 必填）
  --email <email>           证书邮箱（Let's Encrypt 必填）
  --server-ip <ip>          指定服务器 IP（默认自动获取）
  --instance <name>         部署实例名，用于隔离容器/数据卷/网络（默认: license-server-ai）
  --http-port <port>        HTTP 端口（默认: 80）
  --https-port <port>       HTTPS 端口（默认: 443）
  --backend-port <port>     后端端口（默认: 8080）
  --mysql-port <port>       MySQL 对外端口（默认: 3306）
  --redis-port <port>       Redis 对外端口（默认: 6379）
  --image-tag <tag>         镜像标签（默认: main）
  --max-release-upload-mb <mb> 发布/热更新包上传上限（默认: 500）
  --max-request-body-mb <mb>   全局请求体上限（默认: 1024）
  --multipart-memory-mb <mb>   multipart 内存上限（默认: 32）
  --max-script-upload-mb <mb>  普通脚本上传上限（默认: 20）
  --max-secure-script-upload-mb <mb> 安全脚本上传上限（默认: 20）
  --cert <path>             自定义证书文件路径（custom 模式）
  --key <path>              自定义私钥文件路径（custom 模式）

管理员:
  --admin-email <email>     管理员邮箱（默认: admin@example.com）
  --admin-password <pass>   管理员密码（默认自动生成）

模式:
  --non-interactive         非交互模式（需提供必要参数）
  -y, --yes                 同 --non-interactive

更新:
  --update                  仅更新（调用 update.sh）
  --update-version <vX.Y>   更新到指定版本
  --update-force            更新时强制丢弃本地修改

行为控制:
  --nginx-proxy             启用 Nginx 反向代理（HTTPS 非 443 时可用）
  --skip-firewall           跳过防火墙配置
  --no-init-admin           跳过管理员初始化
  --build                   本地构建镜像（默认从 GHCR 拉取）
  --no-build                兼容参数（默认已是拉取镜像）
  --use-cache               构建时使用缓存（仅 --build）
  --force                   覆盖已有安装（重新生成配置）
  --reinstall-db <mode>     重新安装时数据库处理: keep/reset/reset-new

私有仓库:
  --git-token <token>       私有仓库 Token（仅用于 update）
EOF
}

parse_args() {
    while [ $# -gt 0 ]; do
        case "$1" in
            --ssl)
                SSL_MODE="$2"; shift 2 ;;
            --domain)
                DOMAIN="$2"; shift 2 ;;
            --email)
                SSL_EMAIL="$2"; shift 2 ;;
            --server-ip)
                SERVER_IP="$2"; shift 2 ;;
            --instance)
                INSTANCE_NAME="$2"; INSTANCE_NAME_SET=true; shift 2 ;;
            --http-port)
                HTTP_PORT="$2"; shift 2 ;;
            --https-port)
                HTTPS_PORT="$2"; shift 2 ;;
            --backend-port)
                BACKEND_PORT="$2"; shift 2 ;;
            --mysql-port)
                MYSQL_PORT="$2"; shift 2 ;;
            --redis-port)
                REDIS_PORT="$2"; shift 2 ;;
            --image-tag)
                IMAGE_TAG="$2"; shift 2 ;;
            --max-release-upload-mb)
                MAX_RELEASE_UPLOAD_MB="$2"; shift 2 ;;
            --max-request-body-mb)
                MAX_REQUEST_BODY_MB="$2"; shift 2 ;;
            --multipart-memory-mb)
                MULTIPART_MEMORY_MB="$2"; shift 2 ;;
            --max-script-upload-mb)
                MAX_SCRIPT_UPLOAD_MB="$2"; shift 2 ;;
            --max-secure-script-upload-mb)
                MAX_SECURE_SCRIPT_UPLOAD_MB="$2"; shift 2 ;;
            --admin-email)
                ADMIN_EMAIL="$2"; shift 2 ;;
            --admin-password)
                ADMIN_PASSWORD="$2"; shift 2 ;;
            --cert)
                CUSTOM_CERT_PATH="$2"; shift 2 ;;
            --key)
                CUSTOM_KEY_PATH="$2"; shift 2 ;;
            --nginx-proxy)
                ENABLE_NGINX_PROXY="yes"; shift ;;
            --non-interactive)
                NON_INTERACTIVE=true; shift ;;
            -y|--yes)
                NON_INTERACTIVE=true; YES=true; shift ;;
            --update)
                UPDATE_ONLY=true; shift ;;
            --update-version)
                UPDATE_VERSION="$2"; shift 2 ;;
            --update-force)
                UPDATE_FORCE=true; shift ;;
            --skip-firewall)
                SKIP_FIREWALL=true; shift ;;
            --no-init-admin)
                NO_INIT_ADMIN=true; shift ;;
            --build)
                NO_BUILD=false; shift ;;
            --no-build)
                NO_BUILD=true; shift ;;
            --use-cache)
                BUILD_NO_CACHE=false; shift ;;
            --force)
                FORCE_REINSTALL=true; shift ;;
            --reinstall-db)
                REINSTALL_DB_MODE="$2"; shift 2 ;;
            --git-token)
                GIT_TOKEN="$2"; shift 2 ;;
            --repo|--branch|--dir)
                # 兼容参数（由 bootstrap 处理，这里忽略）
                shift 2 ;;
            --ssh)
                # 兼容参数（由 bootstrap 处理，这里忽略）
                shift ;;
            -h|--help)
                usage; exit 0 ;;
            *)
                log_error "未知参数: $1"; usage; exit 1 ;;
        esac
    done
}

check_root() {
    if [ "$EUID" -ne 0 ]; then
        log_error "请使用 root 用户运行此脚本"
        log_info "使用: sudo ./install.sh"
        exit 1
    fi
}

check_requirements() {
    log_info "检查系统要求..."

    if [ -f /etc/os-release ]; then
        . /etc/os-release
        OS_NAME="$NAME"
        OS_ID="$ID"
        log_info "操作系统: $OS_NAME"
        if [ "$OS_ID" != "ubuntu" ] && [ "$OS_ID" != "debian" ]; then
            log_warning "当前系统非 Ubuntu/Debian，可能存在兼容性风险"
        fi
    else
        log_warning "无法检测操作系统"
    fi

    TOTAL_MEM=$(free -m | awk '/^Mem:/{print $2}')
    if [ "$TOTAL_MEM" -lt 1024 ]; then
        log_warning "内存小于 1GB，可能影响性能"
    else
        log_success "内存: ${TOTAL_MEM}MB"
    fi

    FREE_DISK=$(df -m / | awk 'NR==2 {print $4}')
    if [ "$FREE_DISK" -lt 5120 ]; then
        log_warning "磁盘空间小于 5GB"
    else
        log_success "可用磁盘: ${FREE_DISK}MB"
    fi
}

install_dependencies() {
    log_info "检查基础依赖..."

    if command -v apt-get &> /dev/null; then
        apt-get update
        apt-get install -y curl git openssl ca-certificates
    else
        log_warning "未检测到 apt-get，请手动安装 curl/git/openssl"
    fi
}

install_docker() {
    if command -v docker &> /dev/null; then
        log_success "Docker 已安装: $(docker --version)"
    else
        log_info "正在安装 Docker..."
        curl -fsSL https://get.docker.com | sh
        systemctl enable docker
        systemctl start docker
        log_success "Docker 安装完成"
    fi

    if docker compose version &> /dev/null; then
        log_success "Docker Compose 已安装"
    else
        log_info "正在安装 Docker Compose 插件..."
        if command -v apt-get &> /dev/null; then
            apt-get update
            apt-get install -y docker-compose-plugin
        fi
        log_success "Docker Compose 安装完成"
    fi
}

generate_password() {
    local length=${1:-16}
    openssl rand -base64 48 | tr -dc 'a-zA-Z0-9!@#$%^&*()_+' | head -c "$length"
}

generate_secret() {
    openssl rand -base64 32
}

get_server_ip() {
    PUBLIC_IP=$(curl -s --max-time 5 https://api.ipify.org 2>/dev/null || \
                curl -s --max-time 5 https://ifconfig.me 2>/dev/null || \
                curl -s --max-time 5 https://icanhazip.com 2>/dev/null || \
                echo "")

    if [ -z "$PUBLIC_IP" ]; then
        PUBLIC_IP=$(hostname -I | awk '{print $1}')
    fi

    echo "$PUBLIC_IP"
}

validate_positive_int() {
    local name="$1"
    local value="$2"
    if ! [[ "$value" =~ ^[0-9]+$ ]] || [ "$value" -le 0 ]; then
        log_error "${name} 必须是大于 0 的整数，当前值: ${value}"
        exit 1
    fi
}

validate_instance_name() {
    local value="$1"
    if ! [[ "$value" =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]{1,62}$ ]]; then
        log_error "部署实例名只能包含字母、数字、点、下划线、短横线，长度 2-63，并且必须以字母或数字开头"
        exit 1
    fi
}

is_port_in_use() {
    local port="$1"
    if command -v ss >/dev/null 2>&1; then
        ss -ltn "( sport = :${port} )" 2>/dev/null | awk 'NR>1 {found=1} END {exit found ? 0 : 1}'
        return $?
    fi
    if command -v netstat >/dev/null 2>&1; then
        netstat -ltn 2>/dev/null | awk -v p=":${port}" '$4 ~ p"$" {found=1} END {exit found ? 0 : 1}'
        return $?
    fi
    if command -v lsof >/dev/null 2>&1; then
        lsof -iTCP:"${port}" -sTCP:LISTEN >/dev/null 2>&1
        return $?
    fi
    return 1
}

prompt_port() {
    local label="$1"
    local default_value="$2"
    local var_name="$3"
    local value=""

    while true; do
        read -p "${label} [${default_value}]: " value
        value=${value:-$default_value}
        if ! [[ "$value" =~ ^[0-9]+$ ]] || [ "$value" -le 0 ] || [ "$value" -gt 65535 ]; then
            log_warning "${label} 必须是 1-65535 之间的端口"
            continue
        fi
        if is_port_in_use "$value"; then
            log_warning "端口 ${value} 已被占用，请重新输入"
            continue
        fi
        printf -v "$var_name" '%s' "$value"
        break
    done
}

validate_port_available() {
    local label="$1"
    local port="$2"
    validate_positive_int "$label" "$port"
    if [ "$port" -gt 65535 ]; then
        log_error "${label} 必须是 1-65535 之间的端口，当前值: ${port}"
        exit 1
    fi
    if is_port_in_use "$port"; then
        log_error "${label} 端口 ${port} 已被占用，请换一个端口"
        exit 1
    fi
}

validate_distinct_ports() {
    local entries=("$@")
    local i j left_name left_port right_name right_port
    for ((i = 0; i < ${#entries[@]}; i += 2)); do
        left_name="${entries[i]}"
        left_port="${entries[i + 1]}"
        for ((j = i + 2; j < ${#entries[@]}; j += 2)); do
            right_name="${entries[j]}"
            right_port="${entries[j + 1]}"
            if [ "$left_port" = "$right_port" ]; then
                log_error "${left_name} 和 ${right_name} 不能使用同一个端口: ${left_port}"
                exit 1
            fi
        done
    done
}

show_install_summary() {
    local host_name="${DOMAIN:-$SERVER_IP}"
    local frontend_url=""
    local backend_url="http://${host_name}:${BACKEND_PORT}"

    if [ "$SSL_MODE" = "http" ]; then
        if [ "$HTTP_PORT" = "80" ]; then
            frontend_url="http://${host_name}"
        else
            frontend_url="http://${host_name}:${HTTP_PORT}"
        fi
    else
        if [ "$ENABLE_NGINX_PROXY" = "yes" ] || [ "$HTTPS_PORT" = "443" ]; then
            frontend_url="https://${host_name}"
        else
            frontend_url="https://${host_name}:${HTTPS_PORT}"
        fi
    fi

    echo ""
    echo "=========================================="
    echo "         即将使用以下配置安装"
    echo "=========================================="
    echo "管理后台: ${frontend_url}"
    echo "后端 API: ${backend_url}"
    echo "HTTP 端口: ${HTTP_PORT}"
    if [ "$SSL_MODE" != "http" ]; then
        echo "HTTPS 端口: ${HTTPS_PORT}"
    fi
    echo "后端端口: ${BACKEND_PORT}"
    echo "MySQL 对外端口: ${MYSQL_PORT}"
    echo "Redis 对外端口: ${REDIS_PORT}"
    echo "部署实例名: ${INSTANCE_NAME}"
    echo "容器前缀: ${INSTANCE_NAME}-*"
    echo "数据卷前缀: ${INSTANCE_NAME}-*"
    echo "管理员邮箱: ${ADMIN_EMAIL}"
    echo "=========================================="
}

confirm_install_summary() {
    if [ "$NON_INTERACTIVE" = true ] || [ "$YES" = true ]; then
        return 0
    fi

    show_install_summary
    local confirm=""
    read -p "确认使用以上配置开始安装？[Y/n]: " confirm
    confirm=${confirm:-Y}
    case "$confirm" in
        y|Y|yes|YES) ;;
        *)
            log_info "已取消安装"
            exit 0
            ;;
    esac
}

get_env_value() {
    local key="$1"
    if [ ! -f ".env" ]; then
        return 1
    fi
    grep -E "^${key}=" .env | tail -1 | cut -d= -f2- | tr -d '\r' | sed 's/^"//;s/"$//'
}

load_existing_secrets() {
    if [ ! -f ".env" ]; then
        return 1
    fi

    local v
    v=$(get_env_value "MYSQL_ROOT_PASSWORD" || true); [ -n "$v" ] && MYSQL_ROOT_PASSWORD="$v"
    v=$(get_env_value "MYSQL_PASSWORD" || true); [ -n "$v" ] && MYSQL_PASSWORD="$v"
    v=$(get_env_value "REDIS_PASSWORD" || true); [ -n "$v" ] && REDIS_PASSWORD="$v"
    v=$(get_env_value "JWT_SECRET" || true); [ -n "$v" ] && JWT_SECRET="$v"
    v=$(get_env_value "LICENSE_MASTER_KEY" || true); [ -n "$v" ] && LICENSE_MASTER_KEY="$v"
    v=$(get_env_value "DOWNLOAD_TOKEN_SECRET" || true); [ -n "$v" ] && DOWNLOAD_TOKEN_SECRET="$v"
    v=$(get_env_value "CLIENT_ACCESS_TOKEN_SECRET" || true); [ -n "$v" ] && CLIENT_ACCESS_TOKEN_SECRET="$v"
    v=$(get_env_value "MAX_RELEASE_UPLOAD_MB" || true); [ -n "$v" ] && MAX_RELEASE_UPLOAD_MB="$v"
    v=$(get_env_value "MAX_REQUEST_BODY_MB" || true); [ -n "$v" ] && MAX_REQUEST_BODY_MB="$v"
    v=$(get_env_value "MULTIPART_MEMORY_MB" || true); [ -n "$v" ] && MULTIPART_MEMORY_MB="$v"
    v=$(get_env_value "MAX_SCRIPT_UPLOAD_MB" || true); [ -n "$v" ] && MAX_SCRIPT_UPLOAD_MB="$v"
    v=$(get_env_value "MAX_SECURE_SCRIPT_UPLOAD_MB" || true); [ -n "$v" ] && MAX_SECURE_SCRIPT_UPLOAD_MB="$v"
    return 0
}

load_existing_port_defaults() {
    if [ ! -f ".env" ]; then
        return 0
    fi

    local v
    v=$(get_env_value "HTTP_PORT" || true); [ -n "$v" ] && HTTP_PORT="$v"
    v=$(get_env_value "HTTPS_PORT" || true); [ -n "$v" ] && HTTPS_PORT="$v"
    v=$(get_env_value "BACKEND_PORT" || true); [ -n "$v" ] && BACKEND_PORT="$v"
    v=$(get_env_value "MYSQL_PORT" || true); [ -n "$v" ] && MYSQL_PORT="$v"
    v=$(get_env_value "REDIS_PORT" || true); [ -n "$v" ] && REDIS_PORT="$v"
    v=$(get_env_value "INSTANCE_NAME" || true); [ -n "$v" ] && [ "$INSTANCE_NAME_SET" = false ] && INSTANCE_NAME="$v"
    v=$(get_env_value "ADMIN_EMAIL" || true); [ -n "$v" ] && ADMIN_EMAIL="$v"
    return 0
}

reset_data_volumes() {
    log_warning "将重置数据库与缓存数据（会清空 MySQL/Redis）"
    docker rm -f "${INSTANCE_NAME}-backend" "${INSTANCE_NAME}-frontend" "${INSTANCE_NAME}-mysql" "${INSTANCE_NAME}-redis" >/dev/null 2>&1 || true
    docker volume rm -f "${INSTANCE_NAME}-mysql-data" "${INSTANCE_NAME}-redis-data" >/dev/null 2>&1 || true
}

apply_reinstall_db_mode() {
    case "$REINSTALL_DB_MODE" in
        ""|keep)
            REINSTALL_DB_MODE="keep"
            REUSE_SECRETS=true
            ;;
        reset)
            REINSTALL_DB_MODE="reset"
            REUSE_SECRETS=true
            RESET_VOLUMES=true
            ;;
        reset-new|reset_new)
            REINSTALL_DB_MODE="reset-new"
            RESET_VOLUMES=true
            ;;
        *)
            log_error "无效的 --reinstall-db 参数: $REINSTALL_DB_MODE"
            exit 1
            ;;
    esac
}

prompt_reinstall_db_mode() {
    if [ -n "$REINSTALL_DB_MODE" ]; then
        apply_reinstall_db_mode
        return 0
    fi

    echo ""
    echo "重新安装数据库处理方式:"
    echo "  1) 保留数据库（推荐，复用旧密码）"
    echo "  2) 重置数据库（清空数据，保留旧密码）"
    echo "  3) 重置数据库（清空数据，重新生成密码）"
    read -p "请选择 [1]: " db_choice
    db_choice=${db_choice:-1}

    case $db_choice in
        1) REINSTALL_DB_MODE="keep" ;;
        2) REINSTALL_DB_MODE="reset" ;;
        3) REINSTALL_DB_MODE="reset-new" ;;
        *) REINSTALL_DB_MODE="keep" ;;
    esac

    apply_reinstall_db_mode
}

resolve_domain_ips() {
    if [ -z "$DOMAIN" ]; then
        return 0
    fi

    if command -v getent >/dev/null 2>&1; then
        getent ahosts "$DOMAIN" 2>/dev/null | awk '{print $1}' | grep -E '^[0-9]+\\.' | sort -u
        return 0
    fi

    if command -v dig >/dev/null 2>&1; then
        dig +short "$DOMAIN" 2>/dev/null | grep -E '^[0-9]+\\.' | sort -u
        return 0
    fi

    if command -v nslookup >/dev/null 2>&1; then
        nslookup "$DOMAIN" 2>/dev/null | awk '/^Address: /{print $2}' | grep -E '^[0-9]+\\.' | sort -u
        return 0
    fi
}

check_domain_resolution() {
    if [ -z "$DOMAIN" ]; then
        return 0
    fi

    if [ -z "$SERVER_IP" ]; then
        SERVER_IP=$(get_server_ip)
    fi

    local ips
    ips=$(resolve_domain_ips || true)
    if [ -z "$ips" ]; then
        log_error "无法解析域名: ${DOMAIN}"
        return 1
    fi

    if ! echo "$ips" | grep -qx "$SERVER_IP"; then
        log_error "域名解析未指向当前服务器 IP"
        log_error "域名: ${DOMAIN}"
        log_error "解析结果: $(echo "$ips" | tr '\n' ' ')"
        log_error "服务器 IP: ${SERVER_IP}"
        return 1
    fi

    return 0
}

check_domain_access() {
    if [ -z "$DOMAIN" ]; then
        return 0
    fi

    local url=""
    if [ "$SSL_MODE" = "http" ]; then
        if [ "$HTTP_PORT" = "80" ]; then
            url="http://${DOMAIN}"
        else
            url="http://${DOMAIN}:${HTTP_PORT}"
        fi
    else
        if [ "$ENABLE_NGINX_PROXY" = "yes" ] || [ "$HTTPS_PORT" = "443" ]; then
            url="https://${DOMAIN}"
        else
            url="https://${DOMAIN}:${HTTPS_PORT}"
        fi
    fi

    log_info "检查域名访问: ${url}"
    local ok=false
    for i in {1..10}; do
        local code
        code=$(curl -k -s -o /dev/null -w "%{http_code}" --connect-timeout 5 "$url" || true)
        if [ -n "$code" ] && [ "$code" != "000" ] && [ "$code" -ge 200 ] && [ "$code" -lt 400 ]; then
            ok=true
            break
        fi
        sleep 2
    done

    if [ "$ok" != true ]; then
        log_error "域名访问检测失败，请检查 DNS 解析、证书和端口放行"
        log_error "检测地址: ${url}"
        return 1
    fi

    log_success "域名访问检测通过"
    return 0
}

interactive_config() {
    log_info "开始配置..."
    echo ""

    if [ -z "$SERVER_IP" ]; then
        DEFAULT_IP=$(get_server_ip)
        read -p "服务器 IP 地址 [$DEFAULT_IP]: " SERVER_IP
        SERVER_IP=${SERVER_IP:-$DEFAULT_IP}
    fi

    if [ "$INSTANCE_NAME_SET" = false ]; then
        read -p "部署实例名 [${INSTANCE_NAME}]: " input_instance
        INSTANCE_NAME=${input_instance:-$INSTANCE_NAME}
    fi
    validate_instance_name "$INSTANCE_NAME"

    if [ -z "$DOMAIN" ]; then
        read -p "域名（可留空）: " DOMAIN
    fi

    if [ -z "$SSL_MODE" ]; then
        echo ""
        echo "=========================================="
        echo "         选择 SSL 证书类型"
        echo "=========================================="
        echo ""
        echo "  1) 自签名证书（推荐用于 IP 地址部署）"
        echo "  2) Let's Encrypt 证书（推荐用于域名部署）"
        echo "  3) 仅 HTTP（不推荐）"
        echo "  4) 使用自定义证书（已购买证书）"
        echo ""

        read -p "请选择 [1]: " ssl_choice
        ssl_choice=${ssl_choice:-1}

        case $ssl_choice in
            1) SSL_MODE="self-signed" ;;
            2) SSL_MODE="letsencrypt" ;;
            3) SSL_MODE="http" ;;
            4) SSL_MODE="custom" ;;
            *) SSL_MODE="self-signed" ;;
        esac
    fi

    if [ "$SSL_MODE" = "custom" ]; then
        local default_cert="$ROOT_DIR/certs/ssl/server.crt"
        local default_key="$ROOT_DIR/certs/ssl/server.key"

        while true; do
            read -p "证书文件路径 [${default_cert}]: " CUSTOM_CERT_PATH
            CUSTOM_CERT_PATH=${CUSTOM_CERT_PATH:-$default_cert}

            read -p "私钥文件路径 [${default_key}]: " CUSTOM_KEY_PATH
            CUSTOM_KEY_PATH=${CUSTOM_KEY_PATH:-$default_key}

            if [ -f "$CUSTOM_CERT_PATH" ] && [ -f "$CUSTOM_KEY_PATH" ]; then
                break
            fi

            echo ""
            log_warning "未找到证书或私钥文件"
            echo "请将证书放置到以下默认路径，或重新输入有效路径："
            echo "  证书: $default_cert"
            echo "  私钥: $default_key"
            echo ""
            read -p "已放置完成？按回车继续，或输入 q 退出: " confirm
            if [ "$confirm" = "q" ] || [ "$confirm" = "Q" ]; then
                exit 1
            fi
        done
    fi

    if [ "$SSL_MODE" = "letsencrypt" ]; then
        if [ -z "$DOMAIN" ]; then
            read -p "请输入域名: " DOMAIN
        fi
        if [ -z "$SSL_EMAIL" ]; then
            read -p "请输入邮箱（用于证书到期提醒）: " SSL_EMAIL
        fi
        if [ -z "$DOMAIN" ] || [ -z "$SSL_EMAIL" ]; then
            log_error "Let's Encrypt 需要域名和邮箱"
            exit 1
        fi
    fi

    if [ "$SSL_MODE" = "http" ]; then
        prompt_port "HTTP 端口" "80" HTTP_PORT
    else
        prompt_port "HTTP 端口（用于重定向）" "80" HTTP_PORT
        prompt_port "HTTPS 端口" "443" HTTPS_PORT
    fi

    prompt_port "后端 API 端口" "8080" BACKEND_PORT

    prompt_port "MySQL 对外端口" "3306" MYSQL_PORT

    prompt_port "Redis 对外端口" "6379" REDIS_PORT

    read -p "管理员邮箱 [admin@example.com]: " ADMIN_EMAIL
    ADMIN_EMAIL=${ADMIN_EMAIL:-admin@example.com}
}

validate_non_interactive() {
    if [ -z "$SSL_MODE" ]; then
        log_error "非交互模式必须指定 --ssl"
        exit 1
    fi

    if [ "$SSL_MODE" = "letsencrypt" ]; then
        if [ -z "$DOMAIN" ] || [ -z "$SSL_EMAIL" ]; then
            log_error "Let's Encrypt 模式必须指定 --domain 和 --email"
            exit 1
        fi
    fi

    if [ "$SSL_MODE" = "custom" ]; then
        if [ -z "$CUSTOM_CERT_PATH" ]; then
            CUSTOM_CERT_PATH="$ROOT_DIR/certs/ssl/server.crt"
        fi
        if [ -z "$CUSTOM_KEY_PATH" ]; then
            CUSTOM_KEY_PATH="$ROOT_DIR/certs/ssl/server.key"
        fi
        if [ ! -f "$CUSTOM_CERT_PATH" ] || [ ! -f "$CUSTOM_KEY_PATH" ]; then
            log_error "custom 模式下证书或私钥不存在，请使用 --cert/--key 指定有效路径"
            exit 1
        fi
    fi

    if [ -z "$SERVER_IP" ]; then
        SERVER_IP=$(get_server_ip)
        if [ -z "$SERVER_IP" ]; then
            log_error "无法自动获取服务器 IP，请使用 --server-ip 指定"
            exit 1
        fi
    fi

    validate_instance_name "$INSTANCE_NAME"
}

create_env_file() {
    log_info "创建环境配置文件..."

    cat > .env << EOF
# ============================================
# License Server 环境配置
# 生成时间: $(date '+%Y-%m-%d %H:%M:%S')
# ============================================

# 服务器配置
SERVER_IP=${SERVER_IP}
INSTANCE_NAME=${INSTANCE_NAME}
DOMAIN=${DOMAIN:-}
SSL_MODE=${SSL_MODE}
BACKEND_PORT=${BACKEND_PORT}
HTTP_PORT=${HTTP_PORT}
HTTPS_PORT=${HTTPS_PORT}
FRONTEND_PORT=${HTTP_PORT}
IMAGE_TAG=${IMAGE_TAG}

# MySQL 配置
MYSQL_ROOT_PASSWORD=${MYSQL_ROOT_PASSWORD}
MYSQL_DATABASE=license_server
MYSQL_USER=license_admin
MYSQL_PASSWORD=${MYSQL_PASSWORD}
MYSQL_PORT=${MYSQL_PORT}

# Redis 配置
REDIS_PASSWORD=${REDIS_PASSWORD}
REDIS_PORT=${REDIS_PORT}

# JWT 配置
JWT_SECRET=${JWT_SECRET}
JWT_EXPIRE_HOURS=24

# Provider 凭证信封加密主密钥
LICENSE_MASTER_KEY=${LICENSE_MASTER_KEY}

# 客户端下载与会话 token 独立密钥
DOWNLOAD_TOKEN_SECRET=${DOWNLOAD_TOKEN_SECRET}
CLIENT_ACCESS_TOKEN_SECRET=${CLIENT_ACCESS_TOKEN_SECRET}

# 安全配置
SERVER_MODE=release
TLS_ENABLED=$([ "$SSL_MODE" = "http" ] && echo false || echo true)
MAX_RELEASE_UPLOAD_MB=${MAX_RELEASE_UPLOAD_MB}
MAX_REQUEST_BODY_MB=${MAX_REQUEST_BODY_MB}
MULTIPART_MEMORY_MB=${MULTIPART_MEMORY_MB}
MAX_SCRIPT_UPLOAD_MB=${MAX_SCRIPT_UPLOAD_MB}
MAX_SECURE_SCRIPT_UPLOAD_MB=${MAX_SECURE_SCRIPT_UPLOAD_MB}

# 管理员配置
ADMIN_EMAIL=${ADMIN_EMAIL}
ADMIN_PASSWORD=${ADMIN_PASSWORD}

# 前端配置
VITE_API_URL=/api
EOF

    chmod 600 .env
    log_success ".env 文件创建完成"
}

create_docker_config() {
    log_info "创建 Docker 配置文件..."

    local origin_lines=""

    if [ "$SSL_MODE" = "http" ]; then
        origin_lines=$(cat <<EOF
    - "http://${SERVER_IP}:${HTTP_PORT}"
    - "http://${SERVER_IP}"
EOF
)
        if [ -n "$DOMAIN" ]; then
            origin_lines="${origin_lines}
    - \"http://${DOMAIN}:${HTTP_PORT}\"
    - \"http://${DOMAIN}\""
        fi
    else
        local host_name="${DOMAIN:-$SERVER_IP}"
        origin_lines=$(cat <<EOF
    - "https://${host_name}:${HTTPS_PORT}"
    - "https://${host_name}"
    - "http://${host_name}:${HTTP_PORT}"
EOF
)
    fi

    origin_lines="${origin_lines}
    - \"http://localhost:3000\"
    - \"http://127.0.0.1:3000\""

    cat > config.docker.yaml << EOF
# License Server Docker 配置
# 生成时间: $(date '+%Y-%m-%d %H:%M:%S')

server:
  host: "0.0.0.0"
  port: 8080
  mode: "release"
  tls:
    enabled: false
    cert_file: "/app/certs/server.crt"
    key_file: "/app/certs/server.key"

database:
  driver: "mysql"
  host: "mysql"
  port: 3306
  username: "license_admin"
  password: "${MYSQL_PASSWORD}"
  database: "license_server"
  charset: "utf8mb4"
  max_idle_conns: 10
  max_open_conns: 100

redis:
  host: "redis"
  port: 6379
  password: "${REDIS_PASSWORD}"
  db: 0

jwt:
  secret: "${JWT_SECRET}"
  expire_hours: 24

rsa:
  key_size: 2048

storage:
  scripts_dir: "/app/storage/scripts"
  releases_dir: "/app/storage/releases"

log:
  level: "info"
  file: "/app/logs/app.log"
  max_size: 100
  max_backups: 5
  max_age: 30

email:
  enabled: false
  smtp_host: ""
  smtp_port: 587
  username: ""
  password: ""
  from: ""

security:
  max_login_attempts: 5
  login_lock_minutes: 15
  ip_max_attempts: 20
  ip_lock_minutes: 30
  password_min_length: 8
  password_require_num: true
  password_require_sym: true
  csrf_enabled: false
  csrf_token_expiry: 60
  csrf_cookie_name: "csrf_token"
  enable_security_headers: true
  max_release_upload_mb: ${MAX_RELEASE_UPLOAD_MB}
  max_request_body_mb: ${MAX_REQUEST_BODY_MB}
  multipart_memory_mb: ${MULTIPART_MEMORY_MB}
  max_script_upload_mb: ${MAX_SCRIPT_UPLOAD_MB}
  max_secure_script_upload_mb: ${MAX_SECURE_SCRIPT_UPLOAD_MB}
  download_token_secret: "${DOWNLOAD_TOKEN_SECRET}"
  download_token_expire_seconds: 300
  client_access_token_secret: "${CLIENT_ACCESS_TOKEN_SECRET}"
  client_access_token_expire_seconds: 900
  client_refresh_token_expire_seconds: 2592000
  allowed_origins:
${origin_lines}
EOF

    log_success "Docker 配置文件创建完成"
}

create_directories() {
    log_info "创建必要目录..."

    mkdir -p storage/scripts
    mkdir -p storage/releases/hotupdate
    mkdir -p logs
    mkdir -p certs/ssl
    mkdir -p certs/letsencrypt
    mkdir -p certs/certbot

    chown -R 1000:1000 storage logs || true
    chmod -R 755 storage logs certs

    log_success "目录创建完成"
}

update_frontend_config() {
    if [ "${LS_NO_SOURCE:-}" = "1" ] || [ ! -d "admin" ]; then
        log_info "未检测到前端源码，跳过前端配置更新"
        return 0
    fi

    log_info "更新前端配置..."

    cat > admin/.env.production << EOF
VITE_API_URL=/api
EOF

    log_success "前端配置更新完成"
}

disable_certbot_auto_renew() {
    local removed=false
    if [ -f /etc/cron.d/certbot-renew ]; then
        rm -f /etc/cron.d/certbot-renew
        removed=true
    fi

    if command -v systemctl >/dev/null 2>&1; then
        if systemctl list-unit-files 2>/dev/null | grep -q "^certbot.timer"; then
            systemctl stop certbot.timer >/dev/null 2>&1 || true
            systemctl disable certbot.timer >/dev/null 2>&1 || true
            removed=true
        fi
    fi

    if [ "$removed" = true ]; then
        log_info "已禁用 Let's Encrypt 自动续期任务"
    fi
}

generate_ssl_cert() {
    if [ "$SSL_MODE" = "http" ]; then
        log_info "HTTP 模式，跳过证书生成"
        return 0
    fi

    if [ "$SSL_MODE" != "letsencrypt" ]; then
        disable_certbot_auto_renew
    fi

    local ssl_manager="${ROOT_DIR}/ssl-manager.sh"
    if [ ! -x "$ssl_manager" ]; then
        log_warning "未找到 ssl-manager.sh，跳过证书生成"
        if [ "$SSL_MODE" != "custom" ]; then
            return 0
        fi
    fi

    case $SSL_MODE in
        self-signed)
            "$ssl_manager" self-signed "$SERVER_IP"
            ;;
        letsencrypt)
            "$ssl_manager" letsencrypt "$DOMAIN" "$SSL_EMAIL"
            "$ssl_manager" auto-renew
            ;;
        custom)
            local default_cert="$ROOT_DIR/certs/ssl/server.crt"
            local default_key="$ROOT_DIR/certs/ssl/server.key"

            [ -z "$CUSTOM_CERT_PATH" ] && CUSTOM_CERT_PATH="$default_cert"
            [ -z "$CUSTOM_KEY_PATH" ] && CUSTOM_KEY_PATH="$default_key"

            if [ ! -f "$CUSTOM_CERT_PATH" ] || [ ! -f "$CUSTOM_KEY_PATH" ]; then
                log_error "custom 模式下证书或私钥不存在"
                log_error "证书: $CUSTOM_CERT_PATH"
                log_error "私钥: $CUSTOM_KEY_PATH"
                exit 1
            fi

            # 将证书放入 certs/ssl 供容器挂载使用
            cp "$CUSTOM_CERT_PATH" "$default_cert"
            cp "$CUSTOM_KEY_PATH" "$default_key"
            chmod 644 "$default_cert"
            chmod 600 "$default_key"

            log_success "已使用自定义证书"
            ;;
    esac
}

start_services() {
    if [ "$SSL_MODE" = "http" ]; then
        COMPOSE_FILE="docker-compose.yml"
    else
        COMPOSE_FILE="docker-compose.https.yml"
    fi

    if [ "$NO_BUILD" = true ]; then
        log_info "拉取 Docker 镜像..."
        docker compose -f "$COMPOSE_FILE" pull
        log_info "执行数据库迁移..."
        docker compose -f "$COMPOSE_FILE" run --rm migrate
        log_info "启动服务..."
        docker compose -f "$COMPOSE_FILE" up -d
    else
        log_info "构建 Docker 镜像（首次可能需要几分钟）..."
        if [ "$BUILD_NO_CACHE" = true ]; then
            docker compose -f "$COMPOSE_FILE" build --no-cache
        else
            docker compose -f "$COMPOSE_FILE" build
        fi
        log_info "执行数据库迁移..."
        docker compose -f "$COMPOSE_FILE" run --rm migrate
        log_info "启动服务..."
        docker compose -f "$COMPOSE_FILE" up -d
    fi

    log_info "等待服务启动..."
    sleep 15

    if docker compose -f "$COMPOSE_FILE" ps | grep -q "Up"; then
        log_success "服务启动成功"
    else
        log_error "服务启动失败，请检查日志: docker compose -f $COMPOSE_FILE logs"
        exit 1
    fi
}

init_admin() {
    if [ "$NO_INIT_ADMIN" = true ]; then
        log_info "跳过管理员初始化"
        return 0
    fi

    log_info "初始化管理员账号..."
    local mysql_container="${INSTANCE_NAME}-mysql"

    log_info "等待数据库就绪..."
    local max_retries=30
    local retry=0
    while [ $retry -lt $max_retries ]; do
        if docker exec "$mysql_container" mysql -u root -p"${MYSQL_ROOT_PASSWORD}" -e "SELECT 1" &>/dev/null; then
            log_success "数据库已就绪"
            break
        fi
        retry=$((retry + 1))
        log_info "等待数据库... ($retry/$max_retries)"
        sleep 2
    done

    if [ $retry -eq $max_retries ]; then
        log_error "数据库连接超时"
        return 1
    fi

    log_info "生成密码哈希..."
    PASSWORD_HASH=$(docker run --rm python:3-alpine sh -c "pip install -q bcrypt && python -c \"import bcrypt; print(bcrypt.hashpw(b'${ADMIN_PASSWORD}', bcrypt.gensalt(10)).decode())\"" 2>/dev/null)

    if [ -z "$PASSWORD_HASH" ]; then
        log_error "无法生成密码哈希"
        return 1
    fi

    cat > /tmp/init_admin.sql << 'EOSQL'
SET @tenant_exists = (SELECT COUNT(*) FROM tenants WHERE slug = 'default');
SET @tenant_id = UUID();
INSERT INTO tenants (id, name, slug, plan, status, created_at, updated_at)
SELECT @tenant_id, '管理后台', 'default', 'enterprise', 'active', NOW(), NOW()
WHERE @tenant_exists = 0;
SET @final_tenant_id = (SELECT id FROM tenants WHERE slug = 'default' LIMIT 1);
EOSQL

    cat >> /tmp/init_admin.sql << EOSQL
SET @admin_exists = (SELECT COUNT(*) FROM team_members WHERE email = '${ADMIN_EMAIL}');
INSERT INTO team_members (id, tenant_id, email, password, name, role, status, created_at, updated_at, email_verified)
SELECT UUID(), @final_tenant_id, '${ADMIN_EMAIL}', '${PASSWORD_HASH}', '管理员', 'owner', 'active', NOW(), NOW(), 1
WHERE @admin_exists = 0;
SELECT COUNT(*) as created FROM team_members WHERE email = '${ADMIN_EMAIL}';
EOSQL

    docker cp /tmp/init_admin.sql "${mysql_container}:/tmp/init_admin.sql"
    docker exec "$mysql_container" mysql -u root -p"${MYSQL_ROOT_PASSWORD}" --default-character-set=utf8mb4 license_server -e "source /tmp/init_admin.sql"

    local result=$?
    rm -f /tmp/init_admin.sql
    docker exec "$mysql_container" rm -f /tmp/init_admin.sql

    if [ $result -eq 0 ]; then
        local count=$(docker exec "$mysql_container" mysql -u root -p"${MYSQL_ROOT_PASSWORD}" -N -e "SELECT COUNT(*) FROM license_server.team_members WHERE email='${ADMIN_EMAIL}';" 2>/dev/null)
        if [ "$count" = "1" ]; then
            log_success "管理员账号初始化完成"
        else
            log_warning "管理员账号创建可能失败，请手动检查"
        fi
    else
        log_error "管理员账号创建失败，错误码: $result"
    fi
}

install_nginx_proxy() {
    if [ "$ENABLE_NGINX_PROXY" != "yes" ]; then
        return 0
    fi

    log_info "安装 Nginx 反向代理..."

    if command -v nginx &> /dev/null; then
        log_success "Nginx 已安装"
    else
        if command -v apt-get &> /dev/null; then
            apt-get update
            apt-get install -y nginx
            log_success "Nginx 安装完成"
        else
            log_error "未检测到 apt-get，无法自动安装 Nginx"
            return 1
        fi
    fi

    local host_name="${DOMAIN:-$SERVER_IP}"
    local SSL_CERT=""
    local SSL_KEY=""

    if [ "$SSL_MODE" = "letsencrypt" ] && [ -d "/etc/letsencrypt/live/${DOMAIN}" ]; then
        SSL_CERT="/etc/letsencrypt/live/${DOMAIN}/fullchain.pem"
        SSL_KEY="/etc/letsencrypt/live/${DOMAIN}/privkey.pem"
    else
        SSL_CERT="$(pwd)/certs/ssl/server.crt"
        SSL_KEY="$(pwd)/certs/ssl/server.key"
    fi

    cat > /etc/nginx/sites-available/license-server << EOF
# License Server Nginx 反向代理配置
# 生成时间: $(date '+%Y-%m-%d %H:%M:%S')

server {
    listen 80;
    server_name ${host_name};
    return 301 https://\$server_name\$request_uri;
}

server {
    listen 443 ssl http2;
    server_name ${host_name};
    client_max_body_size ${MAX_RELEASE_UPLOAD_MB}M;

    ssl_certificate ${SSL_CERT};
    ssl_certificate_key ${SSL_KEY};

    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384;
    ssl_prefer_server_ciphers off;
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 1d;

    add_header X-Frame-Options "SAMEORIGIN" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;

    location / {
        proxy_pass https://127.0.0.1:${HTTPS_PORT};
        proxy_ssl_verify off;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;

        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 86400;
        proxy_send_timeout 86400;
        proxy_buffering off;
        proxy_buffer_size 4k;
    }
}
EOF

    ln -sf /etc/nginx/sites-available/license-server /etc/nginx/sites-enabled/
    rm -f /etc/nginx/sites-enabled/default

    if nginx -t; then
        systemctl restart nginx
        systemctl enable nginx
        log_success "Nginx 反向代理配置完成"
    else
        log_error "Nginx 配置测试失败，请检查配置"
        return 1
    fi
}

configure_firewall() {
    if [ "$SKIP_FIREWALL" = true ]; then
        log_info "跳过防火墙配置"
        return 0
    fi

    log_info "配置防火墙..."

    if command -v ufw &> /dev/null; then
        if [ "$SSL_MODE" = "http" ]; then
            ufw allow ${HTTP_PORT}/tcp
            ufw allow ${BACKEND_PORT}/tcp
        else
            ufw allow ${HTTP_PORT}/tcp
            ufw allow ${HTTPS_PORT}/tcp
        fi
        log_success "UFW 防火墙规则已添加"
    elif command -v firewall-cmd &> /dev/null; then
        if [ "$SSL_MODE" = "http" ]; then
            firewall-cmd --permanent --add-port=${HTTP_PORT}/tcp
            firewall-cmd --permanent --add-port=${BACKEND_PORT}/tcp
        else
            firewall-cmd --permanent --add-port=${HTTP_PORT}/tcp
            firewall-cmd --permanent --add-port=${HTTPS_PORT}/tcp
        fi
        firewall-cmd --reload
        log_success "Firewalld 防火墙规则已添加"
    else
        log_warning "未检测到防火墙，请手动配置"
    fi
}

save_credentials() {
    local CREDENTIALS_FILE="credentials.txt"
    local FRONTEND_URL=""
    local BACKEND_URL=""
    local host_name="${DOMAIN:-$SERVER_IP}"

    if [ "$SSL_MODE" = "http" ]; then
        if [ "$HTTP_PORT" = "80" ]; then
            FRONTEND_URL="http://${host_name}"
        else
            FRONTEND_URL="http://${host_name}:${HTTP_PORT}"
        fi
        BACKEND_URL="http://${host_name}:${BACKEND_PORT}"
    else
        if [ "$ENABLE_NGINX_PROXY" = "yes" ] || [ "$HTTPS_PORT" = "443" ]; then
            FRONTEND_URL="https://${host_name}"
        else
            FRONTEND_URL="https://${host_name}:${HTTPS_PORT}"
        fi
        BACKEND_URL="http://${host_name}:${BACKEND_PORT}"
    fi

    cat > "$CREDENTIALS_FILE" << EOF
╔══════════════════════════════════════════════════════════════════════════╗
║                    License Server 安装凭据                               ║
║                    生成时间: $(date '+%Y-%m-%d %H:%M:%S')                         ║
╚══════════════════════════════════════════════════════════════════════════╝

【重要提示】请妥善保管此文件，首次登录后请立即修改密码！

═══════════════════════════════════════════════════════════════════════════
                              访问地址
═══════════════════════════════════════════════════════════════════════════

前端管理后台: ${FRONTEND_URL}
后端 API 地址: ${BACKEND_URL}

═══════════════════════════════════════════════════════════════════════════
                            管理员账号
═══════════════════════════════════════════════════════════════════════════

邮箱: ${ADMIN_EMAIL}
密码: ${ADMIN_PASSWORD}

═══════════════════════════════════════════════════════════════════════════
                            数据库信息
═══════════════════════════════════════════════════════════════════════════

MySQL Root 密码: ${MYSQL_ROOT_PASSWORD}
MySQL 用户名:    license_admin
MySQL 密码:      ${MYSQL_PASSWORD}
MySQL 数据库:    license_server

Redis 密码:      ${REDIS_PASSWORD}

═══════════════════════════════════════════════════════════════════════════
                              JWT 密钥
═══════════════════════════════════════════════════════════════════════════

${JWT_SECRET}

═══════════════════════════════════════════════════════════════════════════
                    Provider 凭证加密主密钥
═══════════════════════════════════════════════════════════════════════════

${LICENSE_MASTER_KEY}

═══════════════════════════════════════════════════════════════════════════
                            常用命令
═══════════════════════════════════════════════════════════════════════════

查看服务状态:    docker compose ps
查看日志:        docker compose logs -f
重启服务:        docker compose restart
停止服务:        docker compose down
更新服务:        ./update.sh
EOF

    chmod 600 "$CREDENTIALS_FILE"
    log_success "凭据已保存到 $CREDENTIALS_FILE"
}

print_completion() {
    local FRONTEND_URL=""
    local host_name="${DOMAIN:-$SERVER_IP}"
    if [ "$SSL_MODE" = "http" ]; then
        if [ "$HTTP_PORT" = "80" ]; then
            FRONTEND_URL="http://${host_name}"
        else
            FRONTEND_URL="http://${host_name}:${HTTP_PORT}"
        fi
    else
        if [ "$ENABLE_NGINX_PROXY" = "yes" ] || [ "$HTTPS_PORT" = "443" ]; then
            FRONTEND_URL="https://${host_name}"
        else
            FRONTEND_URL="https://${host_name}:${HTTPS_PORT}"
        fi
    fi

    echo ""
    echo -e "${GREEN}"
    echo "╔══════════════════════════════════════════════════════════════════════════╗"
    echo "║                                                                          ║"
    echo "║                    🎉 安装完成！                                         ║"
    echo "║                                                                          ║"
    echo "╚══════════════════════════════════════════════════════════════════════════╝"
    echo -e "${NC}"
    echo ""
    echo -e "  ${BLUE}前端管理后台:${NC} ${FRONTEND_URL}"
    echo -e "  ${BLUE}管理员邮箱:${NC}   ${ADMIN_EMAIL}"
    echo -e "  ${BLUE}管理员密码:${NC}   ${ADMIN_PASSWORD}"
    echo ""
    echo -e "  ${YELLOW}【重要】所有凭据已保存到 credentials.txt，请妥善保管！${NC}"
    echo -e "  ${YELLOW}【重要】首次登录后请立即修改默认密码！${NC}"
    echo ""
}

run_update() {
    if [ ! -x "./update.sh" ]; then
        log_error "未找到 update.sh"
        exit 1
    fi

    local args=()
    if [ -n "$UPDATE_VERSION" ]; then
        args+=("$UPDATE_VERSION")
    fi
    if [ "$UPDATE_FORCE" = true ]; then
        args+=("--force")
    fi

    log_info "执行更新脚本..."
    if [ -n "$GIT_TOKEN" ]; then
        GIT_TOKEN="$GIT_TOKEN" ./update.sh "${args[@]}"
    else
        ./update.sh "${args[@]}"
    fi
    exit 0
}

main() {
    parse_args "$@"
    check_root

    if [ "$UPDATE_ONLY" = true ]; then
        run_update
    fi

    if [ -n "${LS_REINSTALL_DB:-}" ] && [ -z "$REINSTALL_DB_MODE" ]; then
        REINSTALL_DB_MODE="${LS_REINSTALL_DB}"
    fi

    # 已安装检测
    if [ -f ".env" ] && [ "$FORCE_REINSTALL" = false ]; then
        if [ "$NON_INTERACTIVE" = true ]; then
            log_error "检测到已有安装，请使用 --force 覆盖或 --update 更新"
            exit 1
        fi

        echo ""
        echo "检测到已有安装，请选择操作:"
        echo "  1) 更新到最新版本"
        echo "  2) 重新安装（覆盖配置）"
        echo "  3) 退出"
        read -p "请选择 [1]: " install_choice
        install_choice=${install_choice:-1}

        case $install_choice in
            1)
                UPDATE_ONLY=true
                run_update
                ;;
            2)
                FORCE_REINSTALL=true
                if [ "$NON_INTERACTIVE" = true ]; then
                    if [ -z "$REINSTALL_DB_MODE" ]; then
                        REINSTALL_DB_MODE="keep"
                    fi
                    apply_reinstall_db_mode
                else
                    prompt_reinstall_db_mode
                fi
                ;;
            3)
                log_info "已退出"
                exit 0
                ;;
        esac
    fi

    if [ "$FORCE_REINSTALL" = true ] && [ -f ".env" ] && [ -z "$REINSTALL_DB_MODE" ]; then
        if [ "$NON_INTERACTIVE" = true ]; then
            REINSTALL_DB_MODE="keep"
            apply_reinstall_db_mode
        else
            prompt_reinstall_db_mode
        fi
    fi

    if [ -f ".env" ]; then
        load_existing_port_defaults
    fi

    if [ "$NON_INTERACTIVE" = true ]; then
        validate_non_interactive
    else
        interactive_config
    fi

    case $SSL_MODE in
        self-signed|letsencrypt|http|custom) ;;
        *)
            log_error "无效的 SSL 模式: $SSL_MODE"; exit 1 ;;
    esac

    validate_positive_int "MAX_RELEASE_UPLOAD_MB" "$MAX_RELEASE_UPLOAD_MB"
    validate_positive_int "MAX_REQUEST_BODY_MB" "$MAX_REQUEST_BODY_MB"
    validate_positive_int "MULTIPART_MEMORY_MB" "$MULTIPART_MEMORY_MB"
    validate_positive_int "MAX_SCRIPT_UPLOAD_MB" "$MAX_SCRIPT_UPLOAD_MB"
    validate_positive_int "MAX_SECURE_SCRIPT_UPLOAD_MB" "$MAX_SECURE_SCRIPT_UPLOAD_MB"

    if [ "$SSL_MODE" = "http" ] && [ "$ENABLE_NGINX_PROXY" = "yes" ]; then
        log_warning "HTTP 模式下无法启用 Nginx 反向代理，已忽略 --nginx-proxy"
        ENABLE_NGINX_PROXY="no"
    fi

    validate_port_available "HTTP_PORT" "$HTTP_PORT"
    if [ "$SSL_MODE" != "http" ]; then
        validate_port_available "HTTPS_PORT" "$HTTPS_PORT"
        validate_distinct_ports \
            "HTTP_PORT" "$HTTP_PORT" \
            "HTTPS_PORT" "$HTTPS_PORT" \
            "BACKEND_PORT" "$BACKEND_PORT" \
            "MYSQL_PORT" "$MYSQL_PORT" \
            "REDIS_PORT" "$REDIS_PORT"
    else
        validate_distinct_ports \
            "HTTP_PORT" "$HTTP_PORT" \
            "BACKEND_PORT" "$BACKEND_PORT" \
            "MYSQL_PORT" "$MYSQL_PORT" \
            "REDIS_PORT" "$REDIS_PORT"
    fi
    validate_port_available "BACKEND_PORT" "$BACKEND_PORT"
    validate_port_available "MYSQL_PORT" "$MYSQL_PORT"
    validate_port_available "REDIS_PORT" "$REDIS_PORT"
    confirm_install_summary

    check_requirements
    install_dependencies
    install_docker

    if [ "$REUSE_SECRETS" = true ]; then
        if ! load_existing_secrets; then
            log_error "未找到旧 .env，无法保留数据库密码"
            log_error "请确认当前目录存在 .env，或选择重置数据库"
            exit 1
        fi
    fi

    if [ "$RESET_VOLUMES" = true ]; then
        reset_data_volumes
    fi

    if [ "$REINSTALL_DB_MODE" = "reset-new" ]; then
        MYSQL_ROOT_PASSWORD=""
        MYSQL_PASSWORD=""
        REDIS_PASSWORD=""
        JWT_SECRET=""
        LICENSE_MASTER_KEY=""
        DOWNLOAD_TOKEN_SECRET=""
        CLIENT_ACCESS_TOKEN_SECRET=""
    fi

    if [ -z "$MYSQL_ROOT_PASSWORD" ]; then
        MYSQL_ROOT_PASSWORD=$(generate_password 20)
    fi
    if [ -z "$MYSQL_PASSWORD" ]; then
        MYSQL_PASSWORD=$(generate_password 16)
    fi
    if [ -z "$REDIS_PASSWORD" ]; then
        REDIS_PASSWORD=$(generate_password 16)
    fi
    if [ -z "$ADMIN_PASSWORD" ]; then
        ADMIN_PASSWORD=$(generate_password 12)
    fi
    if [ -z "$JWT_SECRET" ]; then
        JWT_SECRET=$(generate_secret)
    fi
    if [ -z "$LICENSE_MASTER_KEY" ]; then
        LICENSE_MASTER_KEY=$(generate_secret)
    fi
    if [ -z "$DOWNLOAD_TOKEN_SECRET" ]; then
        DOWNLOAD_TOKEN_SECRET=$(generate_secret)
    fi
    if [ -z "$CLIENT_ACCESS_TOKEN_SECRET" ]; then
        CLIENT_ACCESS_TOKEN_SECRET=$(generate_secret)
    fi

    create_directories
    create_env_file
    create_docker_config
    update_frontend_config
    generate_ssl_cert

    start_services
    init_admin
    install_nginx_proxy
    configure_firewall
    save_credentials
    print_completion
}

main "$@"
