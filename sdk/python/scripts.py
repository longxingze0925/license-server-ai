"""
脚本管理和版本下载模块
支持脚本版本检查、下载和版本更新

功能特性：
- 获取脚本版本信息
- 下载脚本文件
- 检查脚本更新
- 下载版本发布文件

使用示例：
    from license_client import LicenseClient
    from scripts import ScriptManager, ReleaseManager

    # 初始化
    client = LicenseClient(server_url, app_key, skip_verify=True)

    # 脚本管理
    script_manager = ScriptManager(client)
    versions = script_manager.get_script_versions()
    content = script_manager.download_script("script.py")

    # 版本下载
    release_manager = ReleaseManager(client)
    release_manager.download_release("app_v1.0.0.zip", "./downloads/app.zip")
"""

import os
import hashlib
from typing import Optional, Dict, List, Callable, Tuple
from dataclasses import dataclass
from urllib.parse import quote, urlparse, urljoin


def _get_with_client_auth(client, url: str, stream: bool = False):
    def do_request():
        access_token = (getattr(client, '_license_info', {}) or {}).get('access_token', '')
        if not access_token:
            raise Exception("缺少客户端会话令牌")
        return client._session.get(
            url,
            headers={"Authorization": f"Bearer {access_token}"},
            stream=stream,
            timeout=client.timeout,
        )

    resp = do_request()
    if resp.status_code == 401 and getattr(client, '_refresh_client_session', lambda: False)():
        resp.close()
        resp = do_request()
    return resp


def _ensure_parent_dir(file_path: str) -> None:
    dir_path = os.path.dirname(file_path)
    if dir_path:
        os.makedirs(dir_path, exist_ok=True)


def _remove_if_exists(file_path: str) -> None:
    try:
        os.remove(file_path)
    except FileNotFoundError:
        pass


def _atomic_replace(temp_path: str, final_path: str) -> None:
    os.replace(temp_path, final_path)


@dataclass
class ScriptInfo:
    """脚本信息"""
    filename: str
    version: str
    version_code: int
    file_size: int
    file_hash: str
    updated_at: str


@dataclass
class ScriptVersionResponse:
    """脚本版本响应"""
    scripts: List[ScriptInfo]
    total_count: int
    last_updated: str


@dataclass
class UpdateInfo:
    """更新信息"""
    version: str
    version_code: int
    download_url: str
    changelog: str
    file_size: int
    file_hash: str
    file_signature: str
    signature_alg: str
    force_update: bool


class ScriptManager:
    """脚本管理器"""

    def __init__(self, license_client):
        """
        初始化脚本管理器

        Args:
            license_client: LicenseClient 实例
        """
        self.client = license_client

    def get_script_versions(self) -> ScriptVersionResponse:
        """
        获取脚本版本信息

        Returns:
            脚本版本响应，包含所有可用脚本的版本信息
        """
        data = self.client._request_with_client_auth('GET', '/scripts/version') or {}
        scripts = [ScriptInfo(
            filename=s.get('filename', ''),
            version=s.get('version', ''),
            version_code=s.get('version_code', 0),
            file_size=s.get('file_size', 0),
            file_hash=s.get('file_hash', ''),
            updated_at=s.get('updated_at', '')
        ) for s in data.get('scripts', [])]

        return ScriptVersionResponse(
            scripts=scripts,
            total_count=data.get('total_count', len(scripts)),
            last_updated=data.get('last_updated', '')
        )

    def download_script(self, filename: str, save_path: Optional[str] = None) -> bytes:
        """
        下载指定脚本文件

        Args:
            filename: 脚本文件名
            save_path: 保存路径（如果为空，返回内容而不保存）

        Returns:
            脚本内容
        """
        url = f"{self.client.server_url}/api/client/scripts/{quote(filename)}"
        resp = _get_with_client_auth(self.client, url)
        try:
            if resp.status_code != 200:
                try:
                    result = resp.json()
                    raise Exception(result.get('message', f'下载失败: HTTP {resp.status_code}'))
                except Exception:
                    raise Exception(f'下载失败: HTTP {resp.status_code}')

            content = resp.content
        finally:
            resp.close()

        # 如果指定了保存路径，保存到文件
        if save_path:
            _ensure_parent_dir(save_path)
            temp_path = f"{save_path}.part"
            _remove_if_exists(temp_path)
            try:
                with open(temp_path, 'wb') as f:
                    f.write(content)
                _atomic_replace(temp_path, save_path)
            except Exception:
                _remove_if_exists(temp_path)
                raise

        return content

    def check_script_update(self, filename: str, current_version_code: int) -> Tuple[bool, Optional[ScriptInfo]]:
        """
        检查脚本是否有更新

        Args:
            filename: 脚本文件名
            current_version_code: 当前版本号

        Returns:
            (是否有更新, 最新版本信息)
        """
        versions = self.get_script_versions()

        for script in versions.scripts:
            if script.filename == filename:
                if script.version_code > current_version_code:
                    return True, script
                return False, script

        raise Exception(f"脚本 {filename} 不存在")


