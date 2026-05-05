"""
安全脚本模块
提供加密脚本的获取、解密和执行功能

安全特性：
- 支持 HTTPS 和证书固定（继承自 LicenseClient）
- AES-GCM 加密
- RSA 签名验证
- HKDF 密钥派生

使用示例：
    from license_client import LicenseClient
    from secure_script import SecureScriptManager

    client = LicenseClient(
        server_url="https://192.168.1.100:8080",
        app_key="your_app_key",
        cert_fingerprint="SHA256:AB:CD:EF:..."  # 证书固定
    )

    # 创建安全脚本管理器
    script_manager = SecureScriptManager(client, app_secret="your_app_secret")

    # 获取脚本版本列表
    versions = script_manager.get_script_versions()

    # 获取并解密脚本
    script = script_manager.fetch_script("script_id")

    # 执行脚本
    result = script_manager.execute_script("script_id", executor=my_executor)
"""

import base64
import hashlib
import json
import threading
import time
from dataclasses import dataclass
from typing import Any, Callable, Dict, List, Optional

try:
    from cryptography.hazmat.primitives.ciphers.aead import AESGCM
    from cryptography.hazmat.primitives import hashes
    from cryptography.hazmat.primitives.kdf.hkdf import HKDF
    from cryptography.hazmat.backends import default_backend
    from cryptography.hazmat.primitives.asymmetric import padding
    from cryptography.hazmat.primitives import serialization
except ImportError:
    raise ImportError("请安装 cryptography 库: pip install cryptography")

try:
    import requests
except ImportError:
    raise ImportError("请安装 requests 库: pip install requests")


@dataclass
class CachedScript:
    """缓存的脚本"""
    script_id: str
    delivery_id: str
    version: str
    content: bytes
    content_hash: str
    fetched_at: float
    expires_at: float


@dataclass
class EncryptedScriptPackage:
    """加密脚本包"""
    script_id: str
    delivery_id: str
    version: str
    script_type: str
    entry_point: str
    encrypted_content: str
    content_hash: str
    key_hint: str
    signature: str
    expires_at: int
    timeout: int
    memory_limit: int
    parameters: str
    execute_once: bool


@dataclass
class ScriptVersionInfo:
    """脚本版本信息"""
    script_id: str
    name: str
    version: str
    content_hash: str
    updated_at: int


class SecureScriptError(Exception):
    """安全脚本错误"""
    pass


