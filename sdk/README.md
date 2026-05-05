# 授权管理系统 SDK

客户端 SDK，支持 Python、Go 和 .NET/C#。.NET/C# 目前已封装后台登录、额度、AI Proxy、任务、文件，以及客户端侧的授权会话、热更新、WebSocket 实时指令、数据同步、普通脚本、安全脚本和版本下载接口。

## 功能特性

- 支持两种授权模式：授权码模式、账号密码模式
- 自动心跳保活
- 离线缓存支持（AES 加密）
- 版本更新检测
- **热更新支持**：增量/全量更新、自动回滚、灰度发布
- 功能权限控制
- **安全防护**：反调试、时间检测、完整性校验
- **证书固定（Certificate Pinning）**：防止中间人攻击

---

## 安全防护功能

SDK 提供了安全增强模块，防止常见的破解手段：

| 防护措施 | 说明 |
|---------|------|
| **证书固定** | 防止中间人攻击（MITM），验证服务器证书指纹 |
| 反调试检测 | 检测调试器、IDE 调试模式 |
| 时间回拨检测 | 防止修改系统时间延长授权 |
| 代码完整性校验 | 检测 SDK 文件是否被篡改 |
| 多点分散验证 | 验证逻辑分散，增加破解难度 |
| 环境检测 | 检测虚拟机、沙箱环境 |
| 缓存加密 | 本地缓存使用 AES-256 加密 |

---

## 证书固定（Certificate Pinning）

证书固定是防止中间人攻击的关键安全措施。当使用 IP 地址 + 自签名证书部署时，**强烈建议启用证书固定**。

### 获取服务器证书指纹

首先需要获取服务器的证书指纹：

**Python:**
```python
from license_client import get_server_certificate_fingerprint

# 获取服务器证书指纹
fingerprint = get_server_certificate_fingerprint("192.168.1.100", 8080)
print(f"服务器证书指纹: {fingerprint}")
# 输出: SHA256:AB:CD:EF:12:34:56:78:90:...
```

**Go:**
```go
import "your_project/license"

fingerprint, err := license.GetServerCertificateFingerprint("192.168.1.100", 8080)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("服务器证书指纹: %s\n", fingerprint)
```

**命令行（OpenSSL）:**
```bash
# 获取证书指纹
openssl s_client -connect 192.168.1.100:8080 < /dev/null 2>/dev/null | \
  openssl x509 -fingerprint -sha256 -noout
```

### 配置证书固定

有三种方式配置证书固定：

#### 方式1：使用证书指纹（推荐）

**Python:**
```python
from license_client import LicenseClient

client = LicenseClient(
    server_url="https://192.168.1.100:8080",
    app_key="your_app_key",
    cert_fingerprint="SHA256:AB:CD:EF:12:34:56:78:90:AB:CD:EF:12:34:56:78:90:AB:CD:EF:12:34:56:78:90:AB:CD:EF:12:34:56:78:90"
)
```

**Go:**
```go
client := license.NewClient(
    "https://192.168.1.100:8080",
    "your_app_key",
    license.WithCertFingerprint("SHA256:AB:CD:EF:12:34:56:78:90:..."),
)
```

#### 方式2：使用证书文件

将服务器的 `server.crt` 文件打包到客户端中：

**Python:**
```python
client = LicenseClient(
    server_url="https://192.168.1.100:8080",
    app_key="your_app_key",
    cert_path="./certs/server.crt"  # 服务器证书文件
)
```

**Go:**
```go
client := license.NewClient(
    "https://192.168.1.100:8080",
    "your_app_key",
    license.WithCertFile("./certs/server.crt"),
)
```

#### 方式3：跳过验证（仅测试用！）

**警告：生产环境绝对不要使用此选项！**

**Python:**
```python
client = LicenseClient(
    server_url="https://192.168.1.100:8080",
    app_key="your_app_key",
    skip_verify=True  # 仅测试环境使用！
)
```

**Go:**
```go
client := license.NewClient(
    "https://192.168.1.100:8080",
    "your_app_key",
    license.WithSkipVerify(true),  // 仅测试环境使用！
)
```

### 证书固定安全级别对比

| 配置方式 | 安全级别 | 适用场景 |
|---------|---------|---------|
| 证书指纹 | ⭐⭐⭐⭐⭐ | 生产环境（推荐） |
| 证书文件 | ⭐⭐⭐⭐ | 生产环境 |
| 跳过验证 | ⭐ | 仅开发测试 |
| 无配置 | ⭐⭐ | 使用系统 CA |

---

## Python SDK

### 安装

```bash
pip install requests cryptography
```

将 `python/license_client.py` 复制到你的项目中。

