"""Serve a keyboard-driven review of the QA approval workbook.

Deciding 225 retrieval questions in a spreadsheet is slow for a specific reason:
under the multi-evidence model a candidate is only judgeable once all corpus
locations of its evidence are read side by side, and the workbook can only list
truncated unit ids. This tool resolves those locations, highlights the evidence
span inside each one, and writes each decision straight back to the workbook.

It binds to localhost only. The workbook and the resolved evidence contain
enterprise document fragments: do not expose this server beyond the local machine.
"""

from __future__ import annotations

import argparse
import json
import sys
import webbrowser
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse

sys.path.insert(0, str(Path(__file__).resolve().parent))

from qa_approval_store import ApprovalStore

DEFAULT_WORKBOOK = "outputs/qa_approval_v1/SEAC_QA_approval.xlsx"
ASSET_ROOT = Path(__file__).resolve().parent / "qa_approval_ui"
CONTENT_TYPES = {
    ".html": "text/html; charset=utf-8",
    ".css": "text/css; charset=utf-8",
    ".js": "text/javascript; charset=utf-8",
}


class Handler(BaseHTTPRequestHandler):
    store: ApprovalStore

    def log_message(self, format: str, *args) -> None:  # noqa: A002 - signature fixed by base class
        return

    def send_json(self, payload: dict, status: int = 200) -> None:
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def send_asset(self, name: str) -> None:
        path = (ASSET_ROOT / name).resolve()
        if not path.is_file() or ASSET_ROOT.resolve() not in path.parents:
            self.send_error(404)
            return
        body = path.read_bytes()
        self.send_response(200)
        self.send_header("Content-Type", CONTENT_TYPES.get(path.suffix, "application/octet-stream"))
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self) -> None:  # noqa: N802 - name fixed by base class
        route = urlparse(self.path).path
        if route in {"/", "/index.html"}:
            self.send_asset("index.html")
        elif route in {"/app.js", "/styles.css"}:
            self.send_asset(route.lstrip("/"))
        elif route == "/api/state":
            index = self.store.next_undecided(0)
            self.send_json(
                {
                    "workbook": self.store.path.name,
                    "projects": self.store.projects,
                    "progress": self.store.progress(),
                    "candidate": self.store.payload(index if index is not None else 0),
                    "finished": index is None,
                }
            )
        else:
            self.send_error(404)

    def do_POST(self) -> None:  # noqa: N802 - name fixed by base class
        route = urlparse(self.path).path
        length = int(self.headers.get("Content-Length", 0))
        request = json.loads(self.rfile.read(length) or b"{}")
        try:
            if route == "/api/decide":
                current = int(request["index"])
                progress = self.store.decide(
                    current,
                    str(request.get("approval", "")),
                    str(request.get("privacy", "")),
                    str(request.get("difficulty", "")),
                    str(request.get("notes", "")),
                )
                index = self.store.next_undecided(current + 1)
                self.send_json(
                    {
                        "progress": progress,
                        "candidate": self.store.payload(index if index is not None else current),
                        "finished": index is None,
                    }
                )
            elif route == "/api/goto":
                index = max(0, min(int(request["index"]), len(self.store.rows) - 1))
                self.send_json({"progress": self.store.progress(), "candidate": self.store.payload(index)})
            elif route == "/api/next-undecided":
                index = self.store.next_undecided(int(request.get("index", 0)) + 1)
                self.send_json(
                    {
                        "progress": self.store.progress(),
                        "candidate": self.store.payload(index if index is not None else 0),
                        "finished": index is None,
                    }
                )
            else:
                self.send_error(404)
        except (KeyError, ValueError) as error:
            self.send_json({"error": str(error)}, status=400)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--workbook", default=DEFAULT_WORKBOOK)
    parser.add_argument("--workbench-root", default="workbench")
    parser.add_argument("--port", type=int, default=8789)
    parser.add_argument("--no-browser", action="store_true")
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]
    workbook = Path(args.workbook)
    if not workbook.is_absolute():
        workbook = root / workbook
    workbench = Path(args.workbench_root)
    if not workbench.is_absolute():
        workbench = root / workbench

    Handler.store = ApprovalStore(workbook, workbench)
    progress = Handler.store.progress()
    print(
        json.dumps(
            {
                "workbook": str(workbook),
                "rows": progress["total"],
                "decided": progress["decided"],
                "remaining": progress["remaining"],
                "url": f"http://127.0.0.1:{args.port}/",
            },
            ensure_ascii=False,
            indent=2,
        )
    )
    try:
        server = ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
    except OSError as error:
        # A stale instance keeps the port and stays bound to a workbook. Say so
        # plainly rather than dumping a traceback on a reviewer.
        raise SystemExit(
            f"port {args.port} is already in use: another review server is probably "
            f"still running. Open http://127.0.0.1:{args.port}/ to use it, or pass a "
            f"different --port. ({error})"
        )
    if not args.no_browser:
        webbrowser.open(f"http://127.0.0.1:{args.port}/")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nstopped; every decision was already saved to the workbook")
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