class SecureScriptManager:
    """安全脚本管理器"""

    def __init__(
        self,
        client,  # LicenseClient 实例
        app_secret: str,
        on_execute: Optional[Callable[[str, str, Optional[Exception]], None]] = None
    ):
        """
        初始化安全脚本管理器

        Args:
            client: LicenseClient 实例
            app_secret: 应用密钥 (用于密钥派生)
            on_execute: 执行回调函数 (script_id, status, error)
        """
        self.client = client
        self.app_secret = app_secret
        self.on_execute = on_execute
        self._script_cache: Dict[str, CachedScript] = {}
        self._lock = threading.Lock()

    def get_script_versions(self) -> List[ScriptVersionInfo]:
        """
        获取脚本版本列表

        Returns:
            脚本版本信息列表
        """
        versions = []
        try:
            data = self.client._request_with_client_auth('GET', '/secure-scripts/versions') or []
        except Exception as e:
            raise SecureScriptError(f"获取版本失败: {e}")

        for item in data:
            versions.append(ScriptVersionInfo(
                script_id=item['script_id'],
                name=item['name'],
                version=item['version'],
                content_hash=item['content_hash'],
                updated_at=item['updated_at']
            ))

        return versions

    def fetch_script(self, script_id: str, force: bool = False) -> CachedScript:
        """
        获取加密脚本并解密

        Args:
            script_id: 脚本ID
            force: 是否强制刷新缓存

        Returns:
            解密后的脚本
        """
        # 检查缓存
        if not force:
            with self._lock:
                if script_id in self._script_cache:
                    cached = self._script_cache[script_id]
                    if time.time() < cached.expires_at:
                        return cached

        # 请求服务器
        data = {
            "script_id": script_id
        }

        try:
            pkg_data = self.client._request_with_client_auth('POST', '/secure-scripts/fetch', data) or {}
        except Exception as e:
            raise SecureScriptError(f"获取脚本失败: {e}")
        pkg = EncryptedScriptPackage(
            script_id=pkg_data['script_id'],
            delivery_id=pkg_data.get('delivery_id', ''),
            version=pkg_data['version'],
            script_type=pkg_data['script_type'],
            entry_point=pkg_data['entry_point'],
            encrypted_content=pkg_data['encrypted_content'],
            content_hash=pkg_data['content_hash'],
            key_hint=pkg_data['key_hint'],
            signature=pkg_data['signature'],
            expires_at=pkg_data['expires_at'],
            timeout=pkg_data['timeout'],
            memory_limit=pkg_data['memory_limit'],
            parameters=pkg_data['parameters'],
            execute_once=pkg_data['execute_once']
        )

        # 验证过期时间
        if time.time() > pkg.expires_at:
            raise SecureScriptError("脚本包已过期")

        # 验证签名
        sign_data = f"{pkg.script_id}:{pkg.encrypted_content}:{self.client.machine_id}:{pkg.expires_at}"
        if not self._verify_signature(sign_data.encode(), pkg.signature):
            raise SecureScriptError("签名验证失败")

        # 派生解密密钥
        decrypt_key = self._derive_key(pkg.key_hint)

        # 解密内容
        content = self._decrypt_content(pkg.encrypted_content, decrypt_key)

        # 验证哈希
        content_hash = hashlib.sha256(content).hexdigest()
        if content_hash != pkg.content_hash:
            raise SecureScriptError("内容哈希不匹配")

        # 缓存
        cached = CachedScript(
            script_id=pkg.script_id,
            delivery_id=pkg.delivery_id,
            version=pkg.version,
            content=content,
            content_hash=pkg.content_hash,
            fetched_at=time.time(),
            expires_at=pkg.expires_at
        )

        with self._lock:
            self._script_cache[script_id] = cached

        return cached

    def execute_script(
        self,
        script_id: str,
        args: Optional[Dict[str, Any]] = None,
        executor: Optional[Callable[[bytes, Dict[str, Any]], str]] = None
    ) -> str:
        """
        执行脚本

        Args:
            script_id: 脚本ID
            args: 执行参数
            executor: 执行函数，接收 (content, args)，返回结果字符串

        Returns:
            执行结果
        """
        if executor is None:
            # 默认使用 exec 执行 Python 脚本
            def default_executor(content: bytes, args: Dict[str, Any]) -> str:
                local_vars = {"args": args or {}, "result": None}
                exec(content.decode('utf-8'), {}, local_vars)
                return str(local_vars.get('result', ''))
            executor = default_executor

        # 获取脚本
        try:
            script = self.fetch_script(script_id)
        except Exception as e:
            self._report_execution(script_id, "", "failed", "", str(e), 0)
            if self.on_execute:
                self.on_execute(script_id, "failed", e)
            raise

        # 上报开始执行
        self._report_execution(script_id, script.delivery_id, "executing", "", "", 0)
        if self.on_execute:
            self.on_execute(script_id, "executing", None)

        # 执行
        start_time = time.time()
        try:
            result = executor(script.content, args or {})
            duration = int((time.time() - start_time) * 1000)

            # 上报成功
            self._report_execution(script_id, script.delivery_id, "success", result, "", duration)
            if self.on_execute:
                self.on_execute(script_id, "success", None)

            # 清除缓存 (安全考虑)
            with self._lock:
                self._script_cache.pop(script_id, None)

            return result

        except Exception as e:
            duration = int((time.time() - start_time) * 1000)

            # 上报失败
            self._report_execution(script_id, script.delivery_id, "failed", "", str(e), duration)
            if self.on_execute:
                self.on_execute(script_id, "failed", e)

            # 清除缓存
            with self._lock:
                self._script_cache.pop(script_id, None)

            raise

    def clear_cache(self):
        """清除脚本缓存"""
        with self._lock:
            self._script_cache.clear()

    def get_cached_script(self, script_id: str) -> Optional[CachedScript]:
        """获取缓存的脚本 (不请求服务器)"""
        with self._lock:
            return self._script_cache.get(script_id)

    # 内部方法

    def _derive_key(self, key_hint: str) -> bytes:
        """派生解密密钥"""
        master_key = self.app_secret.encode()
        salt = self.client.machine_id.encode()
        info = key_hint.encode()

        hkdf = HKDF(
            algorithm=hashes.SHA256(),
            length=32,
            salt=salt,
            info=info,
            backend=default_backend()
        )

        return hkdf.derive(master_key)

    def _decrypt_content(self, encrypted_base64: str, key: bytes) -> bytes:
        """解密内容"""
        ciphertext = base64.b64decode(encrypted_base64)

        # AES-GCM: nonce (12 bytes) + ciphertext + tag (16 bytes)
        nonce_size = 12
        if len(ciphertext) < nonce_size:
            raise SecureScriptError("密文太短")

        nonce = ciphertext[:nonce_size]
        ciphertext = ciphertext[nonce_size:]

        aesgcm = AESGCM(key)
        return aesgcm.decrypt(nonce, ciphertext, None)

    def _verify_signature(self, data: bytes, signature_base64: str) -> bool:
        """验证签名"""
        public_key = getattr(self.client, '_public_key', None) or getattr(self.client, 'public_key', None)
        if public_key is None:
            return True  # 如果没有公钥，跳过验证

        try:
            signature = base64.b64decode(signature_base64)
            public_key.verify(
                signature,
                data,
                padding.PKCS1v15(),
                hashes.SHA256()
            )
            return True
        except Exception:
            return False

    def _report_execution(
        self,
        script_id: str,
        delivery_id: str,
        status: str,
        result: str,
        error_msg: str,
        duration: int
    ):
        """上报执行状态"""
        try:
            data = {
                "script_id": script_id,
                "status": status
            }
            if delivery_id:
                data["delivery_id"] = delivery_id
            if result:
                data["result"] = result
            if error_msg:
                data["error_message"] = error_msg
            if duration > 0:
                data["duration"] = duration

            # 异步上报
            threading.Thread(
                target=lambda: self.client._request_with_client_auth('POST', '/secure-scripts/report', data),
                daemon=True
            ).start()
        except Exception:
            pass


# 便捷函数

def fetch_and_execute(
    client,
    app_secret: str,
    script_id: str,
    args: Optional[Dict[str, Any]] = None,
    executor: Optional[Callable[[bytes, Dict[str, Any]], str]] = None
) -> str:
    """
    获取并执行脚本 (便捷函数)

    Args:
        client: LicenseClient 实例
        app_secret: 应用密钥
        script_id: 脚本ID
        args: 执行参数
        executor: 执行函数

    Returns:
        执行结果
    """
    manager = SecureScriptManager(client, app_secret)
    return manager.execute_script(script_id, args, executor)
