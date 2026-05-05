#!/bin/bash
# ============================================
# License Server 一键安装脚本（Bootstrap）
# ============================================
# 作用：
#   - 拉取仓库并进入目录
#   - 调用核心安装脚本 scripts/install-core.sh
# ============================================

set -e

REPO_URL_DEFAULT="https://github.com/longxingze0925/license-server-ai.git"
REPO_BRANCH_DEFAULT="main"
INSTANCE_NAME_DEFAULT="${LS_INSTANCE:-license-server-ai}"
INSTALL_DIR_DEFAULT="/opt/${INSTANCE_NAME_DEFAULT}"

REPO_URL="${LS_REPO:-$REPO_URL_DEFAULT}"
REPO_BRANCH="${LS_BRANCH:-$REPO_BRANCH_DEFAULT}"
INSTALL_DIR="${LS_DIR:-$INSTALL_DIR_DEFAULT}"
INSTANCE_NAME="${LS_INSTANCE:-$INSTANCE_NAME_DEFAULT}"
INSTALL_DIR_EXPLICIT=false
[ -n "${LS_DIR:-}" ] && INSTALL_DIR_EXPLICIT=true

GIT_TOKEN="${LS_GIT_TOKEN:-${GIT_TOKEN:-}}"
USE_SOURCE=""
USE_SSH=false
NON_INTERACTIVE=false
SHOW_HELP=false

PASS_ARGS=()

log_info() { echo -e "[INFO] $1"; }
log_success() { echo -e "[SUCCESS] $1"; }
log_error() { echo -e "[ERROR] $1"; }
log_warning() { echo -e "[WARN] $1"; }

ensure_root() {
    if [ "$EUID" -eq 0 ]; then
        return 0
    fi
    if command -v sudo >/dev/null 2>&1; then
        log_info "检测到非 root 用户，尝试使用 sudo 重新执行..."
        exec sudo -E bash "$0" "$@"
    fi
    log_error "请使用 root 用户运行此脚本（或安装 sudo）"
    exit 1
}

usage() {
    cat <<'EOF'
用法:
  ./install.sh [选项]

Bootstrap 选项:
  --repo <url>        Git 仓库地址
  --branch <name>     分支或标签
  --instance <name>   部署实例名（默认: license-server-ai）
  --dir <path>        安装目录（默认: /opt/<instance>）
  --git-token <token> 私有仓库 Token（HTTPS）
  --ssh               使用 SSH 克隆
  --source            拉取源码（可本地构建）
  --no-source         不拉取源码（默认，仅下载必要文件）
  --uninstall         卸载当前实例（会进入卸载选项）
  --uninstall-mode <mode> 卸载模式: stop/remove/purge
  -y, --yes, --non-interactive  非交互模式
  -h, --help          显示帮助

说明:
- 其他安装参数会透传给 scripts/install-core.sh
- 私有仓库建议使用 SSH 或 Token
EOF
}

parse_args() {
    while [ $# -gt 0 ]; do
        case "$1" in
            --repo)
                REPO_URL="$2"; shift 2 ;;
            --branch)
                REPO_BRANCH="$2"; shift 2 ;;
            --instance)
                INSTANCE_NAME="$2"
                if [ "$INSTALL_DIR_EXPLICIT" = false ]; then
                    INSTALL_DIR="/opt/${INSTANCE_NAME}"
                fi
                PASS_ARGS+=("--instance" "$INSTANCE_NAME")
                shift 2 ;;
            --dir)
                INSTALL_DIR="$2"; INSTALL_DIR_EXPLICIT=true; shift 2 ;;
            --git-token)
                GIT_TOKEN="$2"; shift 2 ;;
            --ssh)
                USE_SSH=true; shift ;;
            --source)
                USE_SOURCE=true; shift ;;
            --no-source)
                USE_SOURCE=false; shift ;;
            --uninstall)
                PASS_ARGS+=("$1"); shift ;;
            --uninstall-mode)
                PASS_ARGS+=("$1" "$2"); shift 2 ;;
            -y|--yes|--non-interactive)
                NON_INTERACTIVE=true; PASS_ARGS+=("$1"); shift ;;
            -h|--help)
                SHOW_HELP=true; PASS_ARGS+=("$1"); shift ;;
            *)
                PASS_ARGS+=("$1"); shift ;;
        esac
    done
}