### 快速开始

```python
from license_client import LicenseClient

# 初始化客户端（推荐：启用证书固定）
client = LicenseClient(
    server_url="https://192.168.1.100:8080",
    app_key="your_app_key",
    cert_fingerprint="SHA256:AB:CD:EF:..."  # 证书指纹
)

# 方式一：授权码激活
try:
    result = client.activate("XXXX-XXXX-XXXX-XXXX")
    print(f"激活成功，剩余 {result['remaining_days']} 天")
except Exception as e:
    print(f"激活失败: {e}")

# 方式二：账号密码登录
try:
    result = client.login("user@example.com", "password")
    print(f"登录成功，套餐: {result['plan_type']}")
except Exception as e:
    print(f"登录失败: {e}")

# 检查授权状态
if client.is_valid():
    print("授权有效")
else:
    print("授权无效")

# 检查功能权限
if client.has_feature("export"):
    print("有导出权限")

# 获取剩余天数
days = client.get_remaining_days()
print(f"剩余 {days} 天")

# 检查更新
update = client.check_update()
if update:
    print(f"发现新版本: {update['version']}")

# 解绑设备
client.deactivate()
```

### 配置选项

```python
client = LicenseClient(
    server_url="http://localhost:8080",
    app_key="your_app_key",
    cache_dir="/path/to/cache",      # 缓存目录
    heartbeat_interval=3600,          # 心跳间隔（秒）
    offline_grace_days=7              # 离线宽限期（天）
)
```

### 便捷函数

```python
import license_client as lc

# 初始化
lc.init("http://localhost:8080", "your_app_key")

# 检查授权
if lc.is_valid():
    print("授权有效")

# 检查功能
if lc.has_feature("export"):
    print("有导出权限")
```

---

## Go SDK

### 安装

将 `go/license_client.go` 复制到你的项目中，或作为包引入。

### 快速开始

```go
package main

import (
    "fmt"
    "your_project/license"
)

func main() {
    // 初始化客户端
    client := license.NewClient(
        "http://localhost:8080",
        "your_app_key",
    )
    defer client.Close()

    // 方式一：授权码激活
    info, err := client.Activate("XXXX-XXXX-XXXX-XXXX")
    if err != nil {
        fmt.Printf("激活失败: %v\n", err)
        return
    }
    fmt.Printf("激活成功，剩余 %d 天\n", info.RemainingDays)

    // 方式二：账号密码登录
    info, err = client.Login("user@example.com", "password")
    if err != nil {
        fmt.Printf("登录失败: %v\n", err)
        return
    }
    fmt.Printf("登录成功，套餐: %s\n", info.PlanType)

    // 检查授权状态
    if client.IsValid() {
        fmt.Println("授权有效")
    }

    // 检查功能权限
    if client.HasFeature("export") {
        fmt.Println("有导出权限")
    }

    // 获取剩余天数
    days := client.GetRemainingDays()
    fmt.Printf("剩余 %d 天\n", days)

    // 检查更新
    update, err := client.CheckUpdate()
    if err == nil && update != nil {
        fmt.Printf("发现新版本: %s\n", update.Version)
    }

    // 解绑设备
    client.Deactivate()
}
```

### 配置选项

```go
import "time"

client := license.NewClient(
    "http://localhost:8080",
    "your_app_key",
    license.WithCacheDir("/path/to/cache"),
    license.WithHeartbeatInterval(time.Hour),
    license.WithOfflineGraceDays(7),
    license.WithAppVersion("1.0.0"),
)
```

---

## 密码安全

SDK 的密码传输规则按接口分开处理：

- 登录和订阅解绑：SDK 会使用客户端预哈希，服务端按 `password_hashed=true` 验证。
- 注册和修改密码：SDK 会提交原始密码，服务端负责校验密码强度并落库哈希。服务端会拒绝这两个接口的 `password_hashed=true` 请求。

注册和修改密码必须走 HTTPS，生产环境不要用明文 HTTP 暴露这些接口。

### 工作原理

```
用户输入密码: "mypassword123"
        ↓
客户端预哈希: SHA256("mypassword123:user@email.com:license_salt_v1")
        ↓
传输哈希值: "a1b2c3d4e5f6..." (64字符十六进制)
        ↓
服务端再次哈希: bcrypt(预哈希值)
        ↓
存储到数据库: "$2a$10$..."
```

### 安全优势

| 攻击场景 | 无预哈希 | 有预哈希 |
|---------|---------|---------|
| HTTPS 被破解 | ❌ 密码泄露 | ✅ 只泄露哈希 |
| 中间人攻击 | ❌ 密码泄露 | ✅ 只泄露哈希 |
| 服务器日志泄露 | ❌ 可能记录明文 | ✅ 只有哈希 |
| 重放攻击 | ❌ 可重放 | ⚠️ 可重放（需配合其他措施） |