class ReleaseManager:
    """版本发布管理器"""

    def __init__(self, license_client):
        """
        初始化版本发布管理器

        Args:
            license_client: LicenseClient 实例
        """
        self.client = license_client

    def download_release(
        self,
        filename: str,
        save_path: str,
        progress_callback: Optional[Callable[[int, int], None]] = None
    ) -> None:
        """
        下载版本文件

        Args:
            filename: 文件名
            save_path: 保存路径
            progress_callback: 下载进度回调 (已下载字节数, 总字节数)
        """
        url = f"{self.client.server_url}/api/client/releases/download/{quote(filename)}"
        resp = _get_with_client_auth(self.client, url, stream=True)
        try:
            if resp.status_code != 200:
                try:
                    result = resp.json()
                    raise Exception(result.get('message', f'下载失败: HTTP {resp.status_code}'))
                except Exception:
                    raise Exception(f'下载失败: HTTP {resp.status_code}')

            _write_response_to_file(resp, save_path, progress_callback)
        finally:
            resp.close()

    def get_latest_release_and_download(
        self,
        save_path: str,
        progress_callback: Optional[Callable[[int, int], None]] = None
    ) -> UpdateInfo:
        """
        获取最新版本并下载

        Args:
            save_path: 保存路径
            progress_callback: 下载进度回调

        Returns:
            更新信息
        """
        # 获取最新版本信息
        update_info = self.client.check_update()
        if not update_info:
            raise Exception("没有可用的更新")

        # 从 download_url 提取文件名（忽略 query 参数）
        download_url = update_info.get('download_url', '')
        if not download_url:
            raise Exception("无效的下载URL")

        absolute_url = download_url if download_url.startswith(('http://', 'https://')) else urljoin(self.client.server_url, download_url)
        try:
            self._download_url(absolute_url, save_path, progress_callback)
        except Exception as e:
            if not _is_download_auth_error(e):
                raise
            refreshed = self.client.check_update()
            refreshed_url = (refreshed or {}).get('download_url', '')
            if not refreshed_url:
                raise Exception("下载链接已过期，未找到可用的新链接")
            update_info = refreshed
            absolute_url = refreshed_url if refreshed_url.startswith(('http://', 'https://')) else urljoin(self.client.server_url, refreshed_url)
            self._download_url(absolute_url, save_path, progress_callback)
        self._verify_downloaded_release(save_path, update_info)

        return UpdateInfo(
            version=update_info.get('version', ''),
            version_code=update_info.get('version_code', 0),
            download_url=update_info.get('download_url', download_url),
            changelog=update_info.get('changelog', ''),
            file_size=update_info.get('file_size', 0),
            file_hash=update_info.get('file_hash', ''),
            file_signature=update_info.get('file_signature', ''),
            signature_alg=update_info.get('signature_alg', ''),
            force_update=update_info.get('force_update', False)
        )

    def _verify_downloaded_release(self, file_path: str, update_info: Dict) -> None:
        hasher = hashlib.sha256()
        file_size = 0
        with open(file_path, 'rb') as f:
            while True:
                chunk = f.read(64 * 1024)
                if not chunk:
                    break
                hasher.update(chunk)
                file_size += len(chunk)

        file_hash = hasher.hexdigest()
        expected_hash = update_info.get('file_hash', '')
        if expected_hash and file_hash.lower() != expected_hash.lower():
            raise Exception(f"文件校验失败: 期望 {expected_hash}, 实际 {file_hash}")

        self._verify_release_signature(update_info, file_hash, file_size)

    def _verify_release_signature(self, update_info: Dict, file_hash: str, file_size: int) -> None:
        file_signature = update_info.get('file_signature', '')
        signature_alg = update_info.get('signature_alg', '')

        if not file_signature:
            if getattr(self.client, 'require_signature', False):
                raise Exception("缺少文件签名")
            return

        if signature_alg and signature_alg.upper() != 'RSA-SHA256':
            raise Exception(f"不支持的签名算法: {signature_alg}")

        if not getattr(self.client, '_public_key', None):
            if getattr(self.client, 'require_signature', False):
                raise Exception("未配置公钥，无法验证文件签名")
            return

        payload = f"{file_hash.lower()}:{file_size}".encode()
        self.client._verify_signature(payload, file_signature)

    def _download_url(
        self,
        url: str,
        save_path: str,
        progress_callback: Optional[Callable[[int, int], None]] = None
    ) -> None:
        resp = self.client._session.get(url, stream=True, timeout=self.client.timeout)
        try:
            if resp.status_code != 200:
                try:
                    result = resp.json()
                    message = result.get('message', f'下载失败: HTTP {resp.status_code}')
                except Exception:
                    message = f'下载失败: HTTP {resp.status_code}'
                raise Exception(message)

            _write_response_to_file(resp, save_path, progress_callback)
        finally:
            resp.close()


def _write_response_to_file(
    resp,
    save_path: str,
    progress_callback: Optional[Callable[[int, int], None]] = None
) -> None:
    _ensure_parent_dir(save_path)
    temp_path = f"{save_path}.part"
    _remove_if_exists(temp_path)

    try:
        total_size = int(resp.headers.get('content-length', 0))
        with open(temp_path, 'wb') as f:
            downloaded = 0
            for chunk in resp.iter_content(chunk_size=32 * 1024):
                if chunk:
                    f.write(chunk)
                    downloaded += len(chunk)
                    if progress_callback and total_size > 0:
                        progress_callback(downloaded, total_size)
        _atomic_replace(temp_path, save_path)
    except Exception:
        _remove_if_exists(temp_path)
        raise


def _is_download_auth_error(error: Exception) -> bool:
    message = str(error)
    return 'HTTP 401' in message or 'HTTP 403' in message or '下载令牌' in message
