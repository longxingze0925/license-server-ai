"""
数据同步模块
支持将本地数据同步到云端服务器

功能特性：
- 增量同步支持
- 冲突检测和解决
- 批量操作
- 自动同步管理器

使用示例：
    from license_client import LicenseClient
    from data_sync import DataSyncClient

    # 初始化
    client = LicenseClient(server_url, app_key, skip_verify=True)
    sync_client = DataSyncClient(client)

    # 获取表列表
    tables = sync_client.get_table_list()

    # 拉取数据
    records, server_time = sync_client.pull_table("my_table")

    # 推送数据
    result = sync_client.push_record("my_table", "record_id", {"name": "test"})
"""

import json
import re
import time
import threading
import hashlib
from typing import Optional, Dict, List, Any, Callable, Tuple
from dataclasses import dataclass, field
from enum import Enum
from datetime import datetime, timezone


class ConflictResolution(Enum):
    """冲突解决策略"""
    USE_LOCAL = "use_local"
    USE_SERVER = "use_server"
    MERGE = "merge"


@dataclass
class SyncRecord:
    """同步记录"""
    id: str
    data: Dict[str, Any]
    version: int = 0
    is_deleted: bool = False
    updated_at: int = 0


@dataclass
class SyncResult:
    """同步结果"""
    record_id: str
    status: str  # success, conflict, error
    version: int = 0
    server_version: int = 0
    data_key: str = ""
    conflict_id: str = ""
    error: str = ""
    conflict_data: Any = None


@dataclass
class TableInfo:
    """表信息"""
    table_name: str
    record_count: int = 0
    last_updated: str = ""


@dataclass
class SyncChange:
    """同步变更记录"""
    id: str
    table: str
    record_id: str
    operation: str  # insert, update, delete
    data: Dict[str, Any]
    version: int = 0
    change_time: int = 0
    data_type: str = ""
    data_key: str = ""
    action: str = ""  # create, update, delete
    local_version: int = 0
    updated_at: int = 0


@dataclass
class SyncStatus:
    """同步状态"""
    last_sync_time: int = 0
    pending_changes: int = 0
    table_status: Dict[str, int] = field(default_factory=dict)
    server_time: int = 0


@dataclass
class SyncChangesPage:
    """一页同步变更"""
    changes: List[SyncChange] = field(default_factory=list)
    server_time: int = 0
    has_more: bool = False
    next_offset: int = 0
    limit: int = 0
    offset: int = 0


@dataclass
class ConfigData:
    """配置数据"""
    key: str
    value: Any
    version: int = 0
    updated_at: int = 0


@dataclass
class WorkflowData:
    """工作流数据"""
    id: str
    name: str
    config: Dict[str, Any] = field(default_factory=dict)
    enabled: bool = True
    version: int = 0
    updated_at: int = 0


@dataclass
class MaterialData:
    """素材数据"""
    id: str
    type: str
    content: str
    tags: str = ""
    material_id: int = 0
    version: int = 0
    updated_at: int = 0


# 数据类型常量
DATA_TYPE_CONFIG = "config"
DATA_TYPE_WORKFLOW = "workflow"
DATA_TYPE_BATCH_TASK = "batch_task"
DATA_TYPE_MATERIAL = "material"
DATA_TYPE_POST = "post"
DATA_TYPE_COMMENT = "comment"
DATA_TYPE_COMMENT_SCRIPT = "comment_script"
DATA_TYPE_VOICE_CONFIG = "voice_config"
DATA_TYPE_SCRIPTS = "scripts"  # 话术管理
DATA_TYPE_DANMAKU_GROUPS = "danmaku_groups"  # 互动规则
DATA_TYPE_AI_CONFIG = "ai_config"  # AI配置
DATA_TYPE_RANDOM_WORD_AI_CONFIG = "random_word_ai_config"  # 随机词AI配置


@dataclass
class BackupData:
    """备份数据"""
    id: str = ""
    data_type: str = ""
    data_json: str = ""
    version: int = 0
    device_name: str = ""
    machine_id: str = ""
    is_current: bool = False
    data_size: int = 0
    item_count: int = 0
    checksum: str = ""
    created_at: str = ""
    updated_at: str = ""