### 使用方式

SDK 会自动按接口选择正确的密码格式，无需手动设置 `password_hashed`：

**Python:**
```python
# 登录会自动预哈希
client.login("user@example.com", "mypassword123")

# 注册和修改密码会提交原始密码，由服务端执行强度校验和哈希存储
client.register("user@example.com", "mypassword123", "用户名")
client.change_password("old_password", "new_password")
```

**Go:**
```go
// 登录会自动预哈希
client.Login("user@example.com", "mypassword123")

// 注册和修改密码会提交原始密码，由服务端执行强度校验和哈希存储
client.Register("user@example.com", "mypassword123", "用户名")
client.ChangePassword("old_password", "new_password", "")
```

### 兼容性说明

- 登录和订阅解绑接口支持预哈希。
- 注册和修改密码接口只接受原始密码。
- 建议所有客户端升级到最新 SDK

---

## API 参考

### 客户端方法

| 方法 | 说明 |
|------|------|
| `Activate(licenseKey)` | 使用授权码激活 |
| `Login(email, password)` | 使用账号密码登录 |
| `Register(email, password, name)` | 注册新用户 |
| `ChangePassword(old, new, email)` | 修改当前登录账号密码，需要客户端会话 |
| `Verify()` | 验证授权状态 |
| `Heartbeat()` | 发送心跳 |
| `Deactivate()` | 解绑设备 |
| `IsValid()` | 检查授权是否有效 |
| `GetFeatures()` | 获取功能权限列表 |
| `HasFeature(feature)` | 检查是否有某个功能 |
| `GetRemainingDays()` | 获取剩余天数 |
| `GetLicenseInfo()` | 获取完整授权信息 |
| `CheckUpdate()` | 检查版本更新 |
| `Close()` | 关闭客户端 |

### 授权信息字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `valid` | bool | 是否有效 |
| `license_id` | string | 授权ID（授权码模式） |
| `subscription_id` | string | 订阅ID（账号密码模式） |
| `device_id` | string | 设备ID |
| `type` | string | 授权类型 |
| `plan_type` | string | 套餐类型 |
| `expire_at` | string | 过期时间 |
| `remaining_days` | int | 剩余天数，-1表示永久 |
| `features` | []string | 功能权限列表 |

---

## 最佳实践

### 1. 应用启动时检查授权

```python
# Python
from license_client import LicenseClient

client = LicenseClient("http://localhost:8080", "your_app_key")

if not client.is_valid():
    # 显示激活/登录界面
    show_activation_dialog()
else:
    # 正常启动应用
    start_app()
```

### 2. 功能权限控制

```python
# Python
def export_data():
    if not client.has_feature("export"):
        show_upgrade_dialog("导出功能需要专业版")
        return
    # 执行导出
    do_export()
```

### 3. 到期提醒

```python
# Python
days = client.get_remaining_days()
if 0 < days <= 7:
    show_warning(f"您的授权将在 {days} 天后到期，请及时续费")
```

### 4. 版本更新检测

```python
# Python
update = client.check_update()
if update and update['force_update']:
    show_force_update_dialog(update)
elif update:
    show_optional_update_dialog(update)
```

---

## 错误处理

SDK 会抛出 `LicenseError` 异常，常见错误：

| 错误信息 | 说明 |
|----------|------|
| 无效的应用 | app_key 错误 |
| 无效的授权码 | 授权码不存在 |
| 账号或密码错误 | 登录失败 |
| 授权已被吊销 | 授权被管理员吊销 |
| 授权已过期 | 授权已到期 |
| 设备数量已达上限 | 超过最大设备数 |
| 设备已被禁止使用 | 设备在黑名单中 |

---

## 离线支持

SDK 支持离线使用：

1. 首次激活/登录时，授权信息会缓存到本地
2. 离线时，SDK 会使用缓存的授权信息
3. 超过离线宽限期（默认7天）后，需要联网验证
4. 心跳会自动在后台运行，保持授权状态同步

---

## 安全说明

1. **机器码**：基于主机名、MAC地址、硬盘序列号等信息生成，用于设备绑定
2. **缓存加密**：默认使用 AES-256-GCM 加密本地缓存
3. **签名验证**：服务器返回的数据带有 RSA 签名，可用于验证数据完整性

---

## 安全增强使用

### Python 安全模式

