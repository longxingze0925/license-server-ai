"""
热更新模块
提供完整的热更新功能，包括检查更新、下载、安装、回滚等

安全特性：
- 支持 HTTPS 和证书固定（继承自 LicenseClient）
- 文件哈希校验
- 自动备份和回滚

使用示例：
    from license_client import LicenseClient
    from hotupdate import HotUpdateManager

    client = LicenseClient(
        server_url="https://192.168.1.100:8080",
        app_key="your_app_key",
        cert_fingerprint="SHA256:AB:CD:EF:..."  # 证书固定
    )

    # 创建热更新管理器
    updater = HotUpdateManager(client, current_version="1.0.0")

    # 检查更新
    update_info = updater.check_update()
    if update_info and update_info.get('has_update'):
        print(f"发现新版本: {update_info['to_version']}")

        # 下载更新
        update_file = updater.download_update(update_info)

        # 应用更新
        updater.apply_update(update_info, update_file, target_dir="./app")
"""

import os
import json
import hashlib
import shutil
import zipfile
import threading
import time
from pathlib import Path
from typing import Optional, Dict, Callable, List
from enum import Enum
from urllib.parse import urljoin

try:
    import requests
except ImportError:
    raise ImportError("请安装 requests 库: pip install requests")


class HotUpdateStatus(Enum):
    """更新状态"""
    PENDING = "pending"
    DOWNLOADING = "downloading"
    INSTALLING = "installing"
    SUCCESS = "success"
    FAILED = "failed"
    ROLLBACK = "rollback"


class HotUpdateError(Exception):
    """热更新错误"""
    pass


