"""
License Server Python SDK
授权管理系统 Python 客户端 SDK

支持两种授权模式：
1. 授权码模式 - 使用授权码激活
2. 账号密码模式 - 使用账号密码登录

安全特性：
- 证书固定（Certificate Pinning）防止中间人攻击
- RSA 签名验证防止数据篡改
- 缓存加密（AES-256）
- 机器码绑定

使用示例：
    from license_client import LicenseClient

    # 初始化客户端（推荐：启用证书固定和签名验证）
    client = LicenseClient(
        server_url="https://192.168.1.100:8080",
        app_key="your_app_key",
        # 证书固定配置（三选一）
        cert_fingerprint="SHA256:AB:CD:EF:...",  # 方式1：证书指纹
        # cert_path="./server.crt",              # 方式2：证书文件
        # skip_verify=True,                      # 方式3：跳过验证（仅测试用）
        # 签名验证配置（推荐启用）
        public_key_pem=\"\"\"-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA...
-----END PUBLIC KEY-----\"\"\",
    )

    # 授权码模式激活
    result = client.activate("XXXX-XXXX-XXXX-XXXX")

    # 或账号密码模式登录
    result = client.login("user@example.com", "password")

    # 检查授权状态
    if client.is_valid():
        print("授权有效")
"""

import os
import json
import hashlib
import platform
import uuid
import time
import threading
import base64
import ssl
import socket
from typing import Optional, Dict, List, Union
from pathlib import Path

try:
    import requests
    from requests.adapters import HTTPAdapter
    from urllib3.util.ssl_ import create_urllib3_context
except ImportError:
    raise ImportError("请安装 requests 库: pip install requests")

# 可选：使用 cryptography 库进行加密
try:
    from cryptography.fernet import Fernet
    from cryptography.hazmat.primitives import hashes
    from cryptography.hazmat.primitives.kdf.pbkdf2 import PBKDF2HMAC
    from cryptography.x509 import load_pem_x509_certificate
    from cryptography.hazmat.backends import default_backend
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric import rsa, padding
    CRYPTO_AVAILABLE = True
except ImportError:
    CRYPTO_AVAILABLE = False


class LicenseError(Exception):
    """授权错误"""
    pass


class SignatureVerificationError(Exception):
    """签名验证失败"""
    pass


class SignatureMissingError(Exception):
    """缺少签名"""
    pass


class SignatureExpiredError(Exception):
    """签名过期"""
    pass


class CertificatePinningError(Exception):
    """证书固定验证失败"""
    pass


