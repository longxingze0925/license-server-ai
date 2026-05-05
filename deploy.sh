#!/bin/bash
# ============================================
# License Server 快速部署脚本（非交互式）
# ============================================
# 适用于自动化部署，使用环境变量或 .env 文件配置
# 使用方法：
#   ./deploy.sh
#   首次运行如果 .env 不存在，会基于 .env.example 创建并随机生成密钥。
#   后续运行只复用已有 .env，不会覆盖 LICENSE_MASTER_KEY。
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

sed_escape_replacement() {
    printf '%s' "$1" | sed -e 's/[\/&\\]/\\&/g'
}

replace_config_var() {
    local key="$1"
    local value
    value="$(sed_escape_replacement "$2")"
    sed -i "s/\${${key}}/${value}/g" config.docker.yaml
}

ensure_openssl() {
    if ! command -v openssl &> /dev/null; then
        log_error "缺少 openssl，无法生成首次部署密钥"
        log_info "请先安装 openssl，或手动创建 .env 后再运行部署"
        exit 1
    fi
}

generate_password() {
    local length=${1:-24}
    ensure_openssl
    openssl rand -base64 64 | tr -dc 'A-Za-z0-9' | head -c "$length"
}

generate_secret() {
    ensure_openssl
    openssl rand -base64 32
}