@dataclass
class PostData:
    """帖子数据"""
    id: str
    content: str
    status: str = "draft"
    group_id: str = ""
    group_name: str = ""
    post_type: str = ""
    post_link: str = ""
    version: int = 0
    updated_at: int = 0


@dataclass
class CommentScriptData:
    """评论话术数据"""
    id: str
    content: str
    category: str = ""
    version: int = 0
    updated_at: int = 0


@dataclass
class PostGroup:
    """帖子分组"""
    id: str
    name: str
    count: int = 0


class DataSyncClient:
    """数据同步客户端"""

    _sync_identifier_pattern = re.compile(r'^[A-Za-z0-9_.-]+$')

    def __init__(self, license_client):
        """
        初始化数据同步客户端

        Args:
            license_client: LicenseClient 实例
        """
        self.client = license_client
        self.last_sync_time: Dict[str, int] = {}

    @classmethod
    def _validate_sync_identifier(cls, field: str, value: str) -> str:
        normalized = str(value or '').strip()
        if not normalized:
            raise ValueError(f"{field}不能为空")
        if len(normalized) > 100:
            raise ValueError(f"{field}长度不能超过100字符")
        if not cls._sync_identifier_pattern.match(normalized):
            raise ValueError(f"{field}只能包含字母、数字、下划线、点和横线")
        return normalized

    def _request(self, method: str, endpoint: str, data: Optional[Dict] = None, params: Optional[Dict] = None) -> Dict:
        """发送需要客户端会话令牌的 HTTP 请求"""
        payload = dict(data or {})
        if params:
            payload.update(params)
        try:
            return self.client._request_with_client_auth(method, endpoint, payload or None)
        except Exception as e:
            raise Exception(f"请求失败: {e}")

    # ==================== 基础同步功能 ====================

    def get_table_list(self) -> List[TableInfo]:
        """获取服务器上的所有表名"""
        data = self._request('GET', '/sync/tables')
        return [TableInfo(
            table_name=t.get('table_name', ''),
            record_count=t.get('record_count', 0),
            last_updated=t.get('last_updated', '')
        ) for t in data] if isinstance(data, list) else []

    def pull_table(self, table_name: str, since: int = 0) -> Tuple[List[SyncRecord], int]:
        """
        从服务器拉取指定表的数据

        Args:
            table_name: 表名
            since: 增量同步时间戳（0表示全量）

        Returns:
            (记录列表, 服务器时间)
        """
        table_name = self._validate_sync_identifier("table_name", table_name)
        params = {"table": table_name}
        if since > 0:
            params["since"] = str(since)

        data = self._request('GET', '/sync/table', params=params)
        records = [SyncRecord(
            id=r.get('id', ''),
            data=r.get('data', {}),
            version=r.get('version', 0),
            is_deleted=r.get('is_deleted', False),
            updated_at=r.get('updated_at', 0)
        ) for r in data.get('records', [])]

        server_time = data.get('server_time', 0)
        self.last_sync_time[table_name] = server_time

        return records, server_time

    def pull_all_tables(self, since: int = 0) -> Tuple[Dict[str, List[SyncRecord]], int]:
        """
        从服务器拉取所有表的数据

        Args:
            since: 增量同步时间戳（0表示全量）

        Returns:
            (表名到记录列表的映射, 服务器时间)
        """
        params = {}
        if since > 0:
            params["since"] = str(since)

        data = self._request('GET', '/sync/tables/all', params=params)
        tables = {}
        for table_name, records in data.get('tables', {}).items():
            tables[table_name] = [SyncRecord(
                id=r.get('id', ''),
                data=r.get('data', {}),
                version=r.get('version', 0),
                is_deleted=r.get('is_deleted', False),
                updated_at=r.get('updated_at', 0)
            ) for r in records]

        return tables, data.get('server_time', 0)

    def push_record(self, table_name: str, record_id: str, data: Dict[str, Any], version: int = 0) -> SyncResult:
        """
        推送单条记录到服务器

        Args:
            table_name: 表名
            record_id: 记录ID
            data: 记录数据
            version: 版本号（用于冲突检测）

        Returns:
            同步结果
        """
        table_name = self._validate_sync_identifier("table_name", table_name)
        record_id = self._validate_sync_identifier("record_id", record_id)
        req_data = {
            "table": table_name,
            "record_id": record_id,
            "data": data,
            "version": version
        }
        result = self._request('POST', '/sync/table', req_data)
        return SyncResult(
            record_id=record_id,
            status=result.get('status', 'error'),
            version=result.get('version', 0),
            server_version=result.get('server_version', 0)
        )

    def push_record_batch(self, table_name: str, records: List[Dict[str, Any]]) -> List[SyncResult]:
        """
        批量推送记录到服务器

        Args:
            table_name: 表名
            records: 记录列表，每条记录包含 record_id, data, version, deleted

        Returns:
            同步结果列表
        """
        table_name = self._validate_sync_identifier("table_name", table_name)
        normalized_records = []
        for record in records:
            item = dict(record)
            item['record_id'] = self._validate_sync_identifier("record_id", item.get('record_id', ''))
            normalized_records.append(item)
        req_data = {
            "table": table_name,
            "records": normalized_records
        }
        result = self._request('POST', '/sync/table/batch', req_data)
        return [SyncResult(
            record_id=r.get('record_id', ''),
            status=r.get('status', 'error'),
            version=r.get('version', 0),
            server_version=r.get('server_version', 0)
        ) for r in result.get('results', [])]

    def delete_record(self, table_name: str, record_id: str) -> bool:
        """删除服务器上的记录"""
        table_name = self._validate_sync_identifier("table_name", table_name)
        record_id = self._validate_sync_identifier("record_id", record_id)
        req_data = {
            "table": table_name,
            "record_id": record_id
        }
        self._request('DELETE', '/sync/table', req_data)
        return True

    # ==================== 高级同步功能 ====================

    @staticmethod
    def _normalize_sync_result(item: Dict[str, Any], fallback_key: str = "") -> SyncResult:
        data_key = item.get('data_key') or item.get('record_id') or fallback_key
        return SyncResult(
            record_id=item.get('record_id') or data_key,
            data_key=data_key,
            status=item.get('status', 'error'),
            version=item.get('version') or item.get('server_version', 0),
            server_version=item.get('server_version', 0),
            conflict_id=item.get('conflict_id', ''),
            error=item.get('error', ''),
            conflict_data=item.get('conflict_data')
        )

    @staticmethod
    def _change_to_push_item(change: SyncChange) -> Dict[str, Any]:
        data_type = change.data_type or change.table
        data_key = change.data_key or change.record_id or change.id
        action = change.action
        if not action:
            if change.operation in ('insert', 'create'):
                action = 'create'
            elif change.operation == 'delete':
                action = 'delete'
            else:
                action = 'update'
        return {
            "data_type": data_type,
            "data_key": data_key,
            "action": action,
            "data": change.data,
            "local_version": change.local_version or change.version
        }

    def push_changes(self, changes: List[SyncChange]) -> List[SyncResult]:
        """
        推送客户端变更到服务端（Push）

        Args:
            changes: 变更列表

        Returns:
            同步结果列表
        """
        req_data = {
            "items": [self._change_to_push_item(c) for c in changes]
        }
        result = self._request('POST', '/sync/push', req_data)
        return [self._normalize_sync_result(r) for r in result.get('results', [])]

    def get_changes(self, since: int = 0, tables: Optional[List[str]] = None) -> Tuple[List[SyncChange], int]:
        """
        获取服务端变更（Pull）

        Args:
            since: 从指定时间戳开始获取变更
            tables: 兼容旧参数名，实际表示服务端 data_type 列表；为空则获取所有支持的数据类型

        Returns:
            (变更列表, 服务器时间)
        """
        if tables and len(tables) > 1:
            all_changes: List[SyncChange] = []
            latest_server_time = 0
            for data_type in tables:
                changes, server_time = self.get_changes(since, [data_type])
                all_changes.extend(changes)
                latest_server_time = max(latest_server_time, server_time)
            return all_changes, latest_server_time

        params = {}
        if since > 0:
            params["since"] = str(since)
        if tables:
            params["data_type"] = tables[0]

        all_changes: List[SyncChange] = []
        latest_server_time = 0
        offset = 0
        while True:
            if offset > 0:
                params["offset"] = str(offset)
            page = self.get_changes_page(since=since, data_type=tables[0] if tables else "", offset=offset, params=params)
            all_changes.extend(page.changes)
            latest_server_time = max(latest_server_time, page.server_time)
            if not page.has_more:
                break
            if page.next_offset <= offset:
                raise Exception(f"服务端返回了无效的 next_offset: {page.next_offset}")
            offset = page.next_offset

        return all_changes, latest_server_time

    def get_changes_page(
        self,
        since: int = 0,
        data_type: str = "",
        limit: int = 0,
        offset: int = 0,
        params: Optional[Dict[str, Any]] = None,
    ) -> SyncChangesPage:
        """获取一页服务端变更；需要手动分页时使用。"""
        req_params = dict(params or {})
        if since > 0:
            req_params["since"] = str(since)
        if data_type:
            req_params["data_type"] = data_type
        if limit > 0:
            req_params["limit"] = str(limit)
        if offset > 0:
            req_params["offset"] = str(offset)

        data = self._request('GET', '/sync/changes', params=req_params)
        changes = []
        for c in data.get('changes', []):
            data_type = c.get('data_type') or c.get('table', '')
            data_key = c.get('data_key') or c.get('record_id') or c.get('id', '')
            action = c.get('action') or c.get('operation', '')
            item_data = c.get('data', {})
            if not isinstance(item_data, dict):
                item_data = {"value": item_data}
            changes.append(SyncChange(
                id=data_key,
                table=data_type,
                record_id=data_key,
                operation=action,
                data=item_data,
                version=c.get('version', 0),
                change_time=c.get('updated_at') or c.get('change_time', 0),
                data_type=data_type,
                data_key=data_key,
                action=action,
                updated_at=c.get('updated_at', 0)
            ))

        return SyncChangesPage(
            changes=changes,
            server_time=data.get('server_time', 0),
            has_more=bool(data.get('has_more', False)),
            next_offset=int(data.get('next_offset', 0) or 0),
            limit=int(data.get('limit', 0) or 0),
            offset=int(data.get('offset', 0) or 0)
        )

    def get_sync_status(self) -> SyncStatus:
        """获取同步状态"""
        data = self._request('GET', '/sync/status')
        return SyncStatus(
            last_sync_time=data.get('last_sync_time', 0),
            pending_changes=data.get('pending_changes', 0),
            table_status=data.get('table_status', {}),
            server_time=data.get('server_time', 0)
        )

    def resolve_conflict(self, conflict_id: str, resolution: ConflictResolution,
                         merged_data: Optional[Dict[str, Any]] = None) -> SyncResult:
        """
        解决数据冲突

        Args:
            conflict_id: push_changes 返回的 conflict_id
            resolution: 解决策略
            merged_data: 当策略为 merge 时，提供合并后的数据

        Returns:
            同步结果
        """
        req_data = {
            "conflict_id": conflict_id,
            "resolution": resolution.value
        }
        if merged_data:
            req_data["merged_data"] = merged_data

        self._request('POST', '/sync/conflict/resolve', req_data)
        return SyncResult(
            record_id="",
            status="resolved",
            conflict_id=conflict_id
        )

    # ==================== 分类数据同步功能 ====================

    def get_configs(self, since: int = 0) -> Tuple[List[ConfigData], int]:
        """获取配置数据"""
        params = {}
        if since > 0:
            params["since"] = str(since)

        data = self._request('GET', '/sync/configs', params=params)
        if isinstance(data.get('configs'), list):
            configs = [ConfigData(
                key=c.get('key', ''),
                value=c.get('value'),
                version=c.get('version', 0),
                updated_at=c.get('updated_at', 0)
            ) for c in data.get('configs', [])]
            return configs, data.get('server_time', 0)

        configs = []
        server_time = 0
        for key, cfg in data.items():
            if key == 'server_time':
                continue
            if isinstance(cfg, dict):
                updated_at = cfg.get('updated_at', 0)
                configs.append(ConfigData(
                    key=key,
                    value=cfg.get('value'),
                    version=cfg.get('version', 0),
                    updated_at=updated_at
                ))
                server_time = max(server_time, updated_at)
            else:
                configs.append(ConfigData(key=key, value=cfg))

        return configs, server_time

    def save_configs(self, configs: List[ConfigData]) -> bool:
        """保存配置数据"""
        for c in configs:
            req_data = {
                "config_key": c.key,
                "value": c.value,
                "version": c.version
            }
            result = self._request('POST', '/sync/configs', req_data)
            status = result.get('status')
            if status and status != 'success':
                if result.get('error'):
                    raise Exception(f"保存配置 {c.key} 失败: {result.get('error')}")
                if result.get('conflict_id'):
                    raise Exception(f"保存配置 {c.key} 冲突: {result.get('conflict_id')}")
                raise Exception(f"保存配置 {c.key} 失败: {status}")
        return True

    @staticmethod
    def _to_unix(value: Any) -> int:
        if isinstance(value, (int, float)):
            return int(value)
        if isinstance(value, str) and value:
            try:
                return int(datetime.fromisoformat(value.replace('Z', '+00:00')).timestamp())
            except ValueError:
                return 0
        return 0

    @staticmethod
    def _rfc3339(ts: int = 0) -> str:
        if ts <= 0:
            ts = int(time.time())
        return datetime.fromtimestamp(ts, timezone.utc).isoformat().replace('+00:00', 'Z')

    @staticmethod
    def _check_sync_results(action: str, data: Any) -> None:
        if isinstance(data, dict) and isinstance(data.get('results'), list):
            results = data.get('results', [])
        elif isinstance(data, dict) and (data.get('status') or data.get('data_key') or data.get('record_id')):
            results = [data]
        else:
            results = []

        if not results:
            raise Exception(f"{action}失败: 服务端没有返回同步结果")
        for result in results:
            status = result.get('status', '')
            if not status or status == 'success':
                continue
            if status == 'conflict':
                conflict_id = result.get('conflict_id', '')
                raise Exception(f"{action}冲突: {conflict_id}" if conflict_id else f"{action}冲突")
            raise Exception(f"{action}失败: {result.get('error') or status}")

    @staticmethod
    def _list_payload(data: Any, key: str) -> Tuple[List[Dict[str, Any]], int]:
        if isinstance(data, list):
            return data, 0
        if isinstance(data, dict):
            items = data.get(key)
            if items is None and key != 'list':
                items = data.get('list')
            return items or [], data.get('server_time', 0)
        return [], 0

    @staticmethod
    def _workflow_config(steps: Any) -> Dict[str, Any]:
        if isinstance(steps, dict):
            return steps
        if isinstance(steps, str) and steps:
            try:
                value = json.loads(steps)
                return value if isinstance(value, dict) else {"steps": value}
            except json.JSONDecodeError:
                return {"steps": steps}
        return {}

    @staticmethod
    def _material_numeric_id(material: MaterialData) -> int:
        if material.material_id:
            return material.material_id
        try:
            value = int(material.id)
            if value > 0:
                return value
        except ValueError:
            pass
        seed = material.id or str(time.time_ns())
        return int(hashlib.sha256(seed.encode('utf-8')).hexdigest()[:15], 16)

    @staticmethod
    def _workflow_to_server(workflow: WorkflowData) -> Dict[str, Any]:
        return {
            "WorkflowID": workflow.id,
            "WorkflowName": workflow.name,
            "Steps": json.dumps(workflow.config or {}, ensure_ascii=False),
            "Status": "active" if workflow.enabled else "disabled",
            "Version": workflow.version,
            "create_time": DataSyncClient._rfc3339(workflow.updated_at)
        }

    @staticmethod
    def _material_to_server(material: MaterialData) -> Dict[str, Any]:
        return {
            "MaterialID": DataSyncClient._material_numeric_id(material),
            "FileName": material.content,
            "FileType": material.type,
            "Caption": material.content,
            "GroupName": material.tags,
            "Status": "未使用",
            "Version": material.version
        }

    @staticmethod
    def _post_to_server(post: PostData) -> Dict[str, Any]:
        group_name = post.group_name or post.group_id or "default"
        post_type = post.post_type or "user_posts"
        post_link = post.post_link or post.id or f"local://{time.time_ns()}"
        return {
            "id": post.id,
            "PostType": post_type,
            "GroupName": group_name,
            "PostLink": post_link,
            "Caption": post.content,
            "Status": post.status or "unused",
            "Version": post.version,
            "collected_at": DataSyncClient._rfc3339(post.updated_at)
        }

    @staticmethod
    def _comment_script_to_server(script: CommentScriptData) -> Dict[str, Any]:
        return {
            "id": script.id,
            "GroupName": script.category,
            "Content": script.content,
            "Status": "active",
            "Version": script.version
        }

    def get_workflows(self, since: int = 0) -> Tuple[List[WorkflowData], int]:
        """获取工作流数据"""
        params = {}
        if since > 0:
            params["since"] = str(since)

        data = self._request('GET', '/sync/workflows', params=params)
        items, server_time = self._list_payload(data, 'workflows')
        workflows = []
        for w in items:
            updated_at = self._to_unix(w.get('updated_at', 0))
            server_time = max(server_time, updated_at)
            workflows.append(WorkflowData(
                id=w.get('WorkflowID') or w.get('workflow_id') or w.get('id', ''),
                name=w.get('WorkflowName') or w.get('workflow_name') or w.get('name', ''),
                config=self._workflow_config(w.get('Steps') or w.get('steps') or w.get('config')),
                enabled=(w.get('Status') or w.get('status') or 'active') != 'disabled',
                version=w.get('Version') or w.get('version', 0),
                updated_at=updated_at
            ))

        return workflows, server_time

    def save_workflows(self, workflows: List[WorkflowData]) -> bool:
        """保存工作流数据"""
        for workflow in workflows:
            result = self._request('POST', '/sync/workflows', {
                "workflow": self._workflow_to_server(workflow),
                "version": workflow.version
            })
            self._check_sync_results(f"保存工作流 {workflow.id}", result)
        return True

    def delete_workflow(self, workflow_id: str) -> bool:
        """删除工作流"""
        result = self._request('DELETE', f'/sync/workflows/{workflow_id}')
        self._check_sync_results(f"删除工作流 {workflow_id}", result)
        return True

    def get_materials(self, since: int = 0) -> Tuple[List[MaterialData], int]:
        """获取素材数据"""
        params = {}
        if since > 0:
            params["since"] = str(since)

        data = self._request('GET', '/sync/materials', params=params)
        items, server_time = self._list_payload(data, 'materials')
        materials = []
        for m in items:
            material_id = m.get('MaterialID') or m.get('material_id') or 0
            updated_at = self._to_unix(m.get('updated_at', 0))
            server_time = max(server_time, updated_at)
            materials.append(MaterialData(
                id=str(material_id or m.get('id', '')),
                material_id=int(material_id or 0),
                type=m.get('FileType') or m.get('file_type') or m.get('type', ''),
                content=m.get('Caption') or m.get('caption') or m.get('FileName') or m.get('file_name') or m.get('content', ''),
                tags=m.get('GroupName') or m.get('group_name') or m.get('tags', ''),
                version=m.get('Version') or m.get('version', 0),
                updated_at=updated_at
            ))

        return materials, server_time

    def save_materials(self, materials: List[MaterialData]) -> bool:
        """保存素材数据"""
        if not materials:
            return True
        result = self._request('POST', '/sync/materials/batch', {
            "materials": [self._material_to_server(m) for m in materials]
        })
        self._check_sync_results("保存素材", result)
        return True

    def get_posts(self, since: int = 0, group_id: str = "") -> Tuple[List[PostData], int]:
        """获取帖子数据"""
        params = {}
        if since > 0:
            params["since"] = str(since)
        if group_id:
            params["group"] = group_id

        data = self._request('GET', '/sync/posts', params=params)
        items, server_time = self._list_payload(data, 'posts')
        posts = []
        for p in items:
            updated_at = self._to_unix(p.get('updated_at', 0))
            server_time = max(server_time, updated_at)
            group_name = p.get('GroupName') or p.get('group_name') or p.get('group_id', '')
            posts.append(PostData(
                id=p.get('id', ''),
                content=p.get('Caption') or p.get('caption') or p.get('content', ''),
                status=p.get('Status') or p.get('status', 'draft'),
                group_id=group_name,
                group_name=group_name,
                post_type=p.get('PostType') or p.get('post_type', ''),
                post_link=p.get('PostLink') or p.get('post_link', ''),
                version=p.get('Version') or p.get('version', 0),
                updated_at=updated_at
            ))

        return posts, server_time

    def save_posts(self, posts: List[PostData]) -> bool:
        """批量保存帖子数据"""
        if not posts:
            return True
        result = self._request('POST', '/sync/posts/batch', {
            "posts": [self._post_to_server(p) for p in posts]
        })
        self._check_sync_results("保存帖子", result)
        return True

    def update_post_status(self, post_id: str, status: str) -> bool:
        """更新帖子状态"""
        req_data = {"status": status}
        self._request('PUT', f'/sync/posts/{post_id}/status', req_data)
        return True

    def get_post_groups(self) -> List[PostGroup]:
        """获取帖子分组"""
        data = self._request('GET', '/sync/posts/groups')
        if isinstance(data, list):
            return [PostGroup(
                id=g.get('group_name', ''),
                name=g.get('group_name', ''),
                count=g.get('total_count', 0)
            ) for g in data]
        return [PostGroup(
            id=g.get('id', ''),
            name=g.get('name', ''),
            count=g.get('count', 0)
        ) for g in data.get('groups', [])]

    def get_comment_scripts(self, since: int = 0, category: str = "") -> Tuple[List[CommentScriptData], int]:
        """获取评论话术"""
        params = {}
        if since > 0:
            params["since"] = str(since)
        if category:
            params["group"] = category

        data = self._request('GET', '/sync/comment-scripts', params=params)
        items, server_time = self._list_payload(data, 'scripts')
        scripts = []
        for s in items:
            updated_at = self._to_unix(s.get('updated_at', 0))
            server_time = max(server_time, updated_at)
            scripts.append(CommentScriptData(
                id=s.get('id', ''),
                content=s.get('Content') or s.get('content', ''),
                category=s.get('GroupName') or s.get('group_name') or s.get('category', ''),
                version=s.get('Version') or s.get('version', 0),
                updated_at=updated_at
            ))

        return scripts, server_time

    def save_comment_scripts(self, scripts: List[CommentScriptData]) -> bool:
        """批量保存评论话术"""
        if not scripts:
            return True
        result = self._request('POST', '/sync/comment-scripts/batch', {
            "scripts": [self._comment_script_to_server(s) for s in scripts]
        })
        self._check_sync_results("保存评论话术", result)
        return True

    # ==================== 便捷方法 ====================

    def sync_table_to_server(self, table_name: str, records: List[Dict[str, Any]], id_field: str = "id") -> List[SyncResult]:
        """
        将本地表数据同步到服务器

        Args:
            table_name: 表名
            records: 记录列表
            id_field: ID字段名

        Returns:
            同步结果列表
        """
        items = []
        for record in records:
            record_id = str(record.get(id_field, ''))
            if not record_id:
                continue
            items.append({
                "record_id": record_id,
                "data": record,
                "version": 0,
                "deleted": False
            })

        if not items:
            return []

        return self.push_record_batch(table_name, items)

    def sync_table_from_server(self, table_name: str, since: int = 0) -> Tuple[List[SyncRecord], List[str], int]:
        """
        从服务器同步表数据到本地

        Args:
            table_name: 表名
            since: 增量同步时间戳

        Returns:
            (需要更新的记录, 需要删除的记录ID, 服务器时间)
        """
        records, server_time = self.pull_table(table_name, since)

        updates = []
        deletes = []

        for r in records:
            if r.is_deleted:
                deletes.append(r.id)
            else:
                updates.append(r)

        return updates, deletes, server_time

    def get_last_sync_time(self, table_name: str) -> int:
        """获取指定表的最后同步时间"""
        return self.last_sync_time.get(table_name, 0)

    def set_last_sync_time(self, table_name: str, t: int):
        """设置指定表的最后同步时间"""
        self.last_sync_time[table_name] = t

    # ==================== 数据备份和同步功能 ====================

    def push_backup(self, data_type: str, data_json: str, device_name: str = "", item_count: int = 0) -> None:
        """
        推送备份数据到服务器

        Args:
            data_type: 数据类型（scripts/danmaku_groups/ai_config/random_word_ai_config）
            data_json: JSON格式的数据
            device_name: 设备名称（可选）
            item_count: 条目数量（可选）
        """
        self._request('POST', '/backup/push', {
            "data_type": data_type,
            "data_json": data_json,
            "device_name": device_name,
            "item_count": item_count
        })

    def pull_backup(self, data_type: str) -> List[BackupData]:
        """
        从服务器拉取指定类型的备份数据

        Args:
            data_type: 数据类型（scripts/danmaku_groups/ai_config/random_word_ai_config）

        Returns:
            备份数据列表（按版本降序排列，第一个为当前版本）
        """
        data = self._request('GET', '/backup/pull', params={"data_type": data_type})
        if isinstance(data, dict):
            data = data.get('data', [])
        return [BackupData(**item) for item in data] if isinstance(data, list) else []

    def pull_all_backups(self) -> Dict[str, List[BackupData]]:
        """
        从服务器拉取所有类型的备份数据

        Returns:
            按数据类型分组的备份数据映射
        """
        data = self._request('GET', '/backup/pull')
        backup_map: Dict[str, List[BackupData]] = {}
        if isinstance(data, dict):
            data = data.get('data', [])
        if not isinstance(data, list):
            return backup_map

        for item in data:
            backup = BackupData(**item)
            backup_map.setdefault(backup.data_type, []).append(backup)

        return backup_map