is_true() {
    case "$1" in
        1|true|TRUE|yes|YES|y|Y|on|ON) return 0 ;;
        *) return 1 ;;
    esac
}

has_arg() {
    local key="$1"
    for arg in "${PASS_ARGS[@]}"; do
        if [ "$arg" = "$key" ] || [[ "$arg" == "$key="* ]]; then
            return 0
        fi
    done
    return 1
}

append_arg_if_set() {
    local key="$1"
    local value="$2"
    if [ -n "$value" ] && ! has_arg "$key"; then
        PASS_ARGS+=("$key" "$value")
    fi
}

get_env_value_from_file() {
    local file="$1"
    local key="$2"
    if [ ! -f "$file" ]; then
        return 1
    fi
    grep -E "^${key}=" "$file" | tail -1 | cut -d= -f2- | tr -d '\r' | sed 's/^"//;s/"$//'
}

load_existing_instance_default() {
    if [ -n "${LS_INSTANCE:-}" ] || has_arg "--instance"; then
        return 0
    fi

    local value=""
    value=$(get_env_value_from_file ".env" "INSTANCE_NAME" || true)
    if [ -z "$value" ]; then
        value=$(get_env_value_from_file "${INSTALL_DIR}/.env" "INSTANCE_NAME" || true)
    fi

    if [ -n "$value" ]; then
        INSTANCE_NAME="$value"
        if [ "$INSTALL_DIR_EXPLICIT" = false ]; then
            INSTALL_DIR="/opt/${INSTANCE_NAME}"
        fi
    fi
}

apply_env_overrides() {
    local ssl_mode="${LS_SSL:-}"
    local domain="${LS_DOMAIN:-}"
    local email="${LS_EMAIL:-}"
    local server_ip="${LS_SERVER_IP:-}"
    local http_port="${LS_HTTP_PORT:-}"
    local https_port="${LS_HTTPS_PORT:-}"
    local backend_port="${LS_BACKEND_PORT:-}"
    local mysql_port="${LS_MYSQL_PORT:-}"
    local redis_port="${LS_REDIS_PORT:-}"
    local max_release_upload_mb="${LS_MAX_RELEASE_UPLOAD_MB:-}"
    local max_request_body_mb="${LS_MAX_REQUEST_BODY_MB:-}"
    local multipart_memory_mb="${LS_MULTIPART_MEMORY_MB:-}"
    local max_script_upload_mb="${LS_MAX_SCRIPT_UPLOAD_MB:-}"
    local max_secure_script_upload_mb="${LS_MAX_SECURE_SCRIPT_UPLOAD_MB:-}"
    local admin_email="${LS_ADMIN_EMAIL:-}"
    local admin_password="${LS_ADMIN_PASSWORD:-}"
    local cert_path="${LS_CERT:-}"
    local key_path="${LS_KEY:-}"
    local image_tag="${LS_IMAGE_TAG:-}"

    append_arg_if_set "--ssl" "$ssl_mode"
    append_arg_if_set "--domain" "$domain"
    append_arg_if_set "--email" "$email"
    append_arg_if_set "--server-ip" "$server_ip"
    append_arg_if_set "--instance" "${LS_INSTANCE:-}"
    append_arg_if_set "--http-port" "$http_port"
    append_arg_if_set "--https-port" "$https_port"
    append_arg_if_set "--backend-port" "$backend_port"
    append_arg_if_set "--mysql-port" "$mysql_port"
    append_arg_if_set "--redis-port" "$redis_port"
    append_arg_if_set "--max-release-upload-mb" "$max_release_upload_mb"
    append_arg_if_set "--max-request-body-mb" "$max_request_body_mb"
    append_arg_if_set "--multipart-memory-mb" "$multipart_memory_mb"
    append_arg_if_set "--max-script-upload-mb" "$max_script_upload_mb"
    append_arg_if_set "--max-secure-script-upload-mb" "$max_secure_script_upload_mb"
    append_arg_if_set "--admin-email" "$admin_email"
    append_arg_if_set "--admin-password" "$admin_password"
    append_arg_if_set "--cert" "$cert_path"
    append_arg_if_set "--key" "$key_path"
    append_arg_if_set "--image-tag" "$image_tag"

    if is_true "${LS_NGINX_PROXY:-}"; then
        if ! has_arg "--nginx-proxy"; then
            PASS_ARGS+=("--nginx-proxy")
        fi
    fi

    if is_true "${LS_BUILD:-}"; then
        if ! has_arg "--build" && ! has_arg "--no-build"; then
            PASS_ARGS+=("--build")
        fi
    fi

    if is_true "${LS_NON_INTERACTIVE:-}" || is_true "${LS_YES:-}"; then
        if ! has_arg "--non-interactive" && ! has_arg "-y" && ! has_arg "--yes"; then
            PASS_ARGS+=("--non-interactive")
            NON_INTERACTIVE=true
        fi
    fi

    if is_true "${LS_SSH:-}"; then
        USE_SSH=true
    fi

    if [ -n "${LS_SOURCE:-}" ]; then
        if is_true "${LS_SOURCE}"; then
            USE_SOURCE=true
        else
            USE_SOURCE=false
        fi
    fi

    if is_true "${LS_NO_SOURCE:-}"; then
        USE_SOURCE=false
    fi

    if is_true "${LS_UNINSTALL:-}"; then
        if ! has_arg "--uninstall"; then
            PASS_ARGS+=("--uninstall")
        fi
    fi

    if [ -n "${LS_UNINSTALL_MODE:-}" ]; then
        append_arg_if_set "--uninstall-mode" "$LS_UNINSTALL_MODE"
    fi
}