get_server_ip() {
    local ip=""
    if command -v curl &> /dev/null; then
        ip=$(curl -s --max-time 5 https://api.ipify.org 2>/dev/null || true)
    fi
    if [ -z "$ip" ] && command -v hostname &> /dev/null; then
        ip=$(hostname -I 2>/dev/null | awk '{print $1}' || true)
    fi
    echo "$ip"
}

load_env_file() {
    if [ ! -f .env ]; then
        return 0
    fi

    local line key value
    while IFS= read -r line || [ -n "$line" ]; do
        line="${line%$'\r'}"
        [[ "$line" =~ ^[[:space:]]*$ ]] && continue
        [[ "$line" =~ ^[[:space:]]*# ]] && continue
        if [[ "$line" =~ ^[[:space:]]*([A-Za-z_][A-Za-z0-9_]*)=(.*)$ ]]; then
            key="${BASH_REMATCH[1]}"
            value="${BASH_REMATCH[2]}"
            value="${value#"${value%%[![:space:]]*}"}"
            value="${value%"${value##*[![:space:]]}"}"
            if [[ "$value" == \"*\" && "$value" == *\" ]]; then
                value="${value:1:${#value}-2}"
            elif [[ "$value" == \'*\' && "$value" == *\' ]]; then
                value="${value:1:${#value}-2}"
            fi
            export "$key=$value"
        fi
    done < .env
}

set_env_value() {
    local key="$1"
    local value="$2"
    local escaped
    escaped="$(sed_escape_replacement "$value")"

    if grep -qE "^${key}=" .env 2>/dev/null; then
        sed -i "s/^${key}=.*/${key}=${escaped}/" .env
    else
        printf '\n%s=%s\n' "$key" "$value" >> .env
    fi

    export "$key=$value"
}

is_placeholder_or_empty() {
    local value="${1:-}"
    case "$value" in
        ""|YOUR_SERVER_IP|CHANGE_ME*|Admin@123456)
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

ensure_env_file() {
    if [ -f .env ]; then
        return 0
    fi

    if [ ! -f .env.example ]; then
        log_error ".env 不存在，且未找到 .env.example"
        exit 1
    fi

    cp .env.example .env
    chmod 600 .env 2>/dev/null || true
    log_info "未发现 .env，已基于 .env.example 创建"
}

ensure_first_deploy_secrets() {
    ensure_env_file
    load_env_file

    if is_placeholder_or_empty "${SERVER_IP:-}"; then
        local detected_ip
        detected_ip=$(get_server_ip)
        if [ -z "$detected_ip" ]; then
            log_error "无法自动获取服务器 IP，请在 .env 中设置 SERVER_IP"
            exit 1
        fi
        set_env_value "SERVER_IP" "$detected_ip"
    fi

    if is_placeholder_or_empty "${MYSQL_ROOT_PASSWORD:-}"; then
        set_env_value "MYSQL_ROOT_PASSWORD" "$(generate_password 24)"
    fi
    if is_placeholder_or_empty "${MYSQL_PASSWORD:-}"; then
        set_env_value "MYSQL_PASSWORD" "$(generate_password 24)"
    fi
    if is_placeholder_or_empty "${REDIS_PASSWORD:-}"; then
        set_env_value "REDIS_PASSWORD" "$(generate_password 24)"
    fi
    if is_placeholder_or_empty "${JWT_SECRET:-}"; then
        set_env_value "JWT_SECRET" "$(generate_secret)"
    fi
    if is_placeholder_or_empty "${DOWNLOAD_TOKEN_SECRET:-}"; then
        set_env_value "DOWNLOAD_TOKEN_SECRET" "$(generate_secret)"
    fi
    if is_placeholder_or_empty "${CLIENT_ACCESS_TOKEN_SECRET:-}"; then
        set_env_value "CLIENT_ACCESS_TOKEN_SECRET" "$(generate_secret)"
    fi
    if is_placeholder_or_empty "${LICENSE_MASTER_KEY:-}"; then
        set_env_value "LICENSE_MASTER_KEY" "$(generate_secret)"
        log_info "已生成 LICENSE_MASTER_KEY；后续部署会复用 .env 中的同一个值"
    fi
    if is_placeholder_or_empty "${ADMIN_PASSWORD:-}"; then
        set_env_value "ADMIN_PASSWORD" "$(generate_password 16)"
        GENERATED_ADMIN_PASSWORD="$ADMIN_PASSWORD"
    fi
    if [ -z "${ADMIN_EMAIL:-}" ]; then
        set_env_value "ADMIN_EMAIL" "admin@example.com"
    fi

    chmod 600 .env 2>/dev/null || true
}

validate_base64_32() {
    local name="$1"
    local value="$2"
    local decoded_len
    decoded_len=$(printf '%s' "$value" | base64 -d 2>/dev/null | wc -c | tr -d ' ')
    if [ "$decoded_len" != "32" ]; then
        log_error "${name} 必须是 base64 编码的 32 字节随机值"
        log_info "生成命令: openssl rand -base64 32"
        exit 1
    fi
}

GENERATED_ADMIN_PASSWORD=""
ensure_first_deploy_secrets

# 上传限制默认值
: "${MAX_RELEASE_UPLOAD_MB:=500}"
: "${MAX_REQUEST_BODY_MB:=1024}"
: "${MULTIPART_MEMORY_MB:=32}"
: "${MAX_SCRIPT_UPLOAD_MB:=20}"
: "${MAX_SECURE_SCRIPT_UPLOAD_MB:=20}"
: "${DOWNLOAD_TOKEN_SECRET:=}"
: "${CLIENT_ACCESS_TOKEN_SECRET:=}"
: "${ADMIN_EMAIL:=admin@example.com}"
export MAX_RELEASE_UPLOAD_MB MAX_REQUEST_BODY_MB MULTIPART_MEMORY_MB
export MAX_SCRIPT_UPLOAD_MB MAX_SECURE_SCRIPT_UPLOAD_MB
export DOWNLOAD_TOKEN_SECRET CLIENT_ACCESS_TOKEN_SECRET ADMIN_EMAIL
export MYSQL_ROOT_PASSWORD MYSQL_PASSWORD REDIS_PASSWORD JWT_SECRET LICENSE_MASTER_KEY SERVER_IP
export FRONTEND_PORT BACKEND_PORT ADMIN_PASSWORD

# 检查必要配置
if [ -z "$MYSQL_PASSWORD" ] || [ -z "$MYSQL_ROOT_PASSWORD" ] || [ -z "$REDIS_PASSWORD" ]; then
    log_error ".env 中缺少 MySQL 或 Redis 密码"
    exit 1
fi

if [ -z "$JWT_SECRET" ] || [ ${#JWT_SECRET} -lt 32 ]; then
    log_error "JWT_SECRET 必须至少 32 个字符"
    exit 1
fi

if [ -z "$DOWNLOAD_TOKEN_SECRET" ] || [ ${#DOWNLOAD_TOKEN_SECRET} -lt 32 ] || [ -z "$CLIENT_ACCESS_TOKEN_SECRET" ] || [ ${#CLIENT_ACCESS_TOKEN_SECRET} -lt 32 ]; then
    log_error "DOWNLOAD_TOKEN_SECRET 和 CLIENT_ACCESS_TOKEN_SECRET 必须至少 32 个字符"
    exit 1
fi

validate_base64_32 "LICENSE_MASTER_KEY" "$LICENSE_MASTER_KEY"

log_info "检查 Docker 环境..."
if ! command -v docker &> /dev/null; then
    log_error "Docker 未安装，请先安装 Docker"
    exit 1
fi

if ! docker compose version &> /dev/null; then
    log_error "Docker Compose 未安装"
    exit 1
fi

log_info "创建必要目录..."
mkdir -p storage/scripts storage/releases logs certs

log_info "生成 Docker 配置文件..."
# 使用 envsubst 替换变量
envsubst < config.docker.yaml.template > config.docker.yaml 2>/dev/null || {
    # 如果 envsubst 不可用，使用 sed
    cp config.docker.yaml.template config.docker.yaml
    replace_config_var "MYSQL_PASSWORD" "$MYSQL_PASSWORD"
    replace_config_var "REDIS_PASSWORD" "$REDIS_PASSWORD"
    replace_config_var "JWT_SECRET" "$JWT_SECRET"
    replace_config_var "DOWNLOAD_TOKEN_SECRET" "$DOWNLOAD_TOKEN_SECRET"
    replace_config_var "CLIENT_ACCESS_TOKEN_SECRET" "$CLIENT_ACCESS_TOKEN_SECRET"
    replace_config_var "SERVER_IP" "$SERVER_IP"
    replace_config_var "FRONTEND_PORT" "${FRONTEND_PORT:-80}"
    replace_config_var "MAX_RELEASE_UPLOAD_MB" "$MAX_RELEASE_UPLOAD_MB"
    replace_config_var "MAX_REQUEST_BODY_MB" "$MAX_REQUEST_BODY_MB"
    replace_config_var "MULTIPART_MEMORY_MB" "$MULTIPART_MEMORY_MB"
    replace_config_var "MAX_SCRIPT_UPLOAD_MB" "$MAX_SCRIPT_UPLOAD_MB"
    replace_config_var "MAX_SECURE_SCRIPT_UPLOAD_MB" "$MAX_SECURE_SCRIPT_UPLOAD_MB"
}

log_info "构建镜像..."
docker compose build

log_info "执行数据库迁移..."
docker compose run --rm migrate

log_info "启动服务..."
docker compose up -d

log_info "等待服务启动..."
sleep 15

# 检查服务状态
if docker compose ps | grep -q "Up"; then
    log_success "所有服务已启动"
    echo ""
    echo -e "  ${BLUE}前端地址:${NC} http://${SERVER_IP}:${FRONTEND_PORT:-80}"
    echo -e "  ${BLUE}后端地址:${NC} http://${SERVER_IP}:${BACKEND_PORT:-8080}"
    if [ -n "$GENERATED_ADMIN_PASSWORD" ]; then
        echo -e "  ${BLUE}初始管理员:${NC} ${ADMIN_EMAIL:-admin@example.com}"
        echo -e "  ${BLUE}初始密码:${NC} ${GENERATED_ADMIN_PASSWORD}"
        log_warning "请保存初始管理员密码，并在首次登录后修改"
    fi
    echo ""
else
    log_error "服务启动失败，请检查日志: docker compose logs"
    exit 1
fi

# 初始化管理员（如果需要）
log_info "检查管理员账号..."
docker compose exec -T \
    -e INIT_ADMIN_EMAIL="$ADMIN_EMAIL" \
    -e INIT_ADMIN_PASSWORD="$ADMIN_PASSWORD" \
    backend ./license-server -config /app/config.yaml -init-admin 2>/dev/null || true

log_success "部署完成！"