class CertificatePinningAdapter(HTTPAdapter):
    """
    证书固定 HTTP 适配器
    用于防止中间人攻击（MITM）
    """

    def __init__(
        self,
        cert_fingerprint: Optional[str] = None,
        cert_path: Optional[str] = None,
        skip_verify: bool = False,
        **kwargs
    ):
        """
        初始化证书固定适配器

        Args:
            cert_fingerprint: 证书指纹（SHA256），格式如 "SHA256:AB:CD:EF:..."
            cert_path: 证书文件路径（PEM 格式）
            skip_verify: 是否跳过证书验证（仅用于测试，生产环境禁用）
        """
        self.cert_fingerprint = cert_fingerprint
        self.cert_path = cert_path
        self.skip_verify = skip_verify
        self._expected_fingerprint = None

        if cert_fingerprint:
            # 解析指纹格式
            fp = cert_fingerprint.upper()
            if fp.startswith("SHA256:"):
                fp = fp[7:]
            self._expected_fingerprint = fp.replace(":", "").lower()

        if cert_path and os.path.exists(cert_path):
            # 从证书文件计算指纹
            self._expected_fingerprint = self._calculate_cert_fingerprint(cert_path)

        super().__init__(**kwargs)

    def _calculate_cert_fingerprint(self, cert_path: str) -> str:
        """从证书文件计算 SHA256 指纹"""
        with open(cert_path, 'rb') as f:
            cert_data = f.read()

        if CRYPTO_AVAILABLE:
            cert = load_pem_x509_certificate(cert_data, default_backend())
            fingerprint = cert.fingerprint(hashes.SHA256())
            return fingerprint.hex()
        else:
            # 简单方式：直接对 PEM 内容哈希
            return hashlib.sha256(cert_data).hexdigest()

    def init_poolmanager(self, *args, **kwargs):
        """初始化连接池管理器"""
        if self.skip_verify:
            # 跳过验证（仅测试用）
            ctx = create_urllib3_context()
            ctx.check_hostname = False
            ctx.verify_mode = ssl.CERT_NONE
            kwargs['ssl_context'] = ctx
        elif self.cert_path and os.path.exists(self.cert_path):
            # 使用指定的证书文件
            ctx = create_urllib3_context()
            ctx.load_verify_locations(self.cert_path)
            kwargs['ssl_context'] = ctx

        super().init_poolmanager(*args, **kwargs)

    def send(self, request, *args, **kwargs):
        """发送请求并验证证书指纹"""
        if self.skip_verify:
            kwargs['verify'] = False
        elif self.cert_path and os.path.exists(self.cert_path):
            kwargs['verify'] = self.cert_path

        response = super().send(request, *args, **kwargs)

        # 验证证书指纹
        if self._expected_fingerprint and not self.skip_verify:
            self._verify_certificate_fingerprint(request.url)

        return response

    def _verify_certificate_fingerprint(self, url: str):
        """验证服务器证书指纹"""
        from urllib.parse import urlparse
        parsed = urlparse(url)

        if parsed.scheme != 'https':
            return

        hostname = parsed.hostname
        port = parsed.port or 443

        try:
            # 获取服务器证书
            context = ssl.create_default_context()
            context.check_hostname = False
            context.verify_mode = ssl.CERT_NONE

            with socket.create_connection((hostname, port), timeout=10) as sock:
                with context.wrap_socket(sock, server_hostname=hostname) as ssock:
                    cert_der = ssock.getpeercert(binary_form=True)
                    actual_fingerprint = hashlib.sha256(cert_der).hexdigest()

                    if actual_fingerprint != self._expected_fingerprint:
                        raise CertificatePinningError(
                            f"证书指纹不匹配！\n"
                            f"期望: {self._expected_fingerprint}\n"
                            f"实际: {actual_fingerprint}\n"
                            f"可能存在中间人攻击！"
                        )
        except CertificatePinningError:
            raise
        except Exception as e:
            raise CertificatePinningError(f"证书指纹验证失败: {e}")


def get_server_certificate_fingerprint(host: str, port: int = 443) -> str:
    """
    获取服务器证书的 SHA256 指纹

    用于首次配置证书固定时获取服务器证书指纹

    Args:
        host: 服务器地址
        port: 端口号

    Returns:
        证书指纹字符串，格式如 "SHA256:AB:CD:EF:..."

    使用示例：
        fingerprint = get_server_certificate_fingerprint("192.168.1.100", 8080)
        print(f"服务器证书指纹: {fingerprint}")
        # 然后将此指纹配置到客户端
    """
    context = ssl.create_default_context()
    context.check_hostname = False
    context.verify_mode = ssl.CERT_NONE

    with socket.create_connection((host, port), timeout=10) as sock:
        with context.wrap_socket(sock, server_hostname=host) as ssock:
            cert_der = ssock.getpeercert(binary_form=True)
            fingerprint = hashlib.sha256(cert_der).hexdigest()
            # 格式化为易读格式
            formatted = ':'.join(fingerprint[i:i+2].upper() for i in range(0, len(fingerprint), 2))
            return f"SHA256:{formatted}"


