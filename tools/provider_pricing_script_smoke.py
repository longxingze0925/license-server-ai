import argparse
import json
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[1]
PYTHON_SDK = REPO_ROOT / "sdk" / "python"
sys.path.insert(0, str(PYTHON_SDK))

from license_client import LicenseClient  # noqa: E402
from scripts import ReleaseManager, ScriptManager  # noqa: E402


def parse_response_body(raw):
    if not raw:
        return {}
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        return {"raw": raw}


def api_request(base_url, method, path, body=None, token=None, query=None):
    url = base_url.rstrip("/") + "/api" + path
    if query:
        url += "?" + urllib.parse.urlencode(query)

    data = None if body is None else json.dumps(body).encode("utf-8")
    request = urllib.request.Request(url, data=data, method=method)
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


def api_multipart_request(base_url, path, fields, files, token=None):
    boundary = "----codex-smoke-" + str(int(time.time() * 1000))
    chunks = []
    for name, value in fields.items():
        chunks.append(f"--{boundary}\r\n".encode("utf-8"))
        chunks.append(f'Content-Disposition: form-data; name="{name}"\r\n\r\n'.encode("utf-8"))
        chunks.append(str(value).encode("utf-8"))
        chunks.append(b"\r\n")
    for name, file_info in files.items():
        filename, content, content_type = file_info
        chunks.append(f"--{boundary}\r\n".encode("utf-8"))
        chunks.append(
            f'Content-Disposition: form-data; name="{name}"; filename="{filename}"\r\n'
            f"Content-Type: {content_type}\r\n\r\n".encode("utf-8")
        )
        chunks.append(content)
        chunks.append(b"\r\n")
    chunks.append(f"--{boundary}--\r\n".encode("utf-8"))

    request = urllib.request.Request(base_url.rstrip("/") + "/api" + path, data=b"".join(chunks), method="POST")
    request.add_header("Content-Type", "multipart/form-data; boundary=" + boundary)
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
            "name": "codex-provider-pricing-smoke-" + stamp,
            "max_devices_default": 2,
            "features": ["scripts", "secure-scripts", "proxy"],
        },
        token,
    )
    app = require_ok("create app", status, payload)

    email = f"codex-provider-pricing-smoke-{stamp}@example.test"
    status, payload = api_request(
        args.base_url,
        "POST",
        "/admin/customers",
        {"email": email, "password": args.client_password, "name": "Codex Provider Pricing Smoke"},
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
            "features": ["scripts", "secure-scripts", "proxy"],
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
        "password": args.client_password,
    }


def cleanup(base_url, token, context):
    pricing_rule_id = context.get("pricing_rule_id")
    if token and pricing_rule_id:
        api_request(base_url, "DELETE", "/admin/pricing/rules/" + str(pricing_rule_id), token=token)
    if token and context.get("customer_id"):
        api_request(base_url, "DELETE", "/admin/customers/" + context["customer_id"], token=token)
    if token and context.get("app_id"):
        api_request(base_url, "DELETE", "/admin/apps/" + context["app_id"], token=token)


def check_proxy_and_pricing(args, context, stamp):
    token = context["admin_token"]

    status, payload = api_request(args.base_url, "GET", "/admin/proxy/credentials", token=token)
    credentials = require_ok("list provider credentials", status, payload)
    for item in credentials.get("list", []):
        if "api_key" in item or "api_key_enc" in item:
            raise RuntimeError("provider credential list leaked API key fields")

    status, payload = api_request(args.base_url, "GET", "/admin/pricing/rules", token=token)
    pricing_rules = require_ok("list pricing rules", status, payload)

    probe_model = "codex-smoke-grok-video-" + stamp
    status, payload = api_request(
        args.base_url,
        "POST",
        "/admin/pricing/rules",
        {
            "provider": "grok",
            "scope": "video",
            "match_json": json.dumps({"model": probe_model}, separators=(",", ":")),
            "credits": args.probe_credits,
            "priority": 99999,
            "enabled": True,
            "note": "codex smoke temporary pricing rule",
        },
        token,
    )
    rule = require_ok("create pricing rule", status, payload)
    context["pricing_rule_id"] = rule["id"]

    status, payload = api_request(
        args.base_url,
        "POST",
        "/admin/pricing/preview",
        {
            "provider": "grok",
            "scope": "video",
            "params": {"model": probe_model, "duration_seconds": 5, "mode": "official"},
        },
        token,
    )
    preview = require_ok("pricing preview", status, payload)
    if not preview.get("matched") or preview.get("cost") != args.probe_credits or preview.get("rule_id") != rule["id"]:
        raise RuntimeError(f"pricing preview did not hit temporary rule: {preview}")

    return {
        "credentials_total": credentials.get("total", 0),
        "pricing_total_before": pricing_rules.get("total", 0),
        "pricing_rule_id": rule["id"],
        "pricing_preview_cost": preview.get("cost"),
    }