```python
from license_client import LicenseClient
from license_security import SecureLicenseClient, check_environment

# 检查运行环境
env = check_environment()
if env['debugger']:
    print("检测到调试器，程序退出")
    exit(1)

# 创建普通客户端
client = LicenseClient("http://localhost:8080", "your_app_key")

# 包装为安全客户端
secure_client = SecureLicenseClient(client)

# 使用安全客户端进行验证
if secure_client.is_valid():
    print("授权有效")

    # 检查功能权限
    if secure_client.has_feature("export"):
        do_export()
else:
    print("授权无效或检测到安全威胁")
```

### Go 安全模式

```go
package main

import (
    "fmt"
    "your_project/license"
)

func main() {
    // 检查运行环境
    env := license.CheckEnvironment()
    if env["debugger"] {
        fmt.Println("检测到调试器，程序退出")
        return
    }

    // 创建普通客户端
    client := license.NewClient("http://localhost:8080", "your_app_key")

    // 包装为安全客户端
    secureClient := license.WrapClient(client)
    defer secureClient.Close()

    // 使用安全客户端进行验证
    if secureClient.IsValid() {
        fmt.Println("授权有效")

        if secureClient.HasFeature("export") {
            doExport()
        }
    } else {
        fmt.Println("授权无效或检测到安全威胁")
    }
}
```

---

## 代码混淆方案

为了进一步提高安全性，建议对发布的代码进行混淆处理：

### Python 混淆

推荐工具：

| 工具 | 说明 | 命令 |
|------|------|------|
| **PyArmor** | 商业级加密，推荐 | `pyarmor gen -O dist your_app.py` |
| **Nuitka** | 编译为二进制 | `nuitka --standalone your_app.py` |
| **Cython** | 编译为 .so/.pyd | `cythonize -i your_app.py` |

```bash
# PyArmor 示例（推荐）
pip install pyarmor
pyarmor gen --pack onefile -O dist your_app.py

# Nuitka 示例（编译为可执行文件）
pip install nuitka
nuitka --standalone --onefile --windows-disable-console your_app.py
```

### Go 混淆

推荐工具：

| 工具 | 说明 | 命令 |
|------|------|------|
| **garble** | 符号混淆 | `garble build` |
| **go-strip** | 去除符号表 | `go build -ldflags="-s -w"` |
| **UPX** | 压缩加壳 | `upx --best app.exe` |

```bash
# 推荐的编译命令（组合使用）
# 1. 使用 garble 混淆编译
go install mvdan.cc/garble@latest
garble -literals -tiny build -ldflags="-s -w" -o app.exe

# 2. 使用 UPX 压缩
upx --best --lzma app.exe
```

### 混淆效果对比

| 方案 | 反编译难度 | 性能影响 | 推荐场景 |
|------|-----------|---------|---------|
| 无混淆 | ⭐ | 无 | 开发测试 |
| 符号去除 | ⭐⭐ | 无 | 基本保护 |
| 代码混淆 | ⭐⭐⭐ | 轻微 | 商业软件 |
| 编译为二进制 | ⭐⭐⭐⭐ | 无 | 高安全需求 |
| 加壳保护 | ⭐⭐⭐⭐⭐ | 启动稍慢 | 最高安全 |

---

## 服务端功能控制（推荐）

最安全的方式是将关键功能放在服务端执行：

```python
# 不安全：本地验证后执行
if client.has_feature("export"):
    do_export()  # 可被绕过

# 安全：服务端执行关键逻辑
def export_data():
    # 调用服务端 API，服务端验证授权后返回数据
    response = requests.post(
        f"{server_url}/api/client/export",
        json={
            "app_key": app_key,
            "machine_id": client.machine_id,
            "data_type": "users"
        }
    )
    if response.status_code == 200:
        return response.json()['data']
    else:
        raise LicenseError("无权限或授权无效")
```

---

## 安全建议清单

- [ ] 使用安全客户端包装器 (`SecureLicenseClient`)
- [ ] 启用缓存加密 (`encrypt_cache=True`)
- [ ] 发布前进行代码混淆
- [ ] 关键功能放在服务端执行
- [ ] 定期更新 SDK 版本
- [ ] 监控异常激活行为（管理后台）
- [ ] 启用 HTTPS 通信

---

## 热更新功能

SDK 提供完整的热更新支持，包括：

| 功能 | 说明 |
|------|------|
| 增量更新 | 只下载变更部分，节省流量 |
| 全量更新 | 下载完整更新包 |
| 自动回滚 | 更新失败自动恢复到上一版本 |
| 灰度发布 | 按比例逐步推送更新 |
| 强制更新 | 必须更新才能继续使用 |
| 更新日志 | 记录每次更新状态 |

### Python 热更新

