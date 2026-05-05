# License Server - 授权管理平台

多应用授权管理平台，支持用户管理、组织管理、授权码管理、设备绑定、脚本下发等功能。

## 功能特性

- **用户管理**: 注册、登录、JWT 认证
- **组织管理**: 多组织支持，成员角色管理
- **应用管理**: 多应用支持，独立 RSA 密钥对
- **授权管理**: 授权码生成、激活、续费、吊销、暂停
- **设备管理**: 设备绑定、数量限制、黑名单
- **心跳验证**: 定期验证，防止破解
- **脚本下发**: 加密脚本，按需下发
- **版本更新**: 软件版本管理，强制更新

## 技术栈

- Go 1.21+
- Gin (Web 框架)
- GORM (ORM)
- MySQL (数据库)
- JWT (认证)
- RSA (签名加密)

## 服务器一键安装（Docker，推荐）

> 公开仓库默认无需 Token。若使用私有仓库，请准备 GitHub Token 或配置 SSH Key。
> 默认从 GHCR 拉取镜像部署；如需本地构建，请在安装命令末尾加 `--build`。

### 超短一键命令（交互式）

```bash
bash <(curl -Ls https://raw.githubusercontent.com/longxingze0925/license-server-ai/main/install.sh)
```

> 无参数时进入交互模式，会先填写部署实例名，再引导你选择证书类型、域名、端口、管理员账号等。
> 部署实例名用于隔离安装目录、容器、数据卷和网络；脚本会先让你确认配置，确认后才开始安装依赖、Docker、拉镜像和启动服务。
> 过程中会询问是否拉取源码：默认不拉取源码，仅下载必要文件；如需源码可选择 `y` 或使用 `--source` / `LS_SOURCE=1`。
> 已有安装目录时会自动刷新安装脚本和 Compose 文件；只有设置 `LS_NO_PULL=1` 才会跳过刷新。

### HTTPS（Let's Encrypt，域名）

```bash
curl -fsSL https://raw.githubusercontent.com/longxingze0925/license-server-ai/main/install.sh | \
  bash -s -- --repo https://github.com/longxingze0925/license-server-ai.git \
  --branch main \
  --ssl letsencrypt --domain example.com --email admin@example.com -y
```

### HTTPS（自定义证书）

```bash
curl -fsSL https://raw.githubusercontent.com/longxingze0925/license-server-ai/main/install.sh | \
  bash -s -- --repo https://github.com/longxingze0925/license-server-ai.git \
  --branch main \
  --ssl custom --cert /path/to/fullchain.crt --key /path/to/private.key -y
```

> 如需指定镜像版本，可在安装时加 `--image-tag v1.2.0`，或安装后在 `.env` 中设置 `IMAGE_TAG`。

### 环境变量一键安装（非交互）

```bash
LS_SSL=letsencrypt LS_DOMAIN=example.com LS_EMAIL=admin@example.com \
LS_ADMIN_EMAIL=admin@example.com LS_ADMIN_PASSWORD='Admin@123456' \
bash <(curl -Ls https://raw.githubusercontent.com/longxingze0925/license-server-ai/main/install.sh)
```

域名部署时，如果 HTTPS 容器端口不是 `443`，脚本会自动启用系统 Nginx 反向代理：外部访问 `https://example.com`，Nginx 再转发到本机容器端口。也可以显式传入 `--nginx-proxy`。

源码安装示例：

```bash
LS_SOURCE=1 LS_SSL=letsencrypt LS_DOMAIN=example.com LS_EMAIL=admin@example.com \
LS_ADMIN_EMAIL=admin@example.com LS_ADMIN_PASSWORD='Admin@123456' \
bash <(curl -Ls https://raw.githubusercontent.com/longxingze0925/license-server-ai/main/install.sh)
```

自定义证书示例：

```bash
LS_SSL=custom LS_CERT=/path/to/fullchain.crt LS_KEY=/path/to/private.key \
LS_ADMIN_EMAIL=admin@example.com LS_ADMIN_PASSWORD='Admin@123456' \
bash <(curl -Ls https://raw.githubusercontent.com/longxingze0925/license-server-ai/main/install.sh)
```

### 仅拉取镜像的一键安装（非交互）

```bash
LS_SSL=letsencrypt LS_DOMAIN=example.com LS_EMAIL=admin@example.com \
LS_IMAGE_TAG=main \
bash <(curl -Ls https://raw.githubusercontent.com/longxingze0925/license-server-ai/main/install.sh)
```

> 若镜像是私有的，请先 `docker login ghcr.io`。

### 自定义端口安装

```bash
bash <(curl -Ls https://raw.githubusercontent.com/longxingze0925/license-server-ai/main/install.sh) \
  --instance license-server-ai-new \
  --ssl http \
  --http-port 8088 \
  --backend-port 18080 \
  --mysql-port 13306 \
  --redis-port 16379
```

也可以用环境变量：

```bash
LS_INSTANCE=license-server-ai-new \
LS_SSL=http LS_HTTP_PORT=8088 LS_BACKEND_PORT=18080 \
LS_MYSQL_PORT=13306 LS_REDIS_PORT=16379 \
bash <(curl -Ls https://raw.githubusercontent.com/longxingze0925/license-server-ai/main/install.sh)
```

### 更新（拉取镜像）