class HotUpdateManager:
    """热更新管理器"""

    def __init__(
        self,
        client,  # LicenseClient 实例
        current_version: str,
        update_dir: Optional[str] = None,
        backup_dir: Optional[str] = None,
        auto_check: bool = False,
        check_interval: int = 3600,
        callback: Optional[Callable[[HotUpdateStatus, float, Optional[Exception]], None]] = None
    ):
        """
        初始化热更新管理器

        Args:
            client: LicenseClient 实例
            current_version: 当前版本号
            update_dir: 更新文件存放目录
            backup_dir: 备份目录
            auto_check: 是否自动检查更新
            check_interval: 自动检查间隔（秒）
            callback: 更新状态回调函数
        """
        self.client = client
        self.current_version = current_version
        self.update_dir = update_dir or os.path.join(Path.home(), '.app_updates')
        self.backup_dir = backup_dir or os.path.join(Path.home(), '.app_backups')
        self.auto_check = auto_check
        self.check_interval = check_interval
        self.callback = callback

        self._latest_update: Optional[Dict] = None
        self._is_updating = False
        self._stop_auto_check = False
        self._auto_check_thread: Optional[threading.Thread] = None
        self._lock = threading.Lock()

        # 确保目录存在
        os.makedirs(self.update_dir, exist_ok=True)
        os.makedirs(self.backup_dir, exist_ok=True)

    def check_update(self) -> Optional[Dict]:
        """
        检查更新

        Returns:
            更新信息字典，如果没有更新返回 None
        """
        try:
            data = self.client._request_with_client_auth(
                'GET',
                '/hotupdate/check',
                {'version': self.current_version}
            ) or {}

            with self._lock:
                self._latest_update = data

            return data if data.get('has_update') else None

        except requests.exceptions.RequestException as e:
            raise HotUpdateError(f"网络请求失败: {e}")

    def get_latest_update(self) -> Optional[Dict]:
        """获取最新的更新信息（从缓存）"""
        with self._lock:
            return self._latest_update

    def download_update(
        self,
        update_info: Dict,
        progress_callback: Optional[Callable[[int, int], None]] = None
    ) -> str:
        """
        下载更新

        Args:
            update_info: 更新信息
            progress_callback: 下载进度回调 (downloaded_bytes, total_bytes)

        Returns:
            下载的文件路径
        """
        if not update_info or not update_info.get('has_update'):
            raise HotUpdateError("没有可用的更新")

        with self._lock:
            if self._is_updating:
                raise HotUpdateError("正在更新中")
            self._is_updating = True

        try:
            # 上报下载状态
            self._report_status(update_info.get('id'), HotUpdateStatus.DOWNLOADING)
            self._notify_callback(HotUpdateStatus.DOWNLOADING, 0)

            # 构建下载URL
            download_url = update_info['download_url']
            if not download_url.startswith(('http://', 'https://')):
                download_url = urljoin(self.client.server_url, download_url)

            resp, update_info = self._open_download_response(update_info, download_url)

            file_path = ""
            temp_path = ""
            try:
                total_size = int(resp.headers.get('content-length', 0))

                # 创建文件
                filename = f"update_{update_info.get('from_version', 'unknown')}_to_{update_info['to_version']}.zip"
                file_path = os.path.join(self.update_dir, filename)
                temp_path = f"{file_path}.part"
                try:
                    os.remove(temp_path)
                except FileNotFoundError:
                    pass

                downloaded = 0
                hash_obj = hashlib.sha256()

                with open(temp_path, 'wb') as f:
                    for chunk in resp.iter_content(chunk_size=32 * 1024):
                        if chunk:
                            f.write(chunk)
                            hash_obj.update(chunk)
                            downloaded += len(chunk)

                            if progress_callback:
                                progress_callback(downloaded, total_size)

                            if total_size > 0:
                                progress = downloaded / total_size
                                self._notify_callback(HotUpdateStatus.DOWNLOADING, progress)
            except Exception:
                if temp_path:
                    self._remove_file_if_exists(temp_path)
                raise
            finally:
                resp.close()

            # 验证哈希
            file_hash = hash_obj.hexdigest()
            expected_hash = update_info.get('file_hash', '')

            if expected_hash and file_hash != expected_hash:
                self._remove_file_if_exists(temp_path)
                error = HotUpdateError("文件校验失败")
                raise error

            try:
                self._verify_update_signature(update_info, file_hash, downloaded)
                os.replace(temp_path, file_path)
            except Exception:
                self._remove_file_if_exists(temp_path)
                raise

            self._notify_callback(HotUpdateStatus.DOWNLOADING, 1)
            return file_path

        except requests.exceptions.RequestException as e:
            error = HotUpdateError(f"下载失败: {e}")
            self._report_status(update_info.get('id'), HotUpdateStatus.FAILED, str(error))
            self._notify_callback(HotUpdateStatus.FAILED, 0, error)
            raise error
        except Exception as e:
            error = e if isinstance(e, HotUpdateError) else HotUpdateError(f"下载失败: {e}")
            self._report_status(update_info.get('id'), HotUpdateStatus.FAILED, str(error))
            self._notify_callback(HotUpdateStatus.FAILED, 0, error)
            raise error

        finally:
            with self._lock:
                self._is_updating = False

    def _open_download_response(self, update_info: Dict, download_url: str):
        session = getattr(self.client, '_session', None) or requests
        resp = session.get(download_url, stream=True, timeout=300)
        if resp.status_code not in (401, 403):
            resp.raise_for_status()
            return resp, update_info

        resp.close()
        refreshed = self.check_update()
        if (
            not refreshed
            or refreshed.get('id') != update_info.get('id')
            or not refreshed.get('download_url')
        ):
            raise HotUpdateError("下载链接已过期，未找到可用的新链接")

        refreshed_url = refreshed['download_url']
        if not refreshed_url.startswith(('http://', 'https://')):
            refreshed_url = urljoin(self.client.server_url, refreshed_url)

        resp = session.get(refreshed_url, stream=True, timeout=300)
        resp.raise_for_status()
        return resp, refreshed

    def apply_update(
        self,
        update_info: Dict,
        update_file: str,
        target_dir: str,
        pre_update_hook: Optional[Callable[[], bool]] = None,
        post_update_hook: Optional[Callable[[], bool]] = None
    ) -> bool:
        """
        应用更新

        Args:
            update_info: 更新信息
            update_file: 更新文件路径
            target_dir: 目标目录
            pre_update_hook: 更新前钩子，返回 False 取消更新
            post_update_hook: 更新后钩子，返回 False 触发回滚

        Returns:
            是否成功
        """
        if not update_info:
            raise HotUpdateError("更新信息为空")

        # 执行更新前钩子
        if pre_update_hook and not pre_update_hook():
            raise HotUpdateError("更新前检查失败")

        # 上报安装状态
        self._report_status(update_info.get('id'), HotUpdateStatus.INSTALLING)
        self._notify_callback(HotUpdateStatus.INSTALLING, 0)

        # 备份当前版本
        backup_path = os.path.join(
            self.backup_dir,
            f"backup_{self.current_version}_{int(time.time())}"
        )

        try:
            self._backup_current_version(target_dir, backup_path)
        except Exception as e:
            error = HotUpdateError(f"备份失败: {e}")
            self._report_status(update_info.get('id'), HotUpdateStatus.FAILED, str(error))
            self._notify_callback(HotUpdateStatus.FAILED, 0, error)
            raise error

        # 解压更新包
        try:
            self._extract_update(update_file, target_dir)
        except Exception as e:
            # 回滚
            try:
                self._rollback(backup_path, target_dir)
                error = HotUpdateError(f"解压失败: {e}")
            except Exception as rollback_error:
                error = HotUpdateError(f"解压失败: {e}; 回滚也失败: {rollback_error}")
            self._report_status(update_info.get('id'), HotUpdateStatus.FAILED, str(error))
            self._notify_callback(HotUpdateStatus.FAILED, 0, error)
            raise error

        # 执行更新后钩子
        if post_update_hook and not post_update_hook():
            # 回滚
            try:
                self._rollback(backup_path, target_dir)
                error = HotUpdateError("更新后检查失败，已回滚")
            except Exception as rollback_error:
                error = HotUpdateError(f"更新后检查失败，回滚也失败: {rollback_error}")
            self._report_status(update_info.get('id'), HotUpdateStatus.ROLLBACK, str(error))
            self._notify_callback(HotUpdateStatus.ROLLBACK, 0, error)
            raise error

        # 更新成功
        self.current_version = update_info['to_version']
        self._report_status(update_info.get('id'), HotUpdateStatus.SUCCESS)
        self._notify_callback(HotUpdateStatus.SUCCESS, 1)

        # 清理下载的更新包
        try:
            os.remove(update_file)
        except:
            pass

        # 清理旧备份
        self._clean_old_backups(keep=3)

        return True

    def rollback(self, target_dir: str) -> bool:
        """
        回滚到上一个版本

        Args:
            target_dir: 目标目录

        Returns:
            是否成功
        """
        # 查找最新的备份
        backups = []
        for entry in os.scandir(self.backup_dir):
            if entry.is_dir():
                backups.append((entry.path, entry.stat().st_mtime))

        if not backups:
            raise HotUpdateError("没有可用的备份")

        # 按时间排序，获取最新的
        backups.sort(key=lambda x: x[1], reverse=True)
        latest_backup = backups[0][0]

        return self._rollback(latest_backup, target_dir)

    def start_auto_check(self):
        """启动自动检查更新"""
        if not self.auto_check:
            return

        self._stop_auto_check = False

        def check_loop():
            # 立即检查一次
            try:
                self.check_update()
            except:
                pass

            while not self._stop_auto_check:
                time.sleep(self.check_interval)
                if not self._stop_auto_check:
                    try:
                        self.check_update()
                    except:
                        pass

        self._auto_check_thread = threading.Thread(target=check_loop, daemon=True)
        self._auto_check_thread.start()

    def stop_auto_check(self):
        """停止自动检查更新"""
        self._stop_auto_check = True

    def get_update_history(self) -> List[Dict]:
        """获取更新历史"""
        try:
            return self.client._request_with_client_auth('GET', '/hotupdate/history') or []

        except:
            return []

    def is_updating(self) -> bool:
        """是否正在更新"""
        with self._lock:
            return self._is_updating

    def get_current_version(self) -> str:
        """获取当前版本"""
        return self.current_version

    def set_current_version(self, version: str):
        """设置当前版本"""
        self.current_version = version

    # 内部方法

    def _report_status(
        self,
        hot_update_id: Optional[str],
        status: HotUpdateStatus,
        error_msg: str = ""
    ):
        """上报更新状态"""
        if not hot_update_id:
            return

        try:
            data = {
                "hot_update_id": hot_update_id,
                "from_version": self.current_version,
                "status": status.value
            }
            if error_msg:
                data["error_message"] = error_msg

            # 异步上报
            threading.Thread(
                target=lambda: self.client._request_with_client_auth('POST', '/hotupdate/report', data),
                daemon=True
            ).start()
        except:
            pass

    def _notify_callback(
        self,
        status: HotUpdateStatus,
        progress: float,
        error: Optional[Exception] = None
    ):
        """通知回调"""
        if self.callback:
            try:
                self.callback(status, progress, error)
            except:
                pass

    @staticmethod
    def _remove_file_if_exists(file_path: str):
        try:
            os.remove(file_path)
        except FileNotFoundError:
            pass

    def _verify_update_signature(self, update_info: Dict, file_hash: str, file_size: int):
        file_signature = update_info.get('file_signature', '')
        signature_alg = update_info.get('signature_alg', '')

        if not file_signature:
            if getattr(self.client, 'require_signature', False):
                raise HotUpdateError("缺少文件签名")
            return

        if signature_alg and signature_alg.upper() != 'RSA-SHA256':
            raise HotUpdateError(f"不支持的签名算法: {signature_alg}")

        if not getattr(self.client, '_public_key', None):
            if getattr(self.client, 'require_signature', False):
                raise HotUpdateError("未配置公钥，无法验证文件签名")
            return

        payload = f"{file_hash.lower()}:{file_size}".encode()
        try:
            self.client._verify_signature(payload, file_signature)
        except Exception as e:
            raise HotUpdateError(f"文件签名验证失败: {e}")

    def _backup_current_version(self, source_dir: str, backup_path: str):
        """备份当前版本"""
        if os.path.exists(source_dir):
            shutil.copytree(source_dir, backup_path)

    def _extract_update(self, zip_file: str, target_dir: str):
        """解压更新包"""
        # 确保目标目录存在
        os.makedirs(target_dir, exist_ok=True)

        # 检查是否是 zip 文件
        if zipfile.is_zipfile(zip_file):
            with zipfile.ZipFile(zip_file, 'r') as zf:
                self._safe_extract_zip(zf, target_dir)
        else:
            # 如果不是 zip，尝试直接复制
            if os.path.isdir(zip_file):
                shutil.copytree(zip_file, target_dir, dirs_exist_ok=True)
            else:
                shutil.copy2(zip_file, target_dir)

    def _safe_extract_zip(self, zf: zipfile.ZipFile, target_dir: str):
        """安全解压 zip，防止路径穿越写出目标目录"""
        target_root = os.path.abspath(target_dir)
        target_prefix = target_root + os.sep

        for member in zf.infolist():
            member_path = os.path.abspath(os.path.join(target_root, member.filename))
            if member_path != target_root and not member_path.startswith(target_prefix):
                raise HotUpdateError(f"更新包包含非法路径: {member.filename}")

            zf.extract(member, target_root)

    def _rollback(self, backup_path: str, target_dir: str) -> bool:
        """回滚"""
        try:
            if not os.path.isdir(backup_path):
                raise HotUpdateError("备份目录不存在")

            parent = os.path.dirname(os.path.abspath(target_dir)) or "."
            os.makedirs(parent, exist_ok=True)

            restore_path = f"{target_dir}.rollback"
            failed_path = f"{target_dir}.failed_{int(time.time() * 1000)}"
            if os.path.exists(restore_path):
                shutil.rmtree(restore_path)
            if os.path.exists(failed_path):
                shutil.rmtree(failed_path)
            shutil.copytree(backup_path, restore_path)

            target_existed = os.path.exists(target_dir)
            if target_existed:
                os.rename(target_dir, failed_path)
            try:
                os.rename(restore_path, target_dir)
            except Exception:
                if target_existed and os.path.exists(failed_path):
                    os.rename(failed_path, target_dir)
                raise

            if target_existed and os.path.exists(failed_path):
                shutil.rmtree(failed_path)
            return True
        except Exception as e:
            try:
                if 'restore_path' in locals() and os.path.exists(restore_path):
                    shutil.rmtree(restore_path)
            except:
                pass
            raise HotUpdateError(f"回滚失败: {e}")

    def _clean_old_backups(self, keep: int = 3):
        """清理旧备份"""
        try:
            backups = []
            for entry in os.scandir(self.backup_dir):
                if entry.is_dir():
                    backups.append((entry.path, entry.stat().st_mtime))

            if len(backups) <= keep:
                return

            # 按时间排序
            backups.sort(key=lambda x: x[1])

            # 删除旧的备份
            for backup_path, _ in backups[:-keep]:
                shutil.rmtree(backup_path)
        except:
            pass


# 便捷函数

def check_and_update(
    client,
    current_version: str,
    target_dir: str,
    auto_apply: bool = False,
    callback: Optional[Callable[[HotUpdateStatus, float, Optional[Exception]], None]] = None
) -> Optional[Dict]:
    """
    检查并更新（便捷函数）

    Args:
        client: LicenseClient 实例
        current_version: 当前版本
        target_dir: 目标目录
        auto_apply: 是否自动应用更新
        callback: 状态回调

    Returns:
        更新信息，如果没有更新返回 None
    """
    manager = HotUpdateManager(client, current_version, callback=callback)

    update_info = manager.check_update()

    if not update_info or not update_info.get('has_update'):
        return None

    if auto_apply or update_info.get('force_update'):
        update_file = manager.download_update(update_info)
        manager.apply_update(update_info, update_file, target_dir)

    return update_info