```python
from license_client import LicenseClient
from hotupdate import HotUpdateManager, HotUpdateStatus

# 初始化客户端
client = LicenseClient(
    server_url="http://localhost:8080",
    app_key="your_app_key"
)

# 创建热更新管理器
updater = HotUpdateManager(
    client=client,
    current_version="1.0.0",
    auto_check=True,           # 自动检查更新
    check_interval=3600,       # 检查间隔（秒）
    callback=on_update_status  # 状态回调
)

# 状态回调函数
def on_update_status(status: HotUpdateStatus, progress: float, error):
    if status == HotUpdateStatus.DOWNLOADING:
        print(f"下载中: {progress * 100:.1f}%")
    elif status == HotUpdateStatus.INSTALLING:
        print("安装中...")
    elif status == HotUpdateStatus.SUCCESS:
        print("更新成功!")
    elif status == HotUpdateStatus.FAILED:
        print(f"更新失败: {error}")

# 手动检查更新
update_info = updater.check_update()
if update_info and update_info.get('has_update'):
    print(f"发现新版本: {update_info['to_version']}")
    print(f"更新日志: {update_info['changelog']}")

    if update_info.get('force_update'):
        print("这是强制更新，必须更新后才能继续使用")

    # 下载更新
    update_file = updater.download_update(update_info)

    # 应用更新
    updater.apply_update(
        update_info,
        update_file,
        target_dir="./app",
        pre_update_hook=lambda: True,   # 更新前检查
        post_update_hook=lambda: True   # 更新后检查
    )

# 启动自动检查
updater.start_auto_check()

# 获取更新历史
history = updater.get_update_history()
for item in history:
    print(f"{item['from_version']} -> {item['to_version']}: {item['status']}")

# 回滚到上一版本
updater.rollback(target_dir="./app")
```

### Go 热更新

```go
package main

import (
    "fmt"
    "time"
    "your_project/license"
)

func main() {
    // 初始化客户端
    client := license.NewClient(
        "http://localhost:8080",
        "your_app_key",
    )
    defer client.Close()

    // 创建热更新管理器
    updater := license.NewHotUpdateManager(
        client,
        "1.0.0",  // 当前版本
        license.WithAutoCheck(true, time.Hour),
        license.WithUpdateCallback(onUpdateStatus),
    )

    // 检查更新
    updateInfo, err := updater.CheckUpdate()
    if err != nil {
        fmt.Printf("检查更新失败: %v\n", err)
        return
    }

    if updateInfo != nil && updateInfo.HasUpdate {
        fmt.Printf("发现新版本: %s\n", updateInfo.ToVersion)
        fmt.Printf("更新日志: %s\n", updateInfo.Changelog)

        // 下载更新
        updateFile, err := updater.DownloadUpdate(updateInfo)
        if err != nil {
            fmt.Printf("下载失败: %v\n", err)
            return
        }

        // 应用更新
        err = updater.ApplyUpdate(updateInfo, updateFile, "./app")
        if err != nil {
            fmt.Printf("更新失败: %v\n", err)
            return
        }

        fmt.Println("更新成功!")
    }

    // 启动自动检查
    updater.StartAutoCheck()
    defer updater.StopAutoCheck()
}

// 状态回调
func onUpdateStatus(status license.HotUpdateStatus, progress float64, err error) {
    switch status {
    case license.HotUpdateStatusDownloading:
        fmt.Printf("下载中: %.1f%%\n", progress*100)
    case license.HotUpdateStatusInstalling:
        fmt.Println("安装中...")
    case license.HotUpdateStatusSuccess:
        fmt.Println("更新成功!")
    case license.HotUpdateStatusFailed:
        fmt.Printf("更新失败: %v\n", err)
    }
}
```

### 热更新 API

#### HotUpdateManager 方法

| 方法 | 说明 |
|------|------|
| `check_update()` | 检查是否有可用更新 |
| `download_update(info)` | 下载更新包 |
| `apply_update(info, file, dir)` | 应用更新 |
| `rollback(dir)` | 回滚到上一版本 |
| `start_auto_check()` | 启动自动检查 |
| `stop_auto_check()` | 停止自动检查 |
| `get_update_history()` | 获取更新历史 |
| `is_updating()` | 是否正在更新 |
| `get_current_version()` | 获取当前版本 |

#### 更新信息字段

| 字段 | 类型 | 说明 |
|------|------|------|
| `has_update` | bool | 是否有更新 |
| `id` | string | 热更新ID |
| `from_version` | string | 源版本 |
| `to_version` | string | 目标版本 |
| `update_type` | string | 更新类型 (patch/full) |
| `download_url` | string | 下载地址 |
| `file_size` | int | 文件大小 |
| `file_hash` | string | 文件哈希 |
| `changelog` | string | 更新日志 |
| `force_update` | bool | 是否强制更新 |