def check_client_script_apis(args, context):
	script_filename = "codex_smoke_script.py"
	script_content = b"print('codex smoke script')\n"
	status, payload = api_multipart_request(
		args.base_url,
		"/admin/apps/" + context["app_id"] + "/scripts",
		fields={"version": "1.2.3"},
		files={"file": (script_filename, script_content, "text/x-python")},
		token=context["admin_token"],
	)
	uploaded_script = require_ok("upload plain script", status, payload)

	release_content = b"codex smoke release\n"
	status, payload = api_multipart_request(
		args.base_url,
		"/admin/apps/" + context["app_id"] + "/releases/upload",
		fields={
			"version": "9.9.1",
			"version_code": "9009001",
			"changelog": "codex smoke release",
			"force_update": "false",
		},
		files={"file": ("codex_smoke_release.zip", release_content, "application/zip")},
		token=context["admin_token"],
	)
	uploaded_release = require_ok("upload release", status, payload)
	status, payload = api_request(
		args.base_url,
		"POST",
		"/admin/releases/" + uploaded_release["id"] + "/publish",
		token=context["admin_token"],
	)
	require_ok("publish release", status, payload)

	status, payload = api_request(
		args.base_url,
		"GET",
		"/client/scripts/version",
		query={"app_key": context["app_key"], "machine_id": "legacy-probe"},
	)
	assert_denied("legacy script version without bearer", status, payload)
	legacy_script_status = status

	status, payload = api_request(
		args.base_url,
		"GET",
		"/client/secure-scripts/versions",
		query={"app_key": context["app_key"], "machine_id": "legacy-probe"},
	)
	assert_denied("legacy secure script versions without bearer", status, payload)
	legacy_secure_status = status

	status, payload = api_request(
		args.base_url,
		"GET",
		"/client/releases/latest",
		query={"app_key": context["app_key"], "machine_id": "legacy-probe"},
	)
	assert_denied("legacy release latest without bearer", status, payload)
	legacy_release_status = status

	with tempfile.TemporaryDirectory(prefix="license-sdk-smoke-") as cache_dir:
		client = LicenseClient(args.base_url, context["app_key"], skip_verify=True, cache_dir=cache_dir)
		login_result = client.login(context["email"], context["password"])
		if not login_result.get("access_token") and not getattr(client, "_license_info", {}).get("access_token"):
			raise RuntimeError("client login did not return access token")
		access_token = login_result.get("access_token") or getattr(client, "_license_info", {}).get("access_token")

		status, payload = api_request(
			args.base_url,
			"GET",
			"/client/scripts/version",
			token=access_token,
		)
		scripts = require_ok("script version with bearer", status, payload)
		if not isinstance(scripts, dict) or not isinstance(scripts.get("scripts"), list):
			raise RuntimeError(f"script version response has unexpected shape: {scripts}")
		matching_scripts = [s for s in scripts["scripts"] if s.get("filename") == script_filename]
		if not matching_scripts:
			raise RuntimeError(f"uploaded script missing from version response: {scripts}")
		if matching_scripts[0].get("version") != "1.2.3" or not matching_scripts[0].get("version_code"):
			raise RuntimeError(f"uploaded script version metadata is wrong: {matching_scripts[0]}")
		if matching_scripts[0].get("file_hash") != uploaded_script.get("hash"):
			raise RuntimeError(f"uploaded script hash mismatch: {matching_scripts[0]}")

		script_manager = ScriptManager(client)
		content = script_manager.download_script(script_filename)
		if content != script_content:
			raise RuntimeError("downloaded plain script content mismatch")

		release_manager = ReleaseManager(client)
		release_path = Path(cache_dir) / "codex_smoke_release.zip"
		update_info = release_manager.get_latest_release_and_download(str(release_path))
		if update_info.version_code != 9009001 or update_info.version != "9.9.1":
			raise RuntimeError(f"release update metadata is wrong: {update_info}")
		if release_path.read_bytes() != release_content:
			raise RuntimeError("downloaded release content mismatch")

		status, payload = api_request(
			args.base_url,
			"GET",
			"/client/secure-scripts/versions",
			token=access_token,
		)
		secure_scripts = require_ok("secure script versions with bearer", status, payload)

		client.close()
		return {
			"machine_id": client.machine_id,
			"client_auth_mode": login_result.get("auth_mode"),
			"legacy_script_status": legacy_script_status,
			"legacy_secure_script_status": legacy_secure_status,
			"legacy_release_status": legacy_release_status,
			"script_version_count": len(scripts.get("scripts", [])) if isinstance(scripts, dict) else 0,
			"script_download_bytes": len(content),
			"release_version_code": update_info.version_code,
			"release_download_bytes": release_path.stat().st_size,
			"secure_script_version_count": len(secure_scripts) if isinstance(secure_scripts, list) else 0,
		}


def parse_args():
    parser = argparse.ArgumentParser(description="Smoke test provider credentials, pricing preview, and client script APIs.")
    parser.add_argument("--base-url", default="http://127.0.0.1:8081")
    parser.add_argument("--admin-email", default="admin@example.com")
    parser.add_argument("--admin-password", default="admin123")
    parser.add_argument("--client-password", default="SmokePass1!")
    parser.add_argument("--probe-credits", type=int, default=37)
    parser.add_argument("--keep", action="store_true", help="Keep the temporary app/customer/pricing rule for debugging.")
    return parser.parse_args()


def main():
    args = parse_args()
    stamp = time.strftime("%Y%m%d%H%M%S")
    context = {}
    try:
        context = create_test_context(args, stamp)
        result = {
            "provider_pricing": check_proxy_and_pricing(args, context, stamp),
            "client_scripts": check_client_script_apis(args, context),
        }
        print(json.dumps(result, ensure_ascii=False, indent=2))
    finally:
        if context and not args.keep:
            cleanup(args.base_url, context.get("admin_token"), context)


if __name__ == "__main__":
    main()
