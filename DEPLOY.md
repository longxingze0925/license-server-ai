# License Server 部署指南

## 目录

1. [快速开始](#快速开始)
2. [系统要求](#系统要求)
3. [配置说明](#配置说明)
4. [一键安装](#一键安装)
5. [HTTPS 配置](#https-配置)
6. [手动部署](#手动部署)
7. [常用命令](#常用命令)
8. [故障排除](#故障排除)

---

## 快速开始

### 最快方式（HTTPS 一键安装，推荐）

**方式一：远程一键（私有仓库，推荐）**

```bash
export GIT_TOKEN=YOUR_TOKEN
curl -H "Authorization: token $GIT_TOKEN" -fsSL \
  https://raw.githubusercontent.com/longxingze0925/license-server-ai/main/install.sh | \
  bash -s -- --repo https://github.com/longxingze0925/license-server-ai.git \
  --branch main --git-token "$GIT_TOKEN" \
  --ssl letsencrypt --domain example.com --email admin@example.com -y
```

**方式二：服务器本地执行（已克隆仓库）**

```bash
git clone https://github.com/longxingze0925/license-server-ai.git /opt/license-server
cd /opt/license-server
chmod +x install.sh
sudo ./install.sh
```

### HTTP 安装（不推荐用于生产）

```bash
export GIT_TOKEN=YOUR_TOKEN
curl -H "Authorization: token $GIT_TOKEN" -fsSL \
  https://raw.githubusercontent.com/longxingze0925/license-server-ai/main/install.sh | \
  bash -s -- --repo https://github.com/longxingze0925/license-server-ai.git \
  --branch main --git-token "$GIT_TOKEN" \
  --ssl http -y
```

安装脚本会自动：
- 安装 Docker 和 Docker Compose
- 生成安全的随机密码和密钥
- **配置 SSL 证书（自签名 / Let's Encrypt / 自定义证书）**
- 配置所有服务
- 初始化管理员账号
- 保存所有凭据到 `credentials.txt`

---

## 系统要求

| 项目 | 最低要求 | 推荐配置 |
|------|----------|----------|
| CPU | 1 核 | 2 核+ |
| 内存 | 1 GB | 2 GB+ |
| 磁盘 | 10 GB | 20 GB+ |
| 系统 | Ubuntu 20.04+ / Debian 11+ | Ubuntu 22.04 / Debian 12 |

### 端口要求

| 端口 | 用途 |
|------|------|
| 80 | HTTP（可选） |
| 443 | HTTPS（推荐） |
| 8080 | 后端 API（可选暴露）|
| 3306 | MySQL（仅内部访问）|
| 6379 | Redis（仅内部访问）|

---

## 配置说明

### 需要配置的项目

| 配置项 | 说明 | 必填 | 示例 |
|--------|------|------|------|
| `SERVER_IP` | 服务器公网 IP | ✅ | `123.45.67.89` |
| `MYSQL_ROOT_PASSWORD` | MySQL root 密码 | ✅ | 自动生成 |
| `MYSQL_PASSWORD` | 应用数据库密码 | ✅ | 自动生成 |
| `REDIS_PASSWORD` | Redis 密码 | ✅ | 自动生成 |
| `JWT_SECRET` | JWT 签名密钥（≥32字符）| ✅ | 自动生成 |
| `LICENSE_MASTER_KEY` | Provider 凭证加密主密钥（base64 32字节）| ✅ | 自动生成 |
| `ADMIN_EMAIL` | 管理员邮箱 | ✅ | `admin@example.com` |
| `ADMIN_PASSWORD` | 管理员初始密码 | ✅ | 自动生成 |
| `HTTP_PORT` | HTTP 端口 | 可选 | `80` |
| `HTTPS_PORT` | HTTPS 端口 | 可选 | `443` |
| `BACKEND_PORT` | 后端端口 | 可选 | `8080` |

### 配置文件列表

```
├── .env                    # 环境变量（从 .env.example 复制）
├── config.docker.yaml      # 后端配置（自动生成）
├── docker-compose.yml      # Docker 编排配置
├── deploy/
│   ├── Dockerfile.backend  # 后端 Docker 镜像
│   ├── Dockerfile.frontend # 前端 Docker 镜像
│   ├── nginx/
│   │   └── default.conf    # Nginx 配置
│   └── mysql/
│       └── init.sql        # 数据库初始化
```

---

## 一键安装

### 交互式安装（推荐）

```bash
chmod +x install.sh
sudo ./install.sh
```

### 非交互安装（示例）

**Let's Encrypt（域名）**

```bash
export GIT_TOKEN=YOUR_TOKEN
curl -H "Authorization: token $GIT_TOKEN" -fsSL \
  https://raw.githubusercontent.com/longxingze0925/license-server-ai/main/install.sh | \
  bash -s -- --repo https://github.com/longxingze0925/license-server-ai.git \
  --branch main --git-token "$GIT_TOKEN" \
  --ssl letsencrypt --domain example.com --email admin@example.com -y
```

**自定义证书（已购买证书）**

```bash
export GIT_TOKEN=YOUR_TOKEN
curl -H "Authorization: token $GIT_TOKEN" -fsSL \
  https://raw.githubusercontent.com/longxingze0925/license-server-ai/main/install.sh | \
  bash -s -- --repo https://github.com/longxingze0925/license-server-ai.git \
  --branch main --git-token "$GIT_TOKEN" \
  --ssl custom --cert /path/to/fullchain.crt --key /path/to/private.key -y
```

**仅 HTTP（不推荐）**

```bash
export GIT_TOKEN=YOUR_TOKEN
curl -H "Authorization: token $GIT_TOKEN" -fsSL \
  https://raw.githubusercontent.com/longxingze0925/license-server-ai/main/install.sh | \
  bash -s -- --repo https://github.com/longxingze0925/license-server-ai.git \
  --branch main --git-token "$GIT_TOKEN" \
  --ssl http -y
```

所有密码和密钥将自动生成并保存到 `credentials.txt`。

### 静默安装

如果已配置好 `.env` 文件：

```bash
chmod +x deploy.sh
./deploy.sh
```

---

## HTTPS 配置

### 方案对比

| 方案 | 适用场景 | 优点 | 缺点 |
|------|----------|------|------|
| **自签名证书** | IP 地址部署 | 无需域名 | 浏览器显示警告 |
| **Let's Encrypt** | 域名部署 | 免费、自动续期 | 需要域名 |
| **商业证书** | 企业生产 | 最高信任度 | 需付费 |

### 方案一：自签名证书（IP 地址部署）

适用于没有域名的情况（如内网、测试环境）。

```bash
# 使用 SSL 管理脚本生成
chmod +x ssl-manager.sh
./ssl-manager.sh self-signed 你的服务器IP

# 或使用一键安装时选择自签名
./install.sh
# 选择 1) 自签名证书
```

**注意**：自签名证书会导致浏览器显示安全警告，点击「高级」->「继续访问」即可。

### 方案二：Let's Encrypt（域名部署，推荐）

适用于有域名的生产环境，免费且自动续期。

**前提条件**：
1. 拥有一个域名
2. 域名已解析到服务器 IP
3. 服务器 80 端口可访问

```bash
# 使用 SSL 管理脚本申请
./ssl-manager.sh letsencrypt your-domain.com admin@your-domain.com

# 或使用一键安装时选择 Let's Encrypt
./install.sh
# 选择 2) Let's Encrypt 证书
```

### 方案三：使用已有证书

如果你已有商业 SSL 证书：

```bash
# 创建证书目录
mkdir -p certs/ssl

# 复制证书文件
cp your-cert.crt certs/ssl/server.crt
cp your-key.key certs/ssl/server.key

# 设置权限
chmod 644 certs/ssl/server.crt
chmod 600 certs/ssl/server.key

# 启动 HTTPS 服务
docker compose -f docker-compose.https.yml up -d
```

或使用一键安装的 **custom** 模式（推荐）：

```bash
./install.sh --ssl custom --cert /path/to/fullchain.crt --key /path/to/private.key
```

### SSL 证书管理

```bash
# 查看证书状态
./ssl-manager.sh status

# 手动续期
./ssl-manager.sh renew

# 生成新的自签名证书
./ssl-manager.sh self-signed 新IP地址
```

### 证书自动续期

Let's Encrypt 证书有效期为 90 天，安装脚本会自动配置每日检查续期任务。

手动设置自动续期：
```bash
# 添加 cron 任务
echo "0 2 * * * root /opt/license-server/ssl-manager.sh renew" > /etc/cron.d/certbot-renew
```

---

## 手动部署

### 1. 准备配置文件

推荐直接运行：

```bash
./deploy.sh
```

如果当前目录没有 `.env`，脚本会基于 `.env.example` 创建 `.env`，并随机生成数据库密码、JWT 密钥、客户端 token 密钥、`LICENSE_MASTER_KEY` 和初始管理员密码。

如果 `.env` 已存在，脚本会复用里面的值，不会覆盖 `LICENSE_MASTER_KEY`。这个值用于解密数据库里的 Provider API Key，换掉后旧 Provider 凭证将无法解密。

需要完全手动部署时再执行下面步骤。

```bash
# 复制环境变量模板
cp .env.example .env

# 编辑配置
nano .env
```

### 2. 修改必要配置

```env
# 必须准备的配置
SERVER_IP=你的服务器IP
MYSQL_ROOT_PASSWORD=你的MySQL密码
MYSQL_PASSWORD=你的应用密码
REDIS_PASSWORD=你的Redis密码
JWT_SECRET=至少32位的随机字符串
LICENSE_MASTER_KEY=base64编码的32字节随机主密钥
```

`LICENSE_MASTER_KEY` 只在第一次部署时随机生成。后续更新、重启、迁移同一个数据库时，都必须复用原来的值。

生成随机密钥：
```bash
# 生成 JWT Secret
openssl rand -base64 32

# 生成 LICENSE_MASTER_KEY
openssl rand -base64 32

# 生成密码
openssl rand -base64 16
```

### 3. 生成后端配置

```bash
# 使用模板生成配置
envsubst < config.docker.yaml.template > config.docker.yaml
```

### 4. 构建并启动

```bash
# 构建镜像
docker compose build

# 启动服务
docker compose up -d

# 查看日志
docker compose logs -f
```

### 5. 初始化管理员

```bash
docker compose exec backend ./license-server -config /app/config.yaml -init-admin
```

---

## 常用命令

### 服务管理

```bash
# 查看服务状态
docker compose ps

# 查看日志
docker compose logs -f

# 查看特定服务日志
docker compose logs -f backend
docker compose logs -f frontend
docker compose logs -f mysql

# 重启所有服务
docker compose restart

# 重启单个服务
docker compose restart backend

# 停止服务
docker compose down

# 停止并删除数据（谨慎！）
docker compose down -v
```

### 更新部署

```bash
# 推荐：使用更新脚本（私有仓库需设置 Token 或 SSH）
export GIT_TOKEN=YOUR_TOKEN
./update.sh
```

### 数据库操作

```bash
# 进入 MySQL
docker compose exec mysql mysql -u root -p

# 备份数据库
docker compose exec mysql mysqldump -u root -p license_server > backup.sql

# 恢复数据库
docker compose exec -T mysql mysql -u root -p license_server < backup.sql
```

### 查看资源使用

```bash
# 查看容器资源
docker stats

# 查看磁盘使用
docker system df
```

---

## 故障排除

### 服务无法启动

```bash
# 查看详细日志
docker compose logs --tail=100

# 检查容器状态
docker compose ps -a
```

### 数据库连接失败

```bash
# 检查 MySQL 是否就绪
docker compose exec mysql mysqladmin ping -h localhost -u root -p

# 检查网络
docker network ls
docker network inspect license-network
```

### 前端无法访问

1. 检查防火墙：
```bash
# Ubuntu
sudo ufw status
sudo ufw allow 80/tcp

# CentOS
sudo firewall-cmd --list-all
sudo firewall-cmd --permanent --add-port=80/tcp
sudo firewall-cmd --reload
```

2. 检查 Nginx 日志：
```bash
docker compose logs frontend
```

### 健康检查失败

```bash
# 手动测试健康检查
curl http://localhost:8080/api/health
curl http://localhost:80/
```

### 重置管理员密码

```bash
# 进入 MySQL
docker compose exec mysql mysql -u root -p license_server

# 重置密码（需要先计算 bcrypt 哈希）
UPDATE team_members SET password_hash='新的哈希' WHERE email='admin@example.com';
```

---

## 安全建议

1. **首次登录后立即修改默认密码**
2. **妥善保管 credentials.txt 文件**
3. **定期备份数据库**
4. **启用 HTTPS（推荐使用 Let's Encrypt）**
5. **限制数据库端口仅内部访问**
6. **定期更新 Docker 镜像**

---

## 技术支持

如遇问题，请检查：
1. Docker 日志：`docker compose logs`
2. 系统日志：`journalctl -u docker`
3. 磁盘空间：`df -h`
4. 内存使用：`free -m`