### 热更新最佳实践

#### 1. 应用启动时检查更新

```python
def on_app_start():
    update_info = updater.check_update()

    if update_info and update_info.get('force_update'):
        # 强制更新，阻止应用启动
        show_force_update_dialog(update_info)
        return False

    if update_info:
        # 可选更新，提示用户
        show_optional_update_dialog(update_info)

    return True
```

#### 2. 后台静默更新

```python
def background_update():
    update_info = updater.check_update()

    if update_info and not update_info.get('force_update'):
        # 静默下载
        update_file = updater.download_update(update_info)

        # 下次启动时应用
        save_pending_update(update_file, update_info)
```

#### 3. 更新前后钩子

```python
def pre_update_check():
    """更新前检查"""
    # 检查磁盘空间
    if get_free_space() < required_space:
        return False

    # 保存用户数据
    save_user_data()

    return True

def post_update_check():
    """更新后检查"""
    # 验证关键文件
    if not verify_critical_files():
        return False  # 触发回滚

    # 运行自检
    if not run_self_test():
        return False

    return True

updater.apply_update(
    update_info,
    update_file,
    target_dir,
    pre_update_hook=pre_update_check,
    post_update_hook=post_update_check
)
```

---

## 数据同步功能

SDK 提供完整的数据同步支持，可以将本地数据同步到云端服务器。

### Sync/Backup 真实回归

后端本地启动后，可运行：

```bash
python tools/client_sync_backup_smoke.py --run-go-sdk
```

该脚本会临时创建应用、客户和订阅，验证旧 `app_key + machine_id` 访问 `/api/client/sync/*`、`/api/client/backup/*` 被拒绝，并验证 Python SDK Bearer token 同步/备份可用；`--run-go-sdk` 会额外跑 Go SDK 备份集成测试。测试完成会自动清理临时数据。

### Go SDK 加强版防篡改开关

Go SDK 默认构建不会启用读取机器码内存、Hook 检测和断点扫描这类更激进的防篡改逻辑，避免普通客户端因为平台差异、杀软或 `go vet` 的 `unsafe` 检查产生额外成本。

如果你的客户端确实需要加强版检测，可以显式打开 build tag：

```bash
go build -tags advanced_antitamper ./...
go test -tags advanced_antitamper ./sdk/go
```

不开这个 tag 时，SDK 仍保留常规授权、缓存、热更新、WebSocket、数据同步和安全脚本能力。

### Python 数据同步

```python
from license_client import LicenseClient
from data_sync import DataSyncClient, AutoSyncManager, ConflictResolution

# 初始化
client = LicenseClient(server_url, app_key, skip_verify=True)
sync_client = DataSyncClient(client)

# 获取表列表
tables = sync_client.get_table_list()
for table in tables:
    print(f"表: {table.table_name}, 记录数: {table.record_count}")

# 拉取数据
records, server_time = sync_client.pull_table("my_table")
for record in records:
    print(f"ID: {record.id}, 数据: {record.data}")

# 推送数据
result = sync_client.push_record("my_table", "record_id", {"name": "test", "value": 123})
print(f"状态: {result.status}, 版本: {result.version}")

# 批量推送
results = sync_client.push_record_batch("my_table", [
    {"record_id": "1", "data": {"name": "a"}, "version": 0},
    {"record_id": "2", "data": {"name": "b"}, "version": 0},
])

# 解决冲突；conflict_id 来自 push_changes 返回结果
result = sync_client.resolve_conflict(conflict_id, ConflictResolution.USE_LOCAL)

# 获取同步状态
status = sync_client.get_sync_status()
print(f"待同步变更: {status.pending_changes}")

# 自动同步
auto_sync = AutoSyncManager(sync_client, ["table1", "table2"], interval=60)
auto_sync.set_on_pull(lambda table, records, deletes: print(f"收到 {len(records)} 条更新"))
auto_sync.start()
```

### Go 数据同步