class CacheEncryption:
    """缓存加密工具"""

    def __init__(self, machine_id: str, app_key: str):
        """
        初始化加密工具

        Args:
            machine_id: 机器码，用于生成加密密钥
            app_key: 应用密钥
        """
        self.machine_id = machine_id
        self.app_key = app_key
        self._fernet = None

        if CRYPTO_AVAILABLE:
            self._init_fernet()

    def _init_fernet(self):
        """初始化 Fernet 加密器"""
        # 使用机器码和应用密钥派生加密密钥
        salt = hashlib.sha256(self.app_key.encode()).digest()[:16]
        kdf = PBKDF2HMAC(
            algorithm=hashes.SHA256(),
            length=32,
            salt=salt,
            iterations=100000,
        )
        key = base64.urlsafe_b64encode(kdf.derive(self.machine_id.encode()))
        self._fernet = Fernet(key)

    def encrypt(self, data: str) -> str:
        """加密数据"""
        if not CRYPTO_AVAILABLE or not self._fernet:
            # 如果没有加密库，使用简单的混淆
            return self._simple_obfuscate(data)

        encrypted = self._fernet.encrypt(data.encode())
        return base64.b64encode(encrypted).decode()

    def decrypt(self, encrypted_data: str) -> str:
        """解密数据"""
        if not CRYPTO_AVAILABLE or not self._fernet:
            return self._simple_deobfuscate(encrypted_data)

        try:
            decoded = base64.b64decode(encrypted_data.encode())
            decrypted = self._fernet.decrypt(decoded)
            return decrypted.decode()
        except Exception:
            # 尝试简单混淆解密（兼容旧版本）
            try:
                return self._simple_deobfuscate(encrypted_data)
            except:
                raise LicenseError("缓存数据解密失败")

    def _simple_obfuscate(self, data: str) -> str:
        """简单混淆（无加密库时使用）"""
        key = hashlib.sha256((self.machine_id + self.app_key).encode()).digest()
        result = []
        for i, char in enumerate(data.encode()):
            result.append(char ^ key[i % len(key)])
        return base64.b64encode(bytes(result)).decode()

    def _simple_deobfuscate(self, data: str) -> str:
        """简单反混淆"""
        key = hashlib.sha256((self.machine_id + self.app_key).encode()).digest()
        decoded = base64.b64decode(data.encode())
        result = []
        for i, byte in enumerate(decoded):
            result.append(byte ^ key[i % len(key)])
        return bytes(result).decode()


