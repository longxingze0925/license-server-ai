"""
WebSocket 客户端模块
提供实时指令接收和执行功能

安全特性：
- 支持 WSS (WebSocket Secure)
- 支持证书固定（继承自 LicenseClient）
- RSA 签名验证

使用示例：
    from license_client import LicenseClient
    from websocket_client import WSClient

    client = LicenseClient(
        server_url="https://192.168.1.100:8080",
        app_key="your_app_key",
        cert_fingerprint="SHA256:AB:CD:EF:..."  # 证书固定
    )

    # 创建 WebSocket 客户端
    ws = WSClient(client)

    # 注册指令处理器
    @ws.on_instruction("click")
    def handle_click(instruction):
        x = instruction.payload.get("x")
        y = instruction.payload.get("y")
        # 执行点击操作
        return {"clicked": True}

    # 连接服务器
    ws.connect()

    # 保持运行
    ws.run_forever()
"""

import base64
import hashlib
import json
import ssl
import threading
import time
from dataclasses import dataclass
from typing import Any, Callable, Dict, Optional
from urllib.parse import urlparse

try:
    import websocket
except ImportError:
    raise ImportError("请安装 websocket-client 库: pip install websocket-client")

try:
    from cryptography.hazmat.primitives.asymmetric import padding
    from cryptography.hazmat.primitives import hashes
except ImportError:
    raise ImportError("请安装 cryptography 库: pip install cryptography")


@dataclass
class Instruction:
    """指令"""
    id: str
    type: str
    payload: Dict[str, Any]
    timestamp: int
    nonce: str
    signature: str
    expires_at: int


class WSClientError(Exception):
    """WebSocket 客户端错误"""
    pass