```go
// 创建数据同步客户端
syncClient := client.NewDataSyncClient()

// 获取表列表
tables, err := syncClient.GetTableList()

// 拉取数据
records, serverTime, err := syncClient.PullTable("my_table", 0)

// 推送数据
result, err := syncClient.PushRecord("my_table", "record_id", data, 0)

// 批量推送
results, err := syncClient.PushRecordBatch("my_table", items)

// 获取高级同步变更；第二个参数是服务端 data_type，nil 表示全部支持类型
changes, serverTime, err := syncClient.GetChanges(since, []string{license.DataTypeWorkflow})

// 解决冲突；conflict_id 来自 PushChanges 返回结果
result, err := syncClient.ResolveConflict(conflictID, license.UseLocal, nil)

// 分类数据同步
configs, _, _ := syncClient.GetConfigs(0)
workflows, _, _ := syncClient.GetWorkflows(0)
materials, _, _ := syncClient.GetMaterials(0)
posts, _, _ := syncClient.GetPosts(0, "")
scripts, _, _ := syncClient.GetCommentScripts(0, "")

// 自动同步管理器
autoSync := syncClient.NewAutoSyncManager([]string{"table1", "table2"}, time.Minute)
autoSync.OnPull(func(table string, records []license.SyncRecord, deletes []string) error {
    // 处理同步数据
    return nil
})
autoSync.Start()
defer autoSync.Stop()
```

### .NET/C# 客户端接口

```csharp
using LicenseServer.Sdk;

using var client = new LicenseClient(
    baseUrl: "http://localhost:8080",
    appKey: "your_app_key");

// 账号密码模式会保存 /api/client 的 access_token/refresh_token。
await client.LoginAsync("user@example.com", "password");

// 自动按当前会话模式选择授权码或订阅校验/心跳。
var verified = await client.VerifyAsync();
var heartbeat = await client.SendHeartbeatAsync("1.0.0");

// 热更新
// client.HotUpdates.SetSignaturePublicKeyPem(appPublicKeyPem); // 可选：校验更新包 RSA 签名
var update = await client.HotUpdates.CheckAsync("1.0.0");
if (update.HasUpdate)
{
    await client.HotUpdates.DownloadAsync(update, "./downloads/update.zip");
    await client.HotUpdates.ReportInstallingAsync(update);
    await client.HotUpdates.ReportSuccessAsync(update);
}

// 数据同步：通用表
var table = await client.DataSync.PullTableAsync("my_table");
await client.DataSync.PushRecordAsync("my_table", "record-1", new Dictionary<string, object?>
{
    ["name"] = "test",
    ["value"] = 123,
});

// WebSocket 实时指令
await using var ws = client.CreateWebSocketClient();
ws.RegisterInstructionHandler("get_status", _ => new { ok = true });
await ws.ConnectAsync();
await ws.SendStatusAsync(new { online = true });
```

### .NET/C# AI Proxy 与额度扣减

.NET SDK 有两套登录入口，别混用：

- 管理后台账号：`LicenseServerClient + AuthApi + CreditApi + ProxyApi(serverClient)`，走 `/api/*`，适合后台工具。
- 客户端客户账号：`LicenseClient + ClientCreditApi + ProxyApi(licenseClient)`，走 `/api/client/*`，适合正式软件客户端。

AI 软件客户端应使用第二种。客户用邮箱密码登录后，生成图片/视频、Prompt 辅助、任务和文件接口都会带客户端订阅 token，请求归属到当前 app，额度从该客户账号扣。

```csharp
using LicenseServer.Sdk;

using var client = new LicenseClient(
    baseUrl: "https://your-license-server.example.com",
    appKey: "your_app_key");

await client.LoginAsync("customer@example.com", "customer-password");

var credit = new ClientCreditApi(client);
var balance = await credit.GetMyCreditAsync();

var proxy = new ProxyApi(client);
var result = await proxy.GenerateAsync(
    providerSlug: "sora",
    body: new
    {
        model = "sora-2",
        prompt = "a cat dancing under stars",
        duration_seconds = 5,
        aspect_ratio = "16:9",
    },
    mode: "async",
    scope: "video");
```

---

## 脚本管理功能

SDK 支持从服务器获取脚本版本信息和下载脚本文件。

### Python 脚本管理

```python
from license_client import LicenseClient
from scripts import ScriptManager, ReleaseManager

# 初始化
client = LicenseClient(server_url, app_key, skip_verify=True)
script_manager = ScriptManager(client)

# 获取脚本版本
versions = script_manager.get_script_versions()
for script in versions.scripts:
    print(f"脚本: {script.filename}, 版本: {script.version}")

# 下载脚本
content = script_manager.download_script("script.py")
# 或保存到文件
script_manager.download_script("script.py", "./downloads/script.py")

# 检查脚本更新
has_update, info = script_manager.check_script_update("script.py", current_version_code=1)
if has_update:
    print(f"发现新版本: {info.version}")

# 版本下载
release_manager = ReleaseManager(client)

# 下载版本文件（带进度回调）
def on_progress(downloaded, total):
    print(f"下载进度: {downloaded}/{total}")

release_manager.download_release("app_v1.0.0.zip", "./downloads/app.zip", on_progress)

# 获取最新版本并下载
update_info = release_manager.get_latest_release_and_download("./downloads/latest.zip", on_progress)
print(f"下载完成: {update_info.version}")
```