class AutoSyncManager:
    """自动同步管理器"""

    def __init__(self, sync_client: DataSyncClient, tables: List[str], interval: float = 60.0):
        """
        初始化自动同步管理器

        Args:
            sync_client: DataSyncClient 实例
            tables: 要同步的表列表
            interval: 同步间隔（秒）
        """
        self.sync_client = sync_client
        self.tables = tables
        self.interval = interval
        self._stop_event = threading.Event()
        self._thread: Optional[threading.Thread] = None
        self.last_sync_time: Dict[str, int] = {}

        self.on_pull: Optional[Callable[[str, List[SyncRecord], List[str]], None]] = None
        self.on_conflict: Optional[Callable[[str, SyncResult], None]] = None
        self.on_error: Optional[Callable[[str, Exception], None]] = None

    def set_on_pull(self, callback: Callable[[str, List[SyncRecord], List[str]], None]):
        """设置拉取数据回调"""
        self.on_pull = callback

    def set_on_conflict(self, callback: Callable[[str, SyncResult], None]):
        """设置冲突处理回调"""
        self.on_conflict = callback

    def set_on_error(self, callback: Callable[[str, Exception], None]):
        """设置错误处理回调"""
        self.on_error = callback

    def start(self):
        """启动自动同步"""
        if self._thread and self._thread.is_alive():
            return

        self._stop_event.clear()
        self._thread = threading.Thread(target=self._sync_loop, daemon=True)
        self._thread.start()

    def stop(self):
        """停止自动同步"""
        self._stop_event.set()
        if self._thread:
            self._thread.join(timeout=5)

    def sync_now(self):
        """立即同步"""
        self._sync_all()

    def _sync_loop(self):
        """同步循环"""
        # 立即执行一次同步
        self._sync_all()

        while not self._stop_event.is_set():
            self._stop_event.wait(self.interval)
            if not self._stop_event.is_set():
                self._sync_all()

    def _sync_all(self):
        """同步所有表"""
        for table_name in self.tables:
            try:
                since = self.last_sync_time.get(table_name, 0)
                updates, deletes, server_time = self.sync_client.sync_table_from_server(table_name, since)

                if self.on_pull and (updates or deletes):
                    self.on_pull(table_name, updates, deletes)

                self.last_sync_time[table_name] = server_time
            except Exception as e:
                if self.on_error:
                    self.on_error(table_name, e)

    # ==================== 数据备份和同步功能 ====================

    def push_backup(self, data_type: str, data_json: str, device_name: str = "", item_count: int = 0) -> None:
        """
        推送备份数据到服务器

        Args:
            data_type: 数据类型（scripts/danmaku_groups/ai_config/random_word_ai_config）
            data_json: JSON格式的数据
            device_name: 设备名称（可选）
            item_count: 条目数量（可选）

        Raises:
            Exception: 推送失败时抛出异常
        """
        self.sync_client.push_backup(data_type, data_json, device_name, item_count)

    def pull_backup(self, data_type: str) -> List[BackupData]:
        """
        从服务器拉取指定类型的备份数据

        Args:
            data_type: 数据类型（scripts/danmaku_groups/ai_config/random_word_ai_config）

        Returns:
            备份数据列表（按版本降序排列，第一个为当前版本）

        Raises:
            Exception: 拉取失败时抛出异常
        """
        return self.sync_client.pull_backup(data_type)

    def pull_all_backups(self) -> Dict[str, List[BackupData]]:
        """
        从服务器拉取所有类型的备份数据

        Returns:
            按数据类型分组的备份数据映射

        Raises:
            Exception: 拉取失败时抛出异常
        """
        return self.sync_client.pull_all_backups()
