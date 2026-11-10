"""Serve a keyboard-driven view of the P01 privacy review workbook.

The pre-screen flagged every row `CUSTOMER_OR_VENDOR_IDENTITY` by scanning five
columns together, so the flag alone does not say whether the customer name sits in
the published question content or only in the source file name. This view resolves
that per row, marks every occurrence in place, and records the decision straight
back into the two privacy columns.

It binds to localhost only. The workbook contains enterprise evidence: do not
expose this server beyond the local machine.
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

from privacy_review_store import PrivacyReviewStore

DEFAULT_WORKBOOK = "outputs/annotation_workbooks_v1/SEAC_QA_privacy_review.xlsx"
ASSET_ROOT = Path(__file__).resolve().parent / "privacy_review_ui"
CONTENT_TYPES = {
    ".html": "text/html; charset=utf-8",
    ".css": "text/css; charset=utf-8",
    ".js": "text/javascript; charset=utf-8",
}


class Handler(BaseHTTPRequestHandler):
    store: PrivacyReviewStore

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
                    "project": self.store.project,
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
                index = int(request["index"])
                progress = self.store.decide(
                    index,
                    str(request.get("status", "")).upper(),
                    str(request.get("notes", "")),
                )
                if request.get("next") == "undecided":
                    following = self.store.next_undecided(index + 1)
                else:
                    following = min(index + 1, len(self.store.rows) - 1)
                self.send_json(
                    {
                        "progress": progress,
                        "candidate": self.store.payload(following if following is not None else index),
                        "finished": following is None,
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
    parser.add_argument("--project", default="P01")
    parser.add_argument("--corpus", default="")
    parser.add_argument(
        "--identifier",
        nargs="*",
        default=[],
        help="extra sensitive strings to mark; the engagement name is derived from the corpus",
    )
    parser.add_argument("--port", type=int, default=8792)
    parser.add_argument("--no-browser", action="store_true")
    args = parser.parse_args()

    root = Path(__file__).resolve().parents[1]

    def resolve(value: str) -> Path:
        path = Path(value)
        return path if path.is_absolute() else root / path

    workbook = resolve(args.workbook)
    corpus = resolve(args.corpus) if args.corpus else root / "workbench" / args.project / "corpus.json"
    Handler.store = PrivacyReviewStore(
        workbook,
        args.project,
        corpus if corpus.is_file() else None,
        args.identifier,
    )
    progress = Handler.store.progress()
    print(
        json.dumps(
            {
                "workbook": str(workbook),
                "project": args.project,
                "corpus_resolved": corpus.is_file(),
                "marked_identifiers": len(Handler.store.identifiers),
                "rows": progress["total"],
                "decided": progress["decided"],
                "remaining": progress["remaining"],
                "rows_exposing_content": progress["content_exposed"],
                "url": f"http://127.0.0.1:{args.port}/",
            },
            ensure_ascii=False,
            indent=2,
        ),
        # The process then blocks forever, so a buffered line would never reach a
        # reviewer who redirected or piped the output.
        flush=True,
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