class LicenseClient:
    """授权客户端"""

    def __init__(
        self,
        server_url: str,
        app_key: str,
        cache_dir: Optional[str] = None,
        heartbeat_interval: int = 3600,
        offline_grace_days: int = 7,
        encrypt_cache: bool = True,
        # 证书固定配置
        cert_fingerprint: Optional[str] = None,
        cert_path: Optional[str] = None,
        skip_verify: bool = False,
        # 签名验证配置
        public_key_pem: Optional[str] = None,
        require_signature: bool = False,
        signature_time_window: int = 300,
        # 连接配置
        timeout: int = 30,
        max_retries: int = 3
    ):
        """
        初始化授权客户端

        Args:
            server_url: 服务器地址，如 https://192.168.1.100:8080
            app_key: 应用 Key
            cache_dir: 缓存目录，默认为用户目录下的 .license_cache
            heartbeat_interval: 心跳间隔（秒），默认 3600
            offline_grace_days: 离线宽限期（天），默认 7
            encrypt_cache: 是否加密缓存，默认 True

            # 证书固定配置（防止中间人攻击，三选一）
            cert_fingerprint: 证书指纹，格式如 "SHA256:AB:CD:EF:..."
            cert_path: 证书文件路径（PEM 格式）
            skip_verify: 跳过证书验证（仅测试环境使用！）

            # 签名验证配置（防止数据篡改）
            public_key_pem: 服务端公钥（PEM 格式），用于验证响应签名
            require_signature: 是否强制要求签名验证
            signature_time_window: 签名时间窗口（秒），防止重放攻击，默认 300

            # 连接配置
            timeout: 请求超时时间（秒）
            max_retries: 最大重试次数
        """
        self.server_url = server_url.rstrip('/')
        self.app_key = app_key
        self.cache_dir = cache_dir or os.path.join(Path.home(), '.license_cache')
        self.heartbeat_interval = heartbeat_interval
        self.offline_grace_days = offline_grace_days
        self.encrypt_cache = encrypt_cache
        self.timeout = timeout
        self.max_retries = max_retries

        # 证书固定配置
        self.cert_fingerprint = cert_fingerprint
        self.cert_path = cert_path
        self.skip_verify = skip_verify

        # 签名验证配置
        self.public_key_pem = public_key_pem
        self.require_signature = require_signature or (public_key_pem is not None)
        self.signature_time_window = signature_time_window
        self._public_key = None

        self._license_info: Optional[Dict] = None
        self._machine_id: Optional[str] = None
        self._heartbeat_thread: Optional[threading.Thread] = None
        self._stop_heartbeat = False
        self._encryption: Optional[CacheEncryption] = None
        self._session: Optional[requests.Session] = None

        os.makedirs(self.cache_dir, exist_ok=True)

        # 初始化公钥
        if self.public_key_pem and CRYPTO_AVAILABLE:
            self._init_public_key()

        # 初始化加密工具
        if self.encrypt_cache:
            self._encryption = CacheEncryption(self.machine_id, self.app_key)

        # 初始化 HTTP 会话（带证书固定）
        self._init_session()

        self._load_cache()

    def _init_public_key(self):
        """初始化公钥"""
        if not CRYPTO_AVAILABLE:
            return
        try:
            self._public_key = serialization.load_pem_public_key(
                self.public_key_pem.encode(),
                backend=default_backend()
            )
        except Exception as e:
            raise LicenseError(f"无效的公钥格式: {e}")

    def _init_session(self):
        """初始化 HTTP 会话，配置证书固定"""
        self._session = requests.Session()

        # 配置证书固定适配器
        adapter = CertificatePinningAdapter(
            cert_fingerprint=self.cert_fingerprint,
            cert_path=self.cert_path,
            skip_verify=self.skip_verify,
            max_retries=self.max_retries
        )

        self._session.mount('https://', adapter)
        self._session.mount('http://', HTTPAdapter(max_retries=self.max_retries))

    @property
    def machine_id(self) -> str:
        """获取机器码"""
        if self._machine_id is None:
            self._machine_id = self._generate_machine_id()
        return self._machine_id

    def _generate_machine_id(self) -> str:
        """生成机器码（增强版）"""
        info_parts = [
            platform.node(),           # 主机名
            platform.machine(),        # 机器类型
            platform.processor(),      # 处理器信息
            platform.system(),         # 操作系统
        ]

        # 获取 MAC 地址
        try:
            mac = uuid.getnode()
            # 检查是否是真实 MAC 地址（非随机生成）
            if (mac >> 40) % 2 == 0:  # 第一个字节的最低位为0表示真实MAC
                info_parts.append(str(mac))
        except:
            pass

        # 尝试获取更多硬件信息
        try:
            # Windows: 获取硬盘序列号
            if platform.system() == 'Windows':
                import subprocess
                result = subprocess.run(
                    ['wmic', 'diskdrive', 'get', 'serialnumber'],
                    capture_output=True, text=True, timeout=5
                )
                if result.returncode == 0:
                    lines = [l.strip() for l in result.stdout.strip().split('\n') if l.strip() and l.strip() != 'SerialNumber']
                    if lines:
                        info_parts.append(lines[0])
        except:
            pass

        try:
            # Linux: 获取机器 ID
            if platform.system() == 'Linux':
                machine_id_path = '/etc/machine-id'
                if os.path.exists(machine_id_path):
                    with open(machine_id_path, 'r') as f:
                        info_parts.append(f.read().strip())
        except:
            pass

        try:
            # macOS: 获取硬件 UUID
            if platform.system() == 'Darwin':
                import subprocess
                result = subprocess.run(
                    ['system_profiler', 'SPHardwareDataType'],
                    capture_output=True, text=True, timeout=5
                )
                if result.returncode == 0:
                    for line in result.stdout.split('\n'):
                        if 'Hardware UUID' in line:
                            info_parts.append(line.split(':')[-1].strip())
                            break
        except:
            pass

        combined = '|'.join(info_parts)
        return hashlib.sha256(combined.encode()).hexdigest()[:32]

    def _get_device_info(self) -> Dict[str, str]:
        """获取设备信息"""
        return {
            "name": platform.node(),
            "hostname": platform.node(),
            "os": platform.system(),
            "os_version": platform.version(),
            "app_version": "1.0.0"
        }

    def _get_cache_path(self) -> str:
        """获取缓存文件路径"""
        # 使用加密后缀区分
        suffix = ".enc" if self.encrypt_cache else ".json"
        return os.path.join(self.cache_dir, f"{self.app_key}{suffix}")

    def _load_cache(self):
        """加载缓存的授权信息"""
        cache_path = self._get_cache_path()

        # 也尝试加载旧格式缓存
        old_cache_path = os.path.join(self.cache_dir, f"{self.app_key}.json")

        if os.path.exists(cache_path):
            try:
                with open(cache_path, 'r', encoding='utf-8') as f:
                    content = f.read()

                if self.encrypt_cache and self._encryption:
                    content = self._encryption.decrypt(content)

                self._license_info = json.loads(content)
                if self._license_info and not self._check_signature_integrity():
                    self._license_info = None
                    try:
                        os.remove(cache_path)
                    except OSError:
                        pass
            except Exception as e:
                self._license_info = None
        elif os.path.exists(old_cache_path) and self.encrypt_cache:
            # 迁移旧的明文缓存到加密格式
            try:
                with open(old_cache_path, 'r', encoding='utf-8') as f:
                    self._license_info = json.load(f)
                if self._license_info and not self._check_signature_integrity():
                    self._license_info = None
                    try:
                        os.remove(old_cache_path)
                    except OSError:
                        pass
                    return
                # 保存为加密格式
                self._save_cache()
                # 删除旧的明文缓存
                os.remove(old_cache_path)
            except:
                self._license_info = None

    def _save_cache(self):
        """保存授权信息到缓存"""
        if self._license_info:
            cache_path = self._get_cache_path()
            content = json.dumps(self._license_info, ensure_ascii=False, indent=2)

            if self.encrypt_cache and self._encryption:
                content = self._encryption.encrypt(content)

            with open(cache_path, 'w', encoding='utf-8') as f:
                f.write(content)

    def _clear_cache(self):
        """清除缓存"""
        cache_path = self._get_cache_path()
        if os.path.exists(cache_path):
            os.remove(cache_path)
        # 也清除旧格式缓存
        old_cache_path = os.path.join(self.cache_dir, f"{self.app_key}.json")
        if os.path.exists(old_cache_path):
            os.remove(old_cache_path)
        self._license_info = None

    def _request(self, method: str, endpoint: str, data: Optional[Dict] = None, headers: Optional[Dict] = None) -> Dict:
        """发送 HTTP 请求（使用证书固定）"""
        url = f"{self.server_url}/api/client{endpoint}"
        headers = headers or {}
        method_upper = method.upper()
        try:
            if method_upper == 'GET':
                resp = self._session.get(url, params=data, headers=headers, timeout=self.timeout)
            elif method_upper == 'DELETE':
                resp = self._session.delete(url, json=data, headers=headers, timeout=self.timeout)
            elif method_upper == 'PUT':
                resp = self._session.put(url, json=data, headers=headers, timeout=self.timeout)
            else:
                resp = self._session.post(url, json=data, headers=headers, timeout=self.timeout)
            try:
                result = resp.json()
            except ValueError:
                text = resp.text[:500] if resp.text else ''
                if resp.status_code >= 400:
                    raise LicenseError(f"HTTP {resp.status_code}: {text or '非 JSON 错误响应'}")
                raise LicenseError(f"响应不是有效 JSON: {text or '空响应'}")
            if result.get('code') != 0:
                raise LicenseError(result.get('message', '请求失败'))
            return result.get('data', {})
        except CertificatePinningError:
            raise
        except requests.exceptions.RequestException as e:
            raise LicenseError(f"网络请求失败: {e}")

    def activate(self, license_key: str) -> Dict:
        """
        使用授权码激活（授权码模式）

        Args:
            license_key: 授权码

        Returns:
            授权信息字典
        """
        data = {
            "app_key": self.app_key,
            "license_key": license_key,
            "machine_id": self.machine_id,
            "device_info": self._get_device_info()
        }
        result = self._request_with_verification('POST', '/auth/activate', data)
        self._license_info = {
            **result,
            "license_key": license_key,
            "activated_at": time.time(),
            "last_verified_at": time.time()
        }
        self._save_cache()
        self._start_heartbeat()
        return result

    def _hash_password(self, password: str, email: str) -> str:
        """
        客户端预哈希密码

        使用 SHA256(password + email) 作为预哈希
        这样即使 HTTPS 被破解，攻击者也无法获得原始密码

        Args:
            password: 原始密码
            email: 用户邮箱（作为盐值）

        Returns:
            预哈希后的密码（hex 格式）
        """
        # 使用 email 作为盐值，防止彩虹表攻击
        salted = f"{password}:{email.lower()}:license_salt_v1"
        return hashlib.sha256(salted.encode()).hexdigest()

    def login(self, email: str, password: str) -> Dict:
        """
        使用账号密码登录（账号密码模式）

        Args:
            email: 邮箱
            password: 密码

        Returns:
            授权信息字典
        """
        # 客户端预哈希密码，防止明文传输
        hashed_password = self._hash_password(password, email)

        data = {
            "app_key": self.app_key,
            "email": email,
            "password": hashed_password,
            "password_hashed": True,  # 标记密码已预哈希
            "machine_id": self.machine_id,
            "device_info": self._get_device_info()
        }
        result = self._request_with_verification('POST', '/auth/login', data)
        self._license_info = {
            **result,
            "email": email,
            "login_at": time.time(),
            "last_verified_at": time.time()
        }
        self._save_cache()
        self._start_heartbeat()
        return result

    def register(self, email: str, password: str, name: str = "") -> Dict:
        """
        注册新用户（账号密码模式）

        Args:
            email: 邮箱
            password: 密码
            name: 用户名（可选）

        Returns:
            注册结果
        """
        data = {
            "app_key": self.app_key,
            "email": email,
            "password": password,
            "password_hashed": False,
            "name": name
        }
        return self._request('POST', '/auth/register', data)

    def change_password(self, old_password: str, new_password: str, email: str = None) -> Dict:
        """
        修改密码

        Args:
            old_password: 旧密码
            new_password: 新密码
            email: 邮箱（如果未登录需要提供）

        Returns:
            修改结果
        """
        user_email = email or (self._license_info.get('email') if self._license_info else None)
        if not user_email:
            raise LicenseError("需要提供邮箱")

        data = {
            "old_password": old_password,
            "new_password": new_password,
            "password_hashed": False
        }
        return self._request_with_client_auth('PUT', '/auth/password', data)

    def verify(self) -> bool:
        """验证授权状态"""
        try:
            data = {
                "app_key": self.app_key,
                "machine_id": self.machine_id
            }
            result = self._request_with_verification('POST', '/auth/verify', data)
            if self._license_info:
                self._license_info['last_verified_at'] = time.time()
                self._license_info.update(result)
                self._save_cache()
            return result.get('valid', False)
        except LicenseError:
            return False

    def heartbeat(self) -> bool:
        """发送心跳"""
        try:
            auth_mode = (self._license_info or {}).get('auth_mode', '')
            if (auth_mode or '').strip().lower() == 'subscription':
                return self.subscription_heartbeat()

            data = {
                "app_key": self.app_key,
                "machine_id": self.machine_id,
                "app_version": self._get_device_info().get('app_version', '')
            }
            result = self._request_with_verification('POST', '/auth/heartbeat', data)
            if self._license_info:
                self._license_info['last_verified_at'] = time.time()
                self._save_cache()
            return result.get('valid', False)
        except LicenseError:
            return False

    def subscription_heartbeat(self) -> bool:
        """发送订阅模式心跳"""
        try:
            data = {
                "app_key": self.app_key,
                "machine_id": self.machine_id,
                "app_version": self._get_device_info().get('app_version', '')
            }
            result = self._request_with_verification('POST', '/subscription/heartbeat', data)
            if self._license_info:
                self._license_info['last_verified_at'] = time.time()
                self._save_cache()
            return result.get('valid', False)
        except LicenseError:
            return False

    def _check_signature_integrity(self) -> bool:
        """检查当前授权信息的签名完整性"""
        if not self.require_signature or not self._public_key:
            return True
        if not self._license_info:
            return False

        signature = self._license_info.get('signature', '')
        if not signature:
            return False

        try:
            self._verify_response_signature(self._license_info, signature)
            return True
        except (LicenseError, SignatureVerificationError, SignatureMissingError, SignatureExpiredError):
            return False

    def _refresh_client_session(self) -> bool:
        """刷新客户端会话 token"""
        if not self._license_info:
            return False

        refresh_token = self._license_info.get('refresh_token', '')
        if not refresh_token:
            return False

        try:
            result = self._request('POST', '/auth/refresh', {"refresh_token": refresh_token})
            access_token = result.get('access_token', '')
            if not access_token:
                return False
            self._license_info['access_token'] = access_token
            if result.get('refresh_token'):
                self._license_info['refresh_token'] = result.get('refresh_token')
            if result.get('token_type'):
                self._license_info['token_type'] = result.get('token_type')
            if result.get('session_id'):
                self._license_info['session_id'] = result.get('session_id')
            if result.get('auth_mode'):
                self._license_info['auth_mode'] = result.get('auth_mode')
            if result.get('expires_in') is not None:
                self._license_info['expires_in'] = result.get('expires_in')
            if result.get('refresh_expires_in') is not None:
                self._license_info['refresh_expires_in'] = result.get('refresh_expires_in')
            if result.get('access_expires_at') is not None:
                self._license_info['access_expires_at'] = result.get('access_expires_at')
            if result.get('refresh_expires_at') is not None:
                self._license_info['refresh_expires_at'] = result.get('refresh_expires_at')
            self._save_cache()
            return True
        except LicenseError:
            return False

    def _request_with_client_auth(self, method: str, endpoint: str, data: Optional[Dict] = None) -> Dict:
        """使用 access token 发起客户端会话请求，失败时自动 refresh 一次"""
        if not self._license_info:
            raise LicenseError("未登录")

        access_token = self._license_info.get('access_token', '')
        if not access_token:
            raise LicenseError("缺少客户端会话令牌")

        headers = {"Authorization": f"Bearer {access_token}"}
        try:
            return self._request(method, endpoint, data, headers=headers)
        except LicenseError as e:
            msg = str(e)
            if any(k in msg for k in ["会话", "认证", "客户端令牌"]) and self._refresh_client_session():
                access_token = self._license_info.get('access_token', '')
                if not access_token:
                    raise
                headers = {"Authorization": f"Bearer {access_token}"}
                return self._request(method, endpoint, data, headers=headers)
            raise

    def deactivate(self, password: Optional[str] = None) -> bool:
        """解绑设备。订阅模式需要传入 password。"""
        try:
            auth_mode = (self._license_info or {}).get('auth_mode', '')
            access_token = (self._license_info or {}).get('access_token', '')

            if access_token:
                mode = (auth_mode or '').strip().lower()
                body: Dict[str, Union[str, bool]] = {}
                if mode == 'subscription':
                    if not password:
                        return False
                    email = (self._license_info or {}).get('email', '')
                    if email:
                        body['password'] = self._hash_password(password, email)
                        body['password_hashed'] = True
                    else:
                        body['password'] = password

                self._request_with_client_auth('DELETE', '/devices/self', body)
                self._clear_cache()
                self._stop_heartbeat = True
                return True

            # 兼容旧版（授权码模式）
            if (auth_mode or '').strip().lower() == 'subscription':
                return False
            data = {
                "app_key": self.app_key,
                "machine_id": self.machine_id
            }
            self._request('POST', '/auth/deactivate', data)
            self._clear_cache()
            self._stop_heartbeat = True
            return True
        except LicenseError:
            return False

    def is_valid(self) -> bool:
        """检查授权是否有效（支持离线）"""
        if not self._license_info:
            return False
        if not self._license_info.get('valid', False):
            return False

        expire_at = self._license_info.get('expire_at')
        if expire_at:
            try:
                from datetime import datetime
                if isinstance(expire_at, str):
                    expire_time = datetime.fromisoformat(expire_at.replace('Z', '+00:00'))
                    if expire_time.timestamp() < time.time():
                        return False
            except:
                pass

        last_verified = self._license_info.get('last_verified_at', 0)
        offline_days = (time.time() - last_verified) / 86400
        if offline_days > self.offline_grace_days:
            return self.verify()

        if not self._check_signature_integrity():
            return False
        return True

    def get_features(self) -> List[str]:
        """获取功能权限列表"""
        if not self._license_info:
            return []
        return self._license_info.get('features', [])

    def has_feature(self, feature: str) -> bool:
        """检查是否有某个功能权限"""
        return feature in self.get_features()

    def get_remaining_days(self) -> int:
        """获取剩余天数，-1 表示永久"""
        if not self._license_info:
            return 0
        return self._license_info.get('remaining_days', 0)

    def get_license_info(self) -> Optional[Dict]:
        """获取完整的授权信息"""
        return self._license_info

    def check_update(self) -> Optional[Dict]:
        """检查版本更新"""
        try:
            return self._request_with_client_auth('GET', '/releases/latest')
        except LicenseError:
            return None

    def _start_heartbeat(self):
        """启动心跳线程"""
        if self._heartbeat_thread and self._heartbeat_thread.is_alive():
            return
        self._stop_heartbeat = False
        self._heartbeat_thread = threading.Thread(target=self._heartbeat_loop, daemon=True)
        self._heartbeat_thread.start()

    def _heartbeat_loop(self):
        """心跳循环"""
        while not self._stop_heartbeat:
            time.sleep(self.heartbeat_interval)
            if not self._stop_heartbeat:
                self.heartbeat()

    def close(self):
        """关闭客户端"""
        self._stop_heartbeat = True

    # ==================== 签名验证相关方法 ====================

    def _verify_response_signature(self, data: Dict, signature: str) -> None:
        """
        验证服务端响应签名

        Args:
            data: 响应数据
            signature: 签名字符串（Base64 编码）

        Raises:
            SignatureVerificationError: 签名验证失败
            SignatureMissingError: 缺少签名
            SignatureExpiredError: 签名过期
        """
        # 如果没有配置公钥
        if not self._public_key:
            if self.require_signature:
                raise LicenseError("未配置公钥，无法验证签名")
            return

        # 如果没有签名
        if not signature:
            if self.require_signature:
                raise SignatureMissingError("响应缺少签名，无法验证数据完整性")
            return

        # 检查时间戳（防止重放攻击）
        if self.signature_time_window > 0:
            timestamp = data.get('timestamp')
            if timestamp:
                current_time = int(time.time())
                if abs(current_time - int(timestamp)) > self.signature_time_window:
                    raise SignatureExpiredError("签名已过期，可能是重放攻击")

        # 构建待验证的数据（排除签名字段）
        data_to_verify = {k: v for k, v in data.items() if k != 'signature'}

        # 序列化数据（按键排序以确保一致性）
        data_bytes = self._canonical_json(data_to_verify)

        # 验证签名
        self._verify_signature(data_bytes, signature)

    def _canonical_json(self, data: Dict) -> bytes:
        """生成规范化的 JSON（键按字母排序）"""
        return json.dumps(data, sort_keys=True, separators=(',', ':')).encode()

    def _verify_signature(self, data: bytes, signature_base64: str) -> None:
        """
        使用公钥验证签名

        Args:
            data: 原始数据
            signature_base64: Base64 编码的签名

        Raises:
            SignatureVerificationError: 签名验证失败
        """
        if not CRYPTO_AVAILABLE:
            raise LicenseError("需要安装 cryptography 库才能验证签名")

        try:
            signature = base64.b64decode(signature_base64)
            self._public_key.verify(
                signature,
                data,
                padding.PKCS1v15(),
                hashes.SHA256()
            )
        except Exception as e:
            raise SignatureVerificationError(f"签名验证失败，数据可能被篡改: {e}")

    def _request_with_verification(self, method: str, endpoint: str, data: Optional[Dict] = None) -> Dict:
        """发送 HTTP 请求并验证签名"""
        result = self._request(method, endpoint, data)

        # 验证签名
        signature = result.get('signature', '')
        self._verify_response_signature(result, signature)

        return result

    def is_signature_enabled(self) -> bool:
        """检查是否启用了签名验证"""
        return self._public_key is not None and self.require_signature

    def set_public_key(self, public_key_pem: str):
        """
        动态设置公钥

        Args:
            public_key_pem: PEM 格式的公钥
        """
        self.public_key_pem = public_key_pem
        self.require_signature = True
        self._init_public_key()


# 便捷函数
_default_client: Optional[LicenseClient] = None


def init(server_url: str, app_key: str, **kwargs) -> LicenseClient:
    """初始化默认客户端"""
    global _default_client
    _default_client = LicenseClient(server_url, app_key, **kwargs)
    return _default_client


def get_client() -> Optional[LicenseClient]:
    """获取默认客户端"""
    return _default_client


def is_valid() -> bool:
    """检查授权是否有效"""
    if _default_client:
        return _default_client.is_valid()
    return False


def has_feature(feature: str) -> bool:
    """检查是否有某个功能权限"""
    if _default_client:
        return _default_client.has_feature(feature)
    return False
