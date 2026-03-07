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
from typing import Optional, Dict, List, Callable, Tuple
from dataclasses import dataclass
from urllib.parse import quote, urlparse


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
        url = f"{self.client.server_url}/api/client/scripts/version"
        params = {"app_key": self.client.app_key}

        resp = self.client._session.get(url, params=params, timeout=self.client.timeout)
        result = resp.json()

        if result.get('code') != 0:
            raise Exception(result.get('message', '获取脚本版本失败'))

        data = result.get('data', {})
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
        params = {
            "app_key": self.client.app_key,
            "machine_id": self.client.machine_id
        }

        resp = self.client._session.get(url, params=params, timeout=self.client.timeout)

        if resp.status_code != 200:
            try:
                result = resp.json()
                raise Exception(result.get('message', f'下载失败: HTTP {resp.status_code}'))
            except:
                raise Exception(f'下载失败: HTTP {resp.status_code}')

        content = resp.content

        # 如果指定了保存路径，保存到文件
        if save_path:
            # 确保目录存在
            dir_path = os.path.dirname(save_path)
            if dir_path:
                os.makedirs(dir_path, exist_ok=True)

            with open(save_path, 'wb') as f:
                f.write(content)

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
        params = {
            "app_key": self.client.app_key,
            "machine_id": self.client.machine_id
        }

        resp = self.client._session.get(url, params=params, stream=True, timeout=self.client.timeout)

        if resp.status_code != 200:
            try:
                result = resp.json()
                raise Exception(result.get('message', f'下载失败: HTTP {resp.status_code}'))
            except:
                raise Exception(f'下载失败: HTTP {resp.status_code}')

        # 确保目录存在
        dir_path = os.path.dirname(save_path)
        if dir_path:
            os.makedirs(dir_path, exist_ok=True)

        # 获取文件大小
        total_size = int(resp.headers.get('content-length', 0))

        # 下载文件
        with open(save_path, 'wb') as f:
            downloaded = 0
            for chunk in resp.iter_content(chunk_size=32 * 1024):
                if chunk:
                    f.write(chunk)
                    downloaded += len(chunk)
                    if progress_callback and total_size > 0:
                        progress_callback(downloaded, total_size)

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

        filename = os.path.basename(urlparse(download_url).path)
        if not filename or filename == '.':
            raise Exception("无效的下载URL")

        # 下载文件
        self.download_release(filename, save_path, progress_callback)

        return UpdateInfo(
            version=update_info.get('version', ''),
            version_code=update_info.get('version_code', 0),
            download_url=download_url,
            changelog=update_info.get('changelog', ''),
            file_size=update_info.get('file_size', 0),
            file_hash=update_info.get('file_hash', ''),
            force_update=update_info.get('force_update', False)
        )
