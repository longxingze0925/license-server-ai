import asyncio
import json
import urllib.error
import urllib.request
from datetime import datetime

try:
    import websockets
except ModuleNotFoundError as exc:
    raise SystemExit(
        "Missing Python package 'websockets'. "
        "Run `go run ./tools/ws_smoke` for the dependency-free Go smoke test, "
        "or install it with `python -m pip install websockets`."
    ) from exc


BASE = "http://127.0.0.1:8081/api"
WS_URL = "ws://127.0.0.1:8081/api/client/ws"


def request(method, path, body=None, token=None):
    data = None if body is None else json.dumps(body).encode("utf-8")
    req = urllib.request.Request(BASE + path, data=data, method=method)
    if body is not None:
        req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(req, timeout=20) as resp:
            raw = resp.read().decode("utf-8")
            return resp.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as exc:
        raw = exc.read().decode("utf-8")
        return exc.code, json.loads(raw) if raw else {}


async def main():
    stamp = datetime.now().strftime("%Y%m%d%H%M%S")
    status, login = request("POST", "/auth/login", {"email": "admin@example.com", "password": "admin123"})
    assert status == 200 and login["code"] == 0, login
    token = login["data"]["token"]

    app_id = None
    customer_id = None
    try:
        status, app = request(
            "POST",
            "/admin/apps",
            {"name": f"codex-ws-realtest-{stamp}", "max_devices_default": 2, "features": ["ws"]},
            token,
        )
        assert status == 200 and app["code"] == 0, app
        app_id = app["data"]["id"]
        app_key = app["data"]["app_key"]

        email = f"codex-ws-realtest-{stamp}@example.test"
        password = "SmokePass1!"
        status, customer = request(
            "POST",
            "/admin/customers",
            {"email": email, "password": password, "name": "Codex WS Test"},
            token,
        )
        assert status == 200 and customer["code"] == 0, customer
        customer_id = customer["data"]["id"]

        status, sub = request(
            "POST",
            "/admin/subscriptions",
            {
                "customer_id": customer_id,
                "app_id": app_id,
                "plan_type": "pro",
                "max_devices": 2,
                "days": 7,
                "features": ["ws"],
            },
            token,
        )
        assert status == 200 and sub["code"] == 0, sub

        machine_id = f"codex-ws-machine-{stamp}"
        status, client_login = request(
            "POST",
            "/client/auth/login",
            {
                "app_key": app_key,
                "email": email,
                "password": password,
                "password_hashed": False,
                "machine_id": machine_id,
                "device_info": {
                    "name": "Codex WS Device",
                    "hostname": "codex-ws-host",
                    "os": "Windows",
                    "os_version": "test",
                    "app_version": "1.0.0",
                },
            },
        )
        assert status == 200 and client_login["code"] == 0, client_login
        access_token = client_login["data"].get("access_token")
        token_type = client_login["data"].get("token_type") or "Bearer"
        assert access_token, client_login

        async with websockets.connect(
            WS_URL,
            origin="http://127.0.0.1:3000",
            additional_headers={"Authorization": f"{token_type} {access_token}"},
        ) as ws:
            await ws.send(
                json.dumps(
                    {
                        "type": "auth",
                        "payload": {
                            "app_key": app_key,
                            "machine_id": machine_id,
                        },
                    }
                )
            )
            auth_msg = json.loads(await asyncio.wait_for(ws.recv(), timeout=10))
            assert auth_msg["type"] == "auth_ok", auth_msg

            status, online = request("GET", f"/admin/apps/{app_id}/online-devices", token=token)
            assert status == 200 and online["code"] == 0, online
            assert online["data"]["online_count"] >= 1, online

            status, sent = request(
                "POST",
                "/admin/instructions/send",
                {
                    "app_id": app_id,
                    "machine_id": machine_id,
                    "type": "get_status",
                    "payload": '{"probe":"codex"}',
                },
                token,
            )
            assert status == 200 and sent["code"] == 0 and sent["data"]["sent"], sent
            instruction_id = sent["data"]["instruction_id"]

            instruction = json.loads(await asyncio.wait_for(ws.recv(), timeout=10))
            assert instruction["type"] == "instruction", instruction
            assert instruction["id"] == instruction_id, instruction

            await ws.send(
                json.dumps(
                    {
                        "type": "instruction_result",
                        "payload": {
                            "instruction_id": instruction_id,
                            "status": "executed",
                            "result": {"ok": True, "source": "ws_smoke"},
                        },
                    }
                )
            )
            await asyncio.sleep(1)

        status, detail = request("GET", f"/admin/instructions/{instruction_id}", token=token)
        assert status == 200 and detail["code"] == 0, detail
        print(
            json.dumps(
                {
                    "auth": auth_msg,
                    "online_count": online["data"]["online_count"],
                    "instruction_id": instruction_id,
                    "sent": sent["data"],
                    "status": detail["data"].get("status"),
                    "results": detail["data"].get("results"),
                },
                ensure_ascii=False,
                indent=2,
            )
        )
    finally:
        if customer_id:
            request("DELETE", f"/admin/customers/{customer_id}", token=token)
        if app_id:
            request("DELETE", f"/admin/apps/{app_id}", token=token)


asyncio.run(main())
