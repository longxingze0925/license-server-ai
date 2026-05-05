#!/bin/bash
# ============================================
# License Server 自动更新脚本
# ============================================
# 功能：
#   - 拉取最新 Docker 镜像
#   - 平滑重启服务（零停机）
# ============================================
# 使用方法：
#   ./update.sh              # 更新到最新版本
#   ./update.sh v1.2.0       # 更新到指定镜像标签
#   ./update.sh --tag=v1.2.0 # 更新到指定镜像标签
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

# 配置
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# 确定使用的 compose 文件
SSL_MODE=""
if [ -f ".env" ]; then
    SSL_MODE=$(grep -E '^SSL_MODE=' .env 2>/dev/null | tail -1 | cut -d= -f2- | tr -d '"' | tr -d '\r')
fi

if [ "$SSL_MODE" != "http" ] && [ -f "docker-compose.https.yml" ] && [ -f "certs/ssl/server.crt" ]; then
    COMPOSE_FILE="docker-compose.https.yml"
else
    COMPOSE_FILE="docker-compose.yml"
fi

# 尝试从后端配置读取上传限制，确保前端 Nginx 与后端一致
sync_upload_limit_from_config() {
    if [ ! -f "config.docker.yaml" ]; then
        return 0
    fi

    local limit_mb
    limit_mb=$(awk -F: '/^[[:space:]]*max_release_upload_mb[[:space:]]*:/ {gsub(/[[:space:]]/, "", $2); print $2; exit}' config.docker.yaml)
    if [[ "$limit_mb" =~ ^[0-9]+$ ]] && [ "$limit_mb" -gt 0 ]; then
        export MAX_RELEASE_UPLOAD_MB="$limit_mb"
        log_info "已同步上传限制: ${MAX_RELEASE_UPLOAD_MB}MB"
    fi
}

compose_cmd() {
    if [ -n "$IMAGE_TAG_OVERRIDE" ]; then
        IMAGE_TAG="$IMAGE_TAG_OVERRIDE" docker compose -f "$COMPOSE_FILE" "$@"
    else
        docker compose -f "$COMPOSE_FILE" "$@"
    fi
}

ensure_license_master_key() {
    local key="${LICENSE_MASTER_KEY:-}"
    if [ -z "$key" ] && [ -f ".env" ]; then
        key=$(grep -E '^LICENSE_MASTER_KEY=' .env 2>/dev/null | tail -1 | cut -d= -f2- | tr -d '"' | tr -d '\r')
    fi
    if [ -z "$key" ]; then
        log_error "缺少 LICENSE_MASTER_KEY。请先运行 ./deploy.sh 完成首次部署，或在 .env 中恢复首次部署生成的值"
        exit 1
    fi
}

# 解析参数
IMAGE_TAG_OVERRIDE=""
for arg in "$@"; do
    case $arg in
        --tag=*)
            IMAGE_TAG_OVERRIDE="${arg#*=}"
            ;;
        v*)
            IMAGE_TAG_OVERRIDE="$arg"
            ;;
    esac
done

get_env_value() {
    local key="$1"
    if [ ! -f ".env" ]; then
        return 1
    fi
    grep -E "^${key}=" .env | tail -1 | cut -d= -f2- | tr -d '"' | tr -d '\r'
}

get_frontend_status_url() {
    local server_ip
    local domain
    local http_port
    local https_port
    server_ip=$(get_env_value "SERVER_IP" || echo "127.0.0.1")
    domain=$(get_env_value "DOMAIN" || echo "")
    http_port=$(get_env_value "HTTP_PORT" || echo "80")
    https_port=$(get_env_value "HTTPS_PORT" || echo "443")

    local host_name="${domain:-$server_ip}"
    if [ "$SSL_MODE" = "http" ]; then
        if [ "$http_port" = "80" ]; then
            echo "http://${host_name}/"
        else
            echo "http://${host_name}:${http_port}/"
        fi
        return 0
    fi

    if [ "$https_port" = "443" ]; then
        echo "https://${host_name}/"
    else
        echo "https://${host_name}:${https_port}/"
    fi
}