validate_instance_name() {
    local value="$1"
    if ! [[ "$value" =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]{1,62}$ ]]; then
        log_error "部署实例名只能包含字母、数字、点、下划线、短横线，长度 2-63，并且必须以字母或数字开头"
        exit 1
    fi
}

prompt_instance_name() {
    if [ "$NON_INTERACTIVE" = false ] && ! has_arg "--instance"; then
        echo ""
        read -p "部署实例名 [${INSTANCE_NAME}]: " input_instance
        INSTANCE_NAME=${input_instance:-$INSTANCE_NAME}
    fi

    validate_instance_name "$INSTANCE_NAME"

    if [ "$INSTALL_DIR_EXPLICIT" = false ]; then
        INSTALL_DIR="/opt/${INSTANCE_NAME}"
    fi

    append_arg_if_set "--instance" "$INSTANCE_NAME"
}

normalize_repo_url() {
    if [[ "$REPO_URL" != *.git ]]; then
        REPO_URL="${REPO_URL}.git"
    fi
}

https_to_ssh() {
    local url="$1"
    local host_path=${url#https://}
    echo "git@${host_path/\//:}"
}

maybe_pull_repo() {
    if [ -n "${LS_NO_PULL:-}" ]; then
        return 0
    fi
    if [ ! -d "$INSTALL_DIR/.git" ]; then
        return 0
    fi
    if ! command -v git >/dev/null 2>&1; then
        return 0
    fi
    if ! git -C "$INSTALL_DIR" diff --quiet 2>/dev/null || ! git -C "$INSTALL_DIR" diff --cached --quiet 2>/dev/null; then
        log_warning "检测到本地修改，跳过拉取最新脚本"
        return 0
    fi
    local branch
    branch=$(git -C "$INSTALL_DIR" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")
    if [ -z "$branch" ] || [ "$branch" = "HEAD" ]; then
        return 0
    fi
    log_info "检测到已有安装，尝试更新脚本..."
    git -C "$INSTALL_DIR" fetch origin >/dev/null 2>&1 || return 0
    if ! git -C "$INSTALL_DIR" pull --ff-only origin "$branch" >/dev/null 2>&1; then
        log_warning "更新脚本失败，继续使用本地版本"
    fi
}

resolve_source_choice() {
    if [ -n "$USE_SOURCE" ]; then
        return 0
    fi
    if [ "$NON_INTERACTIVE" = true ]; then
        USE_SOURCE=false
        return 0
    fi
    echo ""
    read -p "是否拉取源码（可本地构建）？[y/N]: " source_choice
    if is_true "$source_choice"; then
        USE_SOURCE=true
    else
        USE_SOURCE=false
    fi
}

get_github_repo_path() {
    local url="$REPO_URL"
    if [[ "$url" == git@github.com:* ]]; then
        url=${url#git@github.com:}
    elif [[ "$url" == https://github.com/* ]]; then
        url=${url#https://github.com/}
    else
        return 1
    fi
    url=${url%.git}
    echo "$url"
}

download_file() {
    local url="$1"
    local dest="$2"
    local dir
    dir=$(dirname "$dest")
    mkdir -p "$dir"

    local header=()
    if [ -n "$GIT_TOKEN" ]; then
        header=(-H "Authorization: token ${GIT_TOKEN}")
    fi

    if ! curl -fsSL "${header[@]}" "$url" -o "$dest"; then
        log_error "下载失败: $url"
        exit 1
    fi
}

download_required_files() {
    local repo_path
    repo_path=$(get_github_repo_path) || {
        log_error "仅支持 GitHub 仓库的无源码安装模式"
        exit 1
    }

    local raw_base="https://raw.githubusercontent.com/${repo_path}/${REPO_BRANCH}"

    log_info "下载必要文件..."
    download_file "${raw_base}/docker-compose.yml" "${INSTALL_DIR}/docker-compose.yml"
    download_file "${raw_base}/docker-compose.https.yml" "${INSTALL_DIR}/docker-compose.https.yml"
    download_file "${raw_base}/update.sh" "${INSTALL_DIR}/update.sh"
    download_file "${raw_base}/ssl-manager.sh" "${INSTALL_DIR}/ssl-manager.sh"
    download_file "${raw_base}/deploy/mysql/init.sql" "${INSTALL_DIR}/deploy/mysql/init.sql"
    download_file "${raw_base}/scripts/install-core.sh" "${INSTALL_DIR}/scripts/install-core.sh"

    chmod +x "${INSTALL_DIR}/update.sh" "${INSTALL_DIR}/ssl-manager.sh" "${INSTALL_DIR}/scripts/install-core.sh"
    log_success "必要文件下载完成"
}

refresh_install_files() {
    if [ -n "${LS_NO_PULL:-}" ]; then
        return 0
    fi

    if [ ! -d "$INSTALL_DIR" ]; then
        return 0
    fi

    if [ -d "$INSTALL_DIR/.git" ] && command -v git >/dev/null 2>&1; then
        if git -C "$INSTALL_DIR" diff --quiet 2>/dev/null && git -C "$INSTALL_DIR" diff --cached --quiet 2>/dev/null; then
            maybe_pull_repo
            return 0
        fi
        log_warning "检测到本地修改，改用远程 raw 文件刷新安装脚本"
    fi

    local repo_path
    repo_path=$(get_github_repo_path) || return 0
    local raw_base="https://raw.githubusercontent.com/${repo_path}/${REPO_BRANCH}"

    log_info "刷新安装脚本到最新版本..."
    download_file "${raw_base}/install.sh" "${INSTALL_DIR}/install.sh"
    download_file "${raw_base}/docker-compose.yml" "${INSTALL_DIR}/docker-compose.yml"
    download_file "${raw_base}/docker-compose.https.yml" "${INSTALL_DIR}/docker-compose.https.yml"
    download_file "${raw_base}/update.sh" "${INSTALL_DIR}/update.sh"
    download_file "${raw_base}/ssl-manager.sh" "${INSTALL_DIR}/ssl-manager.sh"
    download_file "${raw_base}/deploy/mysql/init.sql" "${INSTALL_DIR}/deploy/mysql/init.sql"
    download_file "${raw_base}/scripts/install-core.sh" "${INSTALL_DIR}/scripts/install-core.sh"

    chmod +x "${INSTALL_DIR}/install.sh" "${INSTALL_DIR}/update.sh" "${INSTALL_DIR}/ssl-manager.sh" "${INSTALL_DIR}/scripts/install-core.sh"
}

clone_repo() {
    local target_dir="$1"

    normalize_repo_url

    local clone_url="$REPO_URL"
    local clean_url="$REPO_URL"

    if [ "$USE_SSH" = true ]; then
        clone_url=$(https_to_ssh "$REPO_URL")
        clean_url="$clone_url"
    elif [ -n "$GIT_TOKEN" ]; then
        if [[ "$REPO_URL" == https://github.com/* ]]; then
            clone_url="https://x-access-token:${GIT_TOKEN}@${REPO_URL#https://}"
        fi
    fi

    mkdir -p "$target_dir"
    if [ -n "$(ls -A "$target_dir" 2>/dev/null)" ]; then
        log_error "安装目录非空: $target_dir"
        exit 1
    fi

    log_info "克隆仓库: $REPO_URL (branch: $REPO_BRANCH)"
    if ! git clone -b "$REPO_BRANCH" "$clone_url" "$target_dir"; then
        log_error "克隆失败"
        rm -rf "$target_dir" >/dev/null 2>&1 || true
        return 1
    fi

    if [ -n "$GIT_TOKEN" ] && [ "$USE_SSH" = false ]; then
        (cd "$target_dir" && git remote set-url origin "$clean_url")
    fi

    return 0
}

ensure_repo_dir_nosource() {
    if has_arg "--uninstall"; then
        if [ ! -d "$INSTALL_DIR" ]; then
            log_error "未找到实例安装目录: ${INSTALL_DIR}"
            log_error "请确认实例名是否正确，或使用 LS_INSTANCE/--instance 指定要卸载的实例"
            exit 1
        fi

        refresh_install_files

        cd "$INSTALL_DIR"
        return 0
    fi

    mkdir -p "$INSTALL_DIR"
    download_required_files
    cd "$INSTALL_DIR"
}

ensure_repo_dir() {
    if [ "$USE_SOURCE" = true ]; then
        if [ -f "scripts/install-core.sh" ] && [ -f "docker-compose.yml" ]; then
            refresh_install_files
            return 0
        fi

        if [ -d "$INSTALL_DIR/.git" ]; then
            refresh_install_files
            cd "$INSTALL_DIR"
            return 0
        fi

        if clone_repo "$INSTALL_DIR"; then
            cd "$INSTALL_DIR"
            return 0
        fi

        if [ "$NON_INTERACTIVE" = true ]; then
            log_error "克隆失败。若为私有仓库，请使用 --git-token 或 --ssh"
            exit 1
        fi

        echo ""
        echo "克隆失败，可能是私有仓库或网络问题。请选择认证方式后重试:"
        echo "  1) HTTPS Token"
        echo "  2) SSH（需已配置 SSH Key）"
        read -p "请选择 [1]: " auth_choice
        auth_choice=${auth_choice:-1}
        if [ "$auth_choice" = "2" ]; then
            USE_SSH=true
        else
            read -p "请输入 GitHub Token: " GIT_TOKEN
            if [ -z "$GIT_TOKEN" ]; then
                log_error "Token 不能为空"
                exit 1
            fi
        fi

        if clone_repo "$INSTALL_DIR"; then
            cd "$INSTALL_DIR"
            return 0
        fi

        log_error "二次克隆失败，请检查网络或仓库权限"
        exit 1
    fi

    ensure_repo_dir_nosource
}

main() {
    ensure_root "$@"
    parse_args "$@"
    apply_env_overrides

    if [ "$SHOW_HELP" = true ]; then
        if [ -f "scripts/install-core.sh" ]; then
            exec bash scripts/install-core.sh --help
        fi
        usage
        exit 0
    fi

    load_existing_instance_default
    prompt_instance_name

    if has_arg "--uninstall"; then
        USE_SOURCE=false
    else
        resolve_source_choice
    fi

    if [ "$USE_SOURCE" = false ] && has_arg "--build"; then
        log_error "无源码模式不支持 --build，请使用 --source 或 LS_SOURCE=1"
        exit 1
    fi

    ensure_repo_dir

    if [ -n "$GIT_TOKEN" ]; then
        export GIT_TOKEN
    fi

    if [ "$USE_SOURCE" = false ]; then
        export LS_NO_SOURCE=1
    fi

    exec bash scripts/install-core.sh "${PASS_ARGS[@]}"
}

main "$@"
