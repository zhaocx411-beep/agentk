#!/usr/bin/env python3
"""端到端联调：用 AgentK API 创建 Session（挂本目录 profile.json）、
启动 ffmpeg_tool Pipeline、轮询到终态并打印输出。

凭证与地址从环境变量读取；也支持本目录 .env 文件（KEY=VALUE 每行一条，
已 gitignore，API Key 不要提交）：

    AGENTK_BASE_URL   默认 https://agentk-api.staging.kuaishou.com
    AGENTK_API_KEY    必填，Service Principal API Key

用法：

    uv run --no-sync python verify_agentk.py --args '{"op":"probe","video":"db:upload:ylab-stunt:<key>"}'
    uv run --no-sync python verify_agentk.py --session ses_xxx --args '...'   # 复用已有 Session
"""

import argparse
import json
import os
import sys
import time
from pathlib import Path

import requests

HERE = Path(__file__).resolve().parent
TERMINAL_STATUSES = {"completed", "failed", "canceled", "cancelled", "terminated"}


def load_dotenv() -> None:
    env_file = HERE / ".env"
    if not env_file.is_file():
        return
    for line in env_file.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, _, value = line.partition("=")
        os.environ.setdefault(key.strip(), value.strip().strip("'\""))


def main() -> None:
    load_dotenv()
    parser = argparse.ArgumentParser(description="verify ffmpeg_tool via AgentK API")
    parser.add_argument(
        "--args",
        default='{"op": "probe", "video": ""}',
        help='Pipeline Args，JSON 对象字符串，如 \'{"op":"probe","video":"db:upload:ylab-stunt:key"}\'',
    )
    parser.add_argument("--session", default="", help="复用已有 Session ID（缺省新建）")
    parser.add_argument("--profile", default=str(HERE / "profile.json"), help="Session Profile 文件")
    parser.add_argument("--poll-interval", type=float, default=5.0)
    parser.add_argument("--timeout", type=float, default=900.0, help="轮询总超时（秒）")
    opts = parser.parse_args()

    base_url = os.environ.get(
        "AGENTK_BASE_URL", "https://agentk-api.staging.kuaishou.com"
    ).rstrip("/")
    api_key = os.environ.get("AGENTK_API_KEY", "")
    if not api_key:
        sys.exit("缺少 AGENTK_API_KEY（环境变量或本目录 .env）")
    try:
        args_obj = json.loads(opts.args)
    except ValueError:
        sys.exit("--args 必须是 JSON 对象字符串")

    http = requests.Session()
    http.headers.update(
        {"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"}
    )

    def call(method: str, path: str, payload: dict | None = None) -> dict:
        resp = http.request(method, f"{base_url}{path}", json=payload, timeout=60)
        if resp.status_code >= 400:
            sys.exit(f"{method} {path} -> {resp.status_code}: {resp.text[:800]}")
        return resp.json()

    session_id = opts.session
    if not session_id:
        profile = json.loads(Path(opts.profile).read_text())
        created = call(
            "POST",
            "/v1/sessions",
            {"title": "ffmpeg tool verify", "profile": profile},
        )
        session_id = created["id"]
        print(f"session created: {session_id}")
    else:
        print(f"reusing session: {session_id}")

    run = call(
        "POST",
        f"/v1/sessions/{session_id}/pipeline-runs",
        {"pipeline": "ffmpeg_tool", "args": args_obj},
    )
    exec_id = run["id"]
    print(f"pipeline started: {exec_id}")

    deadline = time.monotonic() + opts.timeout
    status = run.get("status", "")
    while status not in TERMINAL_STATUSES:
        if time.monotonic() > deadline:
            sys.exit(f"轮询超时（{opts.timeout}s），最后状态: {status}")
        time.sleep(opts.poll_interval)
        run = call("GET", f"/v1/sessions/{session_id}/pipeline-runs/{exec_id}")
        status = run.get("status", "")
        print(f"status: {status}")

    print(json.dumps(run, ensure_ascii=False, indent=2))
    if status != "completed":
        sys.exit(1)


if __name__ == "__main__":
    main()