```bash
cd /opt/license-server-ai
./update.sh              # 拉取 main 镜像并更新
./update.sh v1.2.0       # 拉取指定标签并更新
# 或者在 .env 中设置 IMAGE_TAG=main / v1.2.0
```

> 如果你的 GHCR 镜像是私有的，需要先执行 `docker login ghcr.io`。
> `update.sh` 只拉取镜像并重启服务，不会更新脚本或配置；如需更新脚本或配置，请重新运行安装脚本（源码模式可 `git pull`）。
> 交互式安装里选择“更新到最新版本”，实际就是执行 `update.sh`，会保留原有数据和配置（不自动备份数据库）。

### 重新安装时的数据库选项

当选择“重新安装（覆盖配置）”时，会提示数据库处理方式：

1) **保留数据库（推荐）**：保留数据卷并复用旧密码  
2) **重置数据库（清空数据，保留旧密码）**  
3) **重置数据库（清空数据，重新生成密码）**

非交互可用：

```bash
LS_REINSTALL_DB=keep      # 保留数据库（默认）
LS_REINSTALL_DB=reset
LS_REINSTALL_DB=reset-new
```

### 卸载当前实例

重新执行一键安装命令，检测到已有安装后选择“卸载当前实例”：

```bash
bash <(curl -Ls https://raw.githubusercontent.com/longxingze0925/license-server-ai/main/install.sh)
```

卸载模式：

| 模式 | 说明 | 是否删除数据库数据 |
|------|------|------------------|
| `stop` | 只停止服务，保留容器、数据和配置 | 否 |
| `remove` | 删除容器和网络，保留数据卷、配置和 `credentials.txt` | 否 |
| `purge` | 删除容器、数据卷和安装目录，需输入实例名确认 | 是 |

非交互示例：

```bash
LS_INSTANCE=license-server-ai \
bash <(curl -Ls https://raw.githubusercontent.com/longxingze0925/license-server-ai/main/install.sh) \
  --uninstall --uninstall-mode remove -y
```

## 快速开始

### 1. 配置数据库

创建 MySQL 数据库：

```sql
CREATE DATABASE license_server CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

### 2. 修改配置

编辑 `config.yaml`，配置数据库连接信息：

```yaml
database:
  host: "localhost"
  port: 3306
  username: "root"
  password: "your_password"
  database: "license_server"
```

### 3. 数据库迁移

```bash
go run ./cmd/main.go -migrate
```

### 4. 启动服务

```bash
go run ./cmd/main.go
```

服务将在 `http://localhost:8080` 启动。

## API 接口

### 公开接口

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /api/auth/register | 用户注册 |
| POST | /api/auth/login | 用户登录 |

### 客户端接口

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /api/client/auth/activate | 激活授权码 |
| POST | /api/client/auth/verify | 验证授权 |
| POST | /api/client/auth/heartbeat | 心跳 |
| POST | /api/client/auth/deactivate | 解绑设备 |
| GET | /api/client/scripts/version | 获取脚本版本 |
| GET | /api/client/scripts/:filename | 下载脚本 |
| GET | /api/client/releases/latest | 获取最新版本 |

### 管理接口（需认证）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /api/admin/apps | 创建应用 |
| GET | /api/admin/apps | 获取应用列表 |
| GET | /api/admin/apps/:id | 获取应用详情 |
| PUT | /api/admin/apps/:id | 更新应用 |
| DELETE | /api/admin/apps/:id | 删除应用 |
| POST | /api/admin/licenses | 创建授权码 |
| GET | /api/admin/licenses | 获取授权列表 |
| POST | /api/admin/licenses/:id/renew | 续费 |
| POST | /api/admin/licenses/:id/revoke | 吊销 |
| POST | /api/admin/licenses/:id/suspend | 暂停 |
| POST | /api/admin/licenses/:id/resume | 恢复 |

## 客户端集成

### 激活授权码

```json
POST /api/client/auth/activate
{
  "app_key": "应用Key",
  "license_key": "XXXX-XXXX-XXXX-XXXX",
  "machine_id": "设备指纹",
  "device_info": {
    "name": "设备名称",
    "os": "Windows",
    "os_version": "10",
    "app_version": "1.0.0"
  }
}
```

### 验证授权

```json
POST /api/client/auth/verify
{
  "app_key": "应用Key",
  "machine_id": "设备指纹"
}
```

### 心跳

```json
POST /api/client/auth/heartbeat
{
  "app_key": "应用Key",
  "machine_id": "设备指纹",
  "app_version": "1.0.0"
}
```

## 目录结构

```
license-server/
├── cmd/
│   └── main.go              # 入口文件
├── internal/
│   ├── config/              # 配置管理
│   ├── handler/             # HTTP 处理器
│   ├── middleware/          # 中间件
│   ├── model/               # 数据模型
│   └── pkg/                 # 工具包
│       ├── crypto/          # 加密工具
│       ├── response/        # 响应封装
│       └── utils/           # 通用工具
├── storage/
│   ├── scripts/             # 脚本存储
│   └── releases/            # 发布包存储
├── config.yaml              # 配置文件
└── README.md
```

## 安全说明

1. **RSA 签名**: 每个应用独立密钥对，响应数据签名验证
2. **设备绑定**: 硬件指纹绑定，限制设备数量
3. **心跳验证**: 定期验证，防止离线破解
4. **脚本保护**: 脚本加密存储，授权验证后下发
