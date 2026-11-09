from __future__ import annotations

import os
import shlex
import subprocess
import time
from pathlib import Path

PROJECT_ROOT = Path(__file__).resolve().parents[1]
START_SCRIPT = PROJECT_ROOT / "deploy/container/start.sh"


def test_container_start_uses_auto_port_and_preserves_standard_streams(tmp_path: Path):
    app_home = tmp_path / "app"
    fake_bin = tmp_path / "bin"
    log_root = tmp_path / "logs"
    pid_file = tmp_path / "service.pid"
    invocations = tmp_path / "python-args.txt"
    test_script = tmp_path / "start.sh"
    app_home.mkdir()
    fake_bin.mkdir()

    script_text = START_SCRIPT.read_text(encoding="utf-8")
    script_text = script_text.replace(
        "APP_HOME=/home/web_server/agentk-capability-demo",
        f"APP_HOME={shlex.quote(str(app_home))}",
    ).replace(
        "LOG_DIR=/home/web_server/kuaishou-runner/logs",
        f"LOG_DIR={shlex.quote(str(log_root))}",
    ).replace(
        "PID_FILE=/tmp/agentk-capability-demo.pid",
        f"PID_FILE={shlex.quote(str(pid_file))}",
    )
    test_script.write_text(script_text, encoding="utf-8")

    fake_python = fake_bin / "python3"
    fake_python.write_text(
        (
            '#!/bin/sh\nprintf "%s\\n" "$*" >> "$START_CAPTURE"\n'
            'printf "child started: %s\\n" "$*"\n'
            'trap "exit 0" TERM INT\n'
            'while :; do sleep 1; done\n'
        ),
        encoding="utf-8",
    )
    fake_python.chmod(0o755)

    env = {
        **os.environ,
        "PATH": f"{fake_bin}:{os.environ['PATH']}",
        "AUTO_PORT0": "22941",
        "START_CAPTURE": str(invocations),
    }
    process = subprocess.Popen(
        ["/bin/sh", str(test_script)],
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        if invocations.exists() and len(invocations.read_text(encoding="utf-8").splitlines()) == 2:
            break
        time.sleep(0.05)
    else:
        process.kill()
        raise AssertionError("start.sh did not launch both child processes")

    assert pid_file.read_text(encoding="utf-8").strip().isdigit()
    process.terminate()
    stdout, stderr = process.communicate(timeout=5)

    assert process.returncode == 0, stderr
    stdout_lines = stdout.splitlines()
    assert stdout_lines[0] == "[agentk-capability-demo] starting Narrato worker"
    assert set(stdout_lines) == {
        "[agentk-capability-demo] starting Narrato worker",
        "[agentk-capability-demo] starting uvicorn on port 22941",
        "child started: -m agentk_capability_demo.agent100.worker_cli narrato",
        "child started: -m uvicorn agentk_capability_demo.app:create_app --factory --host 0.0.0.0 --port 22941",
    }
    assert stderr == ""
    assert not pid_file.exists()
    assert sorted(invocations.read_text(encoding="utf-8").splitlines()) == [
        "-m agentk_capability_demo.agent100.worker_cli narrato",
        "-m uvicorn agentk_capability_demo.app:create_app --factory --host 0.0.0.0 --port 22941",
    ]