# 显示当前版本
show_current_version() {
    if [ -f ".env" ]; then
        local env_tag
        env_tag=$(grep -E '^IMAGE_TAG=' .env 2>/dev/null | tail -1 | cut -d= -f2- | tr -d '"')
        if [ -n "$env_tag" ]; then
            echo "当前镜像标签: $env_tag"
        fi
    fi
    if [ -n "$IMAGE_TAG_OVERRIDE" ]; then
        echo "本次目标标签: $IMAGE_TAG_OVERRIDE"
    fi
    if [ -d ".git" ] && command -v git >/dev/null 2>&1; then
        echo "当前分支: $(git branch --show-current 2>/dev/null || echo 'N/A')"
        echo "最后提交: $(git log -1 --format='%h %s' 2>/dev/null || echo 'N/A')"
    fi
}

# 备份当前版本
backup_current() {
    log_info "备份当前配置..."

    BACKUP_DIR="backups/$(date +%Y%m%d_%H%M%S)"
    mkdir -p "$BACKUP_DIR"

    # 备份配置文件
    [ -f ".env" ] && cp .env "$BACKUP_DIR/"
    [ -f "config.docker.yaml" ] && cp config.docker.yaml "$BACKUP_DIR/"

    log_success "备份完成: $BACKUP_DIR"
}

# 拉取最新镜像
pull_images() {
    log_info "拉取最新 Docker 镜像..."
    compose_cmd pull
    log_success "镜像拉取完成"
}

# 平滑重启服务
restart_services() {
	log_info "重启服务（零停机）..."

	# 使用 rolling update 策略
	# 先启动新容器，等待健康检查通过后再停止旧容器

	# 显式执行数据库迁移，避免后端启动时隐式改库
	log_info "执行数据库迁移..."
	compose_cmd run --rm migrate

	# 重启后端
	compose_cmd up -d --no-deps backend

    # 等待后端健康
    log_info "等待后端服务就绪..."
    sleep 10

    # 检查后端健康状态
    for i in {1..30}; do
        if compose_cmd exec -T backend wget --no-verbose --tries=1 --spider http://localhost:8080/health 2>/dev/null; then
            log_success "后端服务就绪"
            break
        fi
        sleep 2
    done

    # 重启前端
    compose_cmd up -d --no-deps frontend

    log_info "等待前端服务就绪..."
    local frontend_health_url="http://127.0.0.1:80/"
    if [ "$COMPOSE_FILE" = "docker-compose.https.yml" ]; then
        frontend_health_url="https://127.0.0.1:443/"
    fi
    for i in {1..30}; do
        if compose_cmd exec -T frontend wget --no-verbose --tries=1 --spider --no-check-certificate "$frontend_health_url" 2>/dev/null; then
            log_success "前端服务就绪"
            break
        fi
        sleep 2
    done

    log_success "服务重启完成"
}

# 检查服务状态
check_status() {
    log_info "检查服务状态..."

    compose_cmd ps

    echo ""
    log_info "健康检查..."

    # 检查后端
    if curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/health | grep -q "200"; then
        log_success "后端服务: 正常"
    else
        log_error "后端服务: 异常"
    fi

    # 检查前端
    local frontend_url
    frontend_url=$(get_frontend_status_url)
    if curl -k -s -o /dev/null -w "%{http_code}" "$frontend_url" | grep -q "200\|301\|302"; then
        log_success "前端服务: 正常"
    else
        log_error "前端服务: 异常 (${frontend_url})"
    fi
}

# 回滚到上一版本
rollback() {
    log_warning "镜像回滚需要指定版本标签，例如: ./update.sh v1.2.0"
    exit 1
}

# 清理旧镜像
cleanup() {
    log_info "清理旧镜像..."

    docker image prune -f
    docker builder prune -f

    log_success "清理完成"
}

# 主函数
main() {
    echo ""
    echo "=========================================="
    echo "      License Server 自动更新"
    echo "=========================================="
    echo ""

    sync_upload_limit_from_config
    ensure_license_master_key
    show_current_version
    echo ""

    # 备份
    backup_current

    # 拉取镜像
    pull_images

    # 重启服务
    restart_services

    # 检查状态
    check_status

    # 清理
    cleanup

    echo ""
    log_success "更新完成！"
    echo ""
    show_current_version
}

# 处理命令
case "${1:-update}" in
    update|"")
        main
        ;;
    rollback)
        rollback
        ;;
    status)
        check_status
        ;;
    cleanup)
        cleanup
        ;;
    *)
        main
        ;;
esac
