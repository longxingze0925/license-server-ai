#!/bin/bash
# ============================================
# License Server 快速部署脚本（非交互式）
# ============================================
# 适用于自动化部署，使用环境变量或 .env 文件配置
# 使用方法：
#   1. 复制 .env.example 为 .env 并修改
#   2. 运行: ./deploy.sh
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
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# 检查 .env 文件
if [ ! -f .env ]; then
    log_error ".env 文件不存在"
    log_info "请先复制 .env.example 为 .env 并修改配置"
    exit 1
fi

# 加载环境变量
source .env

# 上传限制默认值
: "${MAX_RELEASE_UPLOAD_MB:=500}"
: "${MAX_REQUEST_BODY_MB:=1024}"
: "${MULTIPART_MEMORY_MB:=32}"
: "${MAX_SCRIPT_UPLOAD_MB:=20}"
: "${MAX_SECURE_SCRIPT_UPLOAD_MB:=20}"

# 检查必要配置
if [ "$MYSQL_PASSWORD" = "CHANGE_ME_Db@2024!" ] || [ -z "$MYSQL_PASSWORD" ]; then
    log_error "请修改 .env 中的 MYSQL_PASSWORD"
    exit 1
fi

if [ "$JWT_SECRET" = "CHANGE_ME_JWT_SECRET_AT_LEAST_32_CHARS!" ] || [ -z "$JWT_SECRET" ]; then
    log_error "请修改 .env 中的 JWT_SECRET"
    exit 1
fi

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
    sed -i "s/\${MYSQL_PASSWORD}/${MYSQL_PASSWORD}/g" config.docker.yaml
    sed -i "s/\${REDIS_PASSWORD}/${REDIS_PASSWORD}/g" config.docker.yaml
    sed -i "s/\${JWT_SECRET}/${JWT_SECRET}/g" config.docker.yaml
    sed -i "s/\${SERVER_IP}/${SERVER_IP}/g" config.docker.yaml
    sed -i "s/\${FRONTEND_PORT}/${FRONTEND_PORT:-80}/g" config.docker.yaml
    sed -i "s/\${MAX_RELEASE_UPLOAD_MB}/${MAX_RELEASE_UPLOAD_MB}/g" config.docker.yaml
    sed -i "s/\${MAX_REQUEST_BODY_MB}/${MAX_REQUEST_BODY_MB}/g" config.docker.yaml
    sed -i "s/\${MULTIPART_MEMORY_MB}/${MULTIPART_MEMORY_MB}/g" config.docker.yaml
    sed -i "s/\${MAX_SCRIPT_UPLOAD_MB}/${MAX_SCRIPT_UPLOAD_MB}/g" config.docker.yaml
    sed -i "s/\${MAX_SECURE_SCRIPT_UPLOAD_MB}/${MAX_SECURE_SCRIPT_UPLOAD_MB}/g" config.docker.yaml
}

log_info "构建并启动服务..."
docker compose up -d --build

log_info "等待服务启动..."
sleep 15

# 检查服务状态
if docker compose ps | grep -q "Up"; then
    log_success "所有服务已启动"
    echo ""
    echo -e "  ${BLUE}前端地址:${NC} http://${SERVER_IP}:${FRONTEND_PORT:-80}"
    echo -e "  ${BLUE}后端地址:${NC} http://${SERVER_IP}:${BACKEND_PORT:-8080}"
    echo ""
else
    log_error "服务启动失败，请检查日志: docker compose logs"
    exit 1
fi

# 初始化管理员（如果需要）
log_info "检查管理员账号..."
docker compose exec -T backend ./license-server -config /app/config.yaml -init-admin 2>/dev/null || true

log_success "部署完成！"