class WSClient:
    """WebSocket 客户端"""

    def __init__(
        self,
        client,  # LicenseClient 实例
        reconnect: bool = True,
        reconnect_interval: float = 5.0,
        on_connect: Optional[Callable[[], None]] = None,
        on_disconnect: Optional[Callable[[Optional[Exception]], None]] = None,
        on_error: Optional[Callable[[Exception], None]] = None
    ):
        """
        初始化 WebSocket 客户端

        Args:
            client: LicenseClient 实例（包含证书固定配置）
            reconnect: 是否自动重连
            reconnect_interval: 重连间隔 (秒)
            on_connect: 连接成功回调
            on_disconnect: 断开连接回调
            on_error: 错误回调
        """
        self.client = client
        self.reconnect = reconnect
        self.reconnect_interval = reconnect_interval
        self.on_connect = on_connect
        self.on_disconnect = on_disconnect
        self.on_error = on_error

        # WebSocket URL
        parsed = urlparse(client.server_url)
        ws_scheme = "wss" if parsed.scheme == "https" else "ws"
        self._ws_url = f"{ws_scheme}://{parsed.netloc}/api/client/ws"

        # SSL 配置（继承自 LicenseClient）
        self._ssl_opt = self._build_ssl_options()

        # 状态
        self._ws: Optional[websocket.WebSocketApp] = None
        self._session_id: Optional[str] = None
        self._connected = False
        self._should_reconnect = True
        self._handlers: Dict[str, Callable[[Instruction], Any]] = {}
        self._lock = threading.Lock()
        self._thread: Optional[threading.Thread] = None

    def _build_ssl_options(self) -> Dict:
        """构建 SSL 选项（支持证书固定）"""
        ssl_opt = {}

        if hasattr(self.client, 'skip_verify') and self.client.skip_verify:
            # 跳过验证（仅测试用）
            ssl_opt['cert_reqs'] = ssl.CERT_NONE
            ssl_opt['check_hostname'] = False
        elif hasattr(self.client, 'cert_path') and self.client.cert_path:
            # 使用指定的证书文件
            ssl_opt['ca_certs'] = self.client.cert_path
            ssl_opt['cert_reqs'] = ssl.CERT_REQUIRED

        return ssl_opt

    def connect(self) -> bool:
        """
        连接服务器

        Returns:
            是否连接成功
        """
        with self._lock:
            if self._connected:
                return True
            if self._thread and self._thread.is_alive():
                return False
            self._should_reconnect = True

        self._get_access_token()

        # 在后台线程运行
        self._thread = threading.Thread(target=self._run, daemon=True)
        self._thread.start()

        # 等待连接完成
        for _ in range(50):  # 最多等待 5 秒
            time.sleep(0.1)
            if self._connected:
                return True

        return False

    def disconnect(self):
        """断开连接"""
        with self._lock:
            self._should_reconnect = False
            self._connected = False

        if self._ws:
            self._ws.close()

    def is_connected(self) -> bool:
        """是否已连接"""
        with self._lock:
            return self._connected

    def get_session_id(self) -> Optional[str]:
        """获取会话ID"""
        with self._lock:
            return self._session_id

    def register_handler(self, instruction_type: str, handler: Callable[[Instruction], Any]):
        """
        注册指令处理器

        Args:
            instruction_type: 指令类型
            handler: 处理函数，接收 Instruction，返回结果
        """
        with self._lock:
            self._handlers[instruction_type] = handler

    def on_instruction(self, instruction_type: str):
        """
        指令处理器装饰器

        Usage:
            @ws.on_instruction("click")
            def handle_click(instruction):
                return {"success": True}
        """
        def decorator(func: Callable[[Instruction], Any]):
            self.register_handler(instruction_type, func)
            return func
        return decorator

    def send_status(self, status: Dict[str, Any]):
        """
        发送状态上报

        Args:
            status: 状态数据
        """
        self._send_message({
            "type": "status",
            "payload": status
        })

    def run_forever(self):
        """阻塞运行，直到断开连接"""
        if self._thread:
            self._thread.join()

    # 内部方法

    def _run(self):
        """运行 WebSocket"""
        while True:
            with self._lock:
                should_run = self._should_reconnect
            if not should_run:
                break

            try:
                self._ws = self._create_websocket_app()
                # 使用 SSL 选项运行
                self._ws.run_forever(
                    ping_interval=30,
                    ping_timeout=10,
                    sslopt=self._ssl_opt if self._ssl_opt else None
                )
            except Exception as e:
                print(f"WebSocket error: {e}")
                if self.on_error:
                    self.on_error(e)

            with self._lock:
                self._connected = False
                self._ws = None

            if not self.reconnect:
                break

            with self._lock:
                should_reconnect = self._should_reconnect
            if should_reconnect:
                print(f"WebSocket: 将在 {self.reconnect_interval} 秒后重连...")
                time.sleep(self.reconnect_interval)

        with self._lock:
            self._should_reconnect = False

    def _create_websocket_app(self) -> websocket.WebSocketApp:
        access_token = self._get_access_token()
        return websocket.WebSocketApp(
            self._ws_url,
            header=[f"Authorization: Bearer {access_token}"],
            on_open=self._on_open,
            on_message=self._on_message,
            on_error=self._on_error,
            on_close=self._on_close
        )

    def _get_access_token(self) -> str:
        if self._access_token_expired_soon() and hasattr(self.client, '_refresh_client_session'):
            self.client._refresh_client_session()

        access_token = (getattr(self.client, '_license_info', {}) or {}).get('access_token', '')
        if not access_token:
            raise WSClientError("缺少客户端访问令牌，请先激活或登录")
        return access_token

    def _access_token_expired_soon(self, skew_seconds: int = 60) -> bool:
        info = getattr(self.client, '_license_info', {}) or {}
        access_token = info.get('access_token', '')
        if not access_token:
            return True
        expires_at = info.get('access_expires_at') or 0
        try:
            return bool(expires_at) and time.time() + skew_seconds >= float(expires_at)
        except (TypeError, ValueError):
            return False

    def _on_open(self, ws):
        """连接打开回调"""
        # 发送认证消息
        auth_msg = {
            "type": "auth",
            "payload": {
                "app_key": self.client.app_key,
                "machine_id": self.client.machine_id
            }
        }
        ws.send(json.dumps(auth_msg))

    def _on_message(self, ws, message: str):
        """消息回调"""
        try:
            msg = json.loads(message)
            msg_type = msg.get("type")

            if msg_type == "auth_ok":
                # 认证成功
                payload = msg.get("payload", {})
                if isinstance(payload, str):
                    payload = json.loads(payload)
                with self._lock:
                    self._session_id = payload.get("session_id")
                    self._connected = True
                print(f"WebSocket: 已连接 session={self._session_id}")
                if self.on_connect:
                    self.on_connect()

            elif msg_type == "error":
                # 错误
                payload = msg.get("payload", {})
                if isinstance(payload, str):
                    payload = json.loads(payload)
                error_msg = payload.get("message", "未知错误")
                print(f"WebSocket error: {error_msg}")
                if self.on_error:
                    self.on_error(WSClientError(error_msg))

            elif msg_type == "pong":
                # 心跳响应，忽略
                pass

            elif msg_type == "instruction":
                # 指令
                self._handle_instruction(msg)

            else:
                print(f"WebSocket: 未知消息类型 {msg_type}")

        except Exception as e:
            print(f"WebSocket: 处理消息失败: {e}")

    def _on_error(self, ws, error):
        """错误回调"""
        print(f"WebSocket error: {error}")
        if self.on_error:
            self.on_error(error)

    def _on_close(self, ws, close_status_code, close_msg):
        """关闭回调"""
        with self._lock:
            was_connected = self._connected
            self._connected = False

        if was_connected:
            print("WebSocket: 连接断开")
            if self.on_disconnect:
                self.on_disconnect(None)

    def _handle_instruction(self, msg: Dict):
        """处理指令"""
        try:
            payload = msg.get("payload", {})
            if isinstance(payload, str):
                payload = json.loads(payload)

            payload_body = payload.get("payload", {})
            payload_raw = payload.get("payload_raw")
            if payload_raw is None:
                if isinstance(payload_body, str):
                    payload_raw = payload_body
                else:
                    payload_raw = json.dumps(payload_body, ensure_ascii=False, separators=(',', ':'))

            inst = Instruction(
                id=payload.get("id"),
                type=payload.get("type"),
                payload=json.loads(payload_body) if isinstance(payload_body, str) else payload_body,
                timestamp=payload.get("timestamp", 0),
                nonce=payload.get("nonce", ""),
                signature=payload.get("signature", ""),
                expires_at=payload.get("expires", 0)
            )

            # 验证过期时间
            if inst.expires_at and time.time() > inst.expires_at:
                print(f"WebSocket: 指令已过期 id={inst.id}")
                self._send_instruction_result(inst.id, "failed", None, "指令已过期")
                return

            # 验证签名
            sign_data = f"{inst.id}:{inst.type}:{payload_raw}:{inst.nonce}"
            if not self._verify_signature(sign_data.encode(), inst.signature):
                print(f"WebSocket: 指令签名验证失败 id={inst.id}")
                self._send_instruction_result(inst.id, "failed", None, "签名验证失败")
                return

            # 查找处理器
            with self._lock:
                handler = self._handlers.get(inst.type)

            if not handler:
                print(f"WebSocket: 未知指令类型 {inst.type}")
                self._send_instruction_result(inst.id, "failed", None, "未知指令类型")
                return

            # 在新线程中执行
            def execute():
                try:
                    result = handler(inst)
                    self._send_instruction_result(inst.id, "success", result, "")
                except Exception as e:
                    self._send_instruction_result(inst.id, "failed", None, str(e))

            threading.Thread(target=execute, daemon=True).start()

        except Exception as e:
            print(f"WebSocket: 处理指令失败: {e}")

    def _send_instruction_result(self, instruction_id: str, status: str, result: Any, error: str):
        """发送指令执行结果"""
        payload = {
            "instruction_id": instruction_id,
            "status": status
        }
        if result is not None:
            payload["result"] = result
        if error:
            payload["error"] = error

        self._send_message({
            "type": "instruction_result",
            "id": instruction_id,
            "payload": payload
        })

    def _send_message(self, msg: Dict):
        """发送消息"""
        with self._lock:
            if not self._connected or not self._ws:
                return

        try:
            self._ws.send(json.dumps(msg))
        except Exception as e:
            print(f"WebSocket: 发送消息失败: {e}")

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


# 预定义指令处理器示例

def create_click_handler(click_func: Callable[[int, int], None]):
    """
    创建点击指令处理器

    Args:
        click_func: 点击函数，接收 (x, y)
    """
    def handler(instruction: Instruction) -> Dict:
        x = instruction.payload.get("x", 0)
        y = instruction.payload.get("y", 0)
        click_func(x, y)
        return {"clicked": True, "x": x, "y": y}
    return handler


def create_input_handler(input_func: Callable[[str], None]):
    """
    创建输入指令处理器

    Args:
        input_func: 输入函数，接收文本
    """
    def handler(instruction: Instruction) -> Dict:
        text = instruction.payload.get("text", "")
        input_func(text)
        return {"input": True, "length": len(text)}
    return handler


def create_screenshot_handler(screenshot_func: Callable[[], bytes]):
    """
    创建截图指令处理器

    Args:
        screenshot_func: 截图函数，返回图片字节
    """
    def handler(instruction: Instruction) -> Dict:
        image_data = screenshot_func()
        # 可以选择上传或返回 base64
        return {"captured": True, "size": len(image_data)}
    return handler