### Go 脚本管理

```go
// 创建脚本管理器
scriptManager := client.NewScriptManager()

// 获取脚本版本
versions, err := scriptManager.GetScriptVersions()
for _, script := range versions.Scripts {
    fmt.Printf("脚本: %s, 版本: %s\n", script.Filename, script.Version)
}

// 下载脚本
content, err := scriptManager.DownloadScript("script.py", "")
// 或保存到文件
content, err = scriptManager.DownloadScript("script.py", "./downloads/script.py")

// 检查脚本更新
hasUpdate, info, err := scriptManager.CheckScriptUpdate("script.py", 1)
if hasUpdate {
    fmt.Printf("发现新版本: %s\n", info.Version)
}

// 版本下载
releaseManager := client.NewReleaseManager()

// 下载版本文件（带进度回调）
err = releaseManager.DownloadRelease("app_v1.0.0.zip", "./downloads/app.zip",
    func(downloaded, total int64) {
        fmt.Printf("下载进度: %d/%d\n", downloaded, total)
    })

// 获取最新版本并下载
updateInfo, err := releaseManager.GetLatestReleaseAndDownload("./downloads/latest.zip",
    func(downloaded, total int64) {
        fmt.Printf("下载进度: %d/%d\n", downloaded, total)
    })
```

---

## 安全脚本功能

SDK 支持从服务器获取加密脚本并安全执行。

### Python 安全脚本

```python
from license_client import LicenseClient
from secure_script import SecureScriptManager

# 初始化
client = LicenseClient(server_url, app_key, skip_verify=True)
script_manager = SecureScriptManager(client, app_secret="your_app_secret")

# 获取脚本版本列表
versions = script_manager.get_script_versions()
for v in versions:
    print(f"脚本: {v.name}, 版本: {v.version}")

# 获取并解密脚本
script = script_manager.fetch_script("script_id")
print(f"脚本内容长度: {len(script.content)}")

# 执行脚本
def my_executor(content, args):
    # 自定义执行逻辑
    exec(content.decode('utf-8'))
    return "success"

result = script_manager.execute_script("script_id", {"arg1": "value"}, my_executor)
```

### Go 安全脚本

```go
// 创建安全脚本管理器
scriptManager := license.NewSecureScriptManager(client,
    license.WithAppSecret("your_app_secret"),
    license.WithExecuteCallback(func(scriptID, status string, err error) {
        fmt.Printf("脚本 %s 状态: %s\n", scriptID, status)
    }),
)

// 获取脚本版本列表
versions, err := scriptManager.GetScriptVersions()

// 获取并解密脚本
script, err := scriptManager.FetchScript("script_id")
fmt.Printf("脚本内容长度: %d\n", len(script.Content))

// 执行脚本
result, err := scriptManager.ExecuteScript("script_id", args,
    func(content []byte, args map[string]interface{}) (string, error) {
        // 自定义执行逻辑
        return "success", nil
    })
```

---

## SDK 功能对比

| 功能模块 | Go SDK | Python SDK | .NET/C# SDK |
|---------|--------|------------|-------------|
| 授权码激活 | ✅ | ✅ | ✅ |
| 账号密码登录 | ✅ | ✅ | ✅ |
| 修改密码 | ✅ | ✅ | ✅ |
| 证书固定 | ✅ | ✅ | 由宿主 `HttpClient` 配置 |
| 缓存加密 | ✅ AES-256-GCM | ✅ Fernet | 由宿主 `IAuthSessionStore` / `IClientSessionStore` 实现 |
| AI Proxy / 任务 / 文件 | - | - | ✅ |
| 热更新 | ✅ | ✅ | ✅ 检查/下载/状态上报 |
| WebSocket | ✅ | ✅ | ✅ 实时指令/状态/脚本回执 |
| 数据同步 | ✅ 完整 | ✅ 完整 | ✅ 核心同步/通用表/常用分类 |
| 安全脚本 | ✅ | ✅ | ✅ |
| 脚本管理 | ✅ | ✅ | ✅ |
| 版本下载 | ✅ | ✅ | ✅ |
| 反调试检测 | ✅ | ✅ | 暂未封装 |
| 时间回拨检测 | ✅ | ✅ | 暂未封装 |
| 环境检测 | ✅ | ✅ | 暂未封装 |
| 高级安全模块 | ✅ | ❌ | 暂未封装 |
| 强化安全模块 | ✅ | ❌ | 暂未封装 |
