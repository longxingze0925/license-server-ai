import argparse
import json
import os
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[1]
PYTHON_SDK = REPO_ROOT / "sdk" / "python"
sys.path.insert(0, str(PYTHON_SDK))

from data_sync import DataSyncClient  # noqa: E402
from license_client import LicenseClient  # noqa: E402


def parse_response_body(raw):
    if not raw:
        return {}
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        return {"raw": raw}


def api_request(base_url, method, path, body=None, token=None):
    data = None if body is None else json.dumps(body).encode("utf-8")
    request = urllib.request.Request(base_url.rstrip("/") + "/api" + path, data=data, method=method)
    if body is not None:
        request.add_header("Content-Type", "application/json")
    if token:
        request.add_header("Authorization", "Bearer " + token)

    try:
        with urllib.request.urlopen(request, timeout=20) as response:
            raw = response.read().decode("utf-8", errors="replace")
            return response.status, parse_response_body(raw)
    except urllib.error.HTTPError as exc:
        raw = exc.read().decode("utf-8", errors="replace")
        return exc.code, parse_response_body(raw)
    except (urllib.error.URLError, ConnectionResetError, TimeoutError) as exc:
        return 0, {"message": str(exc), "transport_error": exc.__class__.__name__}


def require_ok(label, status, payload):
    if status != 200 or payload.get("code") != 0:
        raise RuntimeError(f"{label} failed: status={status}, payload={payload}")
    return payload.get("data") or {}


def assert_denied(label, status, payload):
    if status == 200 and payload.get("code") == 0:
        raise RuntimeError(f"{label} unexpectedly succeeded: {payload}")


def create_test_context(args, stamp):
    status, payload = api_request(
        args.base_url,
        "POST",
        "/auth/login",
        {"email": args.admin_email, "password": args.admin_password},
    )
    admin = require_ok("admin login", status, payload)
    token = admin["token"]

    status, payload = api_request(
        args.base_url,
        "POST",
        "/admin/apps",
        {
            "name": "codex-sync-backup-smoke-" + stamp,
            "max_devices_default": 2,
            "features": ["sync", "backup"],
        },
        token,
    )
    app = require_ok("create app", status, payload)

    email = f"codex-sync-backup-smoke-{stamp}@example.test"
    password = args.client_password
    status, payload = api_request(
        args.base_url,
        "POST",
        "/admin/customers",
        {"email": email, "password": password, "name": "Codex Sync Backup Smoke"},
        token,
    )
    customer = require_ok("create customer", status, payload)

    status, payload = api_request(
        args.base_url,
        "POST",
        "/admin/subscriptions",
        {
            "customer_id": customer["id"],
            "app_id": app["id"],
            "plan_type": "pro",
            "max_devices": 2,
            "days": 7,
            "features": ["sync", "backup"],
        },
        token,
    )
    require_ok("create subscription", status, payload)

    return {
        "admin_token": token,
        "app_id": app["id"],
        "app_key": app["app_key"],
        "customer_id": customer["id"],
        "email": email,
        "password": password,
    }


def cleanup(base_url, token, app_id, customer_id):
    if token and customer_id:
        api_request(base_url, "DELETE", "/admin/customers/" + customer_id, token=token)
    if token and app_id:
        api_request(base_url, "DELETE", "/admin/apps/" + app_id, token=token)


def run_python_sdk(args, context):
    with tempfile.TemporaryDirectory(prefix="license-sdk-smoke-") as cache_dir:
        client = LicenseClient(args.base_url, context["app_key"], skip_verify=True, cache_dir=cache_dir)
        login_result = client.login(context["email"], context["password"])
        if not login_result.get("access_token") and not getattr(client, "_license_info", {}).get("access_token"):
            raise RuntimeError("client login did not return access token")

        status, payload = api_request(
            args.base_url,
            "POST",
            "/client/sync/table",
            {
                "app_key": context["app_key"],
                "machine_id": client.machine_id,
                "table": "legacy_probe",
                "record_id": "old-1",
                "data": {"old": True},
            },
        )
        assert_denied("legacy sync without bearer", status, payload)

        backup_status, backup_payload = api_request(
            args.base_url,
            "POST",
            "/client/backup/push",
            {
                "app_key": context["app_key"],
                "machine_id": client.machine_id,
                "data_type": "scripts",
                "data_json": "[]",
            },
        )
        assert_denied("legacy backup without bearer", backup_status, backup_payload)

        sync = DataSyncClient(client)
        record = sync.push_record("codex_table", "record-1", {"name": "bearer", "value": 42})
        records, server_time = sync.pull_table("codex_table")
        if not any(item.id == "record-1" and item.data.get("value") == 42 for item in records):
            raise RuntimeError("bearer sync pull did not return pushed record")

        sync.push_backup(
            "scripts",
            json.dumps([{"id": "script-1", "text": "hello"}], ensure_ascii=False),
            "Codex Smoke Device",
            1,
        )
        backups = sync.pull_backup("scripts")
        if not backups or backups[0].data_type != "scripts":
            raise RuntimeError("bearer backup pull failed")

        return {
            "legacy_sync_status": status,
            "legacy_backup_status": backup_status,
            "bearer_sync_push": record.status,
            "bearer_sync_pull_count": len(records),
            "bearer_sync_server_time": server_time,
            "bearer_backup_count": len(backups),
        }


def run_go_sdk(args, context):
    env = os.environ.copy()
    env["LS_INTEGRATION_SERVER_URL"] = args.base_url
    env["LS_INTEGRATION_APP_KEY"] = context["app_key"]
    env["LS_TEST_EMAIL"] = context["email"]
    env["LS_TEST_PASSWORD"] = context["password"]

    command = [
        "go",
        "test",
        "-tags",
        "integration",
        "./sdk/go",
        "-run",
        "TestIntegration_DataSync_BackupPushPull",
        "-count=1",
        "-v",
    ]
    result = subprocess.run(
        command,
        cwd=REPO_ROOT,
        env=env,
        text=True,
        encoding="utf-8",
        errors="replace",
        capture_output=True,
    )
    if result.returncode != 0:
        raise RuntimeError("go sdk smoke failed:\n" + result.stdout + result.stderr)
    return {"go_sdk": "ok", "output": result.stdout}


def parse_args():
    parser = argparse.ArgumentParser(description="Smoke test client sync/backup Bearer-token flow.")
    parser.add_argument("--base-url", default="http://127.0.0.1:8081")
    parser.add_argument("--admin-email", default="admin@example.com")
    parser.add_argument("--admin-password", default="admin123")
    parser.add_argument("--client-password", default="SmokePass1!")
    parser.add_argument("--run-go-sdk", action="store_true", help="Also run the Go SDK backup integration smoke.")
    parser.add_argument("--keep", action="store_true", help="Keep the temporary app/customer for debugging.")
    return parser.parse_args()


def main():
    args = parse_args()
    stamp = time.strftime("%Y%m%d%H%M%S")
    context = {}
    try:
        context = create_test_context(args, stamp)
        result = {
            "python_sdk": run_python_sdk(args, context),
        }
        if args.run_go_sdk:
            result["go_sdk"] = run_go_sdk(args, context)
        print(json.dumps(result, ensure_ascii=False, indent=2))
    finally:
        if context and not args.keep:
            cleanup(args.base_url, context.get("admin_token"), context.get("app_id"), context.get("customer_id"))


if __name__ == "__main__":
    main()
