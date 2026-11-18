from __future__ import annotations

import argparse
import fcntl
import hmac
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import re
import signal
import sys
import threading

from .contracts import ContractError, ID, MIB
from .supervisor import ExecutionError, Settings, Supervisor

MAX_BODY = 34 * MIB


def _unique_object(pairs):
    value = {}
    for key, item in pairs:
        if key in value:
            raise ContractError("duplicate JSON field")
        value[key] = item
    return value


def parse_json(body: bytes):
    try:
        return json.loads(body, object_pairs_hook=_unique_object)
    except (ValueError, UnicodeError, RecursionError) as exc:
        raise ContractError("invalid JSON request") from exc


def make_handler(supervisor: Supervisor, token: str):
    # Small semaphore also bounds body decoding before a task slot is acquired.
    requests = threading.BoundedSemaphore(supervisor.settings.concurrency + 2)
    class Handler(BaseHTTPRequestHandler):
        server_version = "MiloOfficeExecutor/1"
        protocol_version = "HTTP/1.1"

        def setup(self):
            super().setup()
            self.connection.settimeout(15)

        def log_message(self, format, *args):
            # Do not log Authorization, scripts, user file names, paths, or tool output.
            pass

        def reply(self, code: int, payload: dict):
            body = json.dumps(payload, ensure_ascii=True, separators=(",", ":")).encode()
            self.send_response(code)
            self.send_header("Content-Type", "application/json")
            self.send_header("Cache-Control", "no-store")
            self.send_header("X-Content-Type-Options", "nosniff")
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Connection", "close")
            self.end_headers()
            self.close_connection = True
            self.wfile.write(body)

        def authorized(self):
            return hmac.compare_digest(self.headers.get("Authorization", "").encode("utf-8"),
                                       ("Bearer " + token).encode("utf-8"))

        def do_GET(self):
            if self.path == "/healthz":
                ready = not supervisor._unhealthy.is_set()
                self.reply(200 if ready else 503, {"status": "ready" if ready else "cleanup_required", "execution_runtime": "runsc"})
            else:
                self.reply(404, {"error_code": "NOT_FOUND"})

        def do_POST(self):
            if not self.authorized():
                self.reply(401, {"error_code": "UNAUTHORIZED"})
                return
            match = re.fullmatch(r"/v1/runs/([A-Za-z0-9_-]+)", self.path)
            if not match or not ID.fullmatch(match[1]):
                self.reply(404, {"error_code": "NOT_FOUND"})
                return
            if not requests.acquire(blocking=False):
                self.reply(429, {"error_code": "EXECUTOR_BUSY"})
                return
            try:
                if self.headers.get_content_type() != "application/json" or self.headers.get("Transfer-Encoding"):
                    raise ContractError("only bounded application/json requests are supported")
                length = self.headers.get("Content-Length", "")
                if not length.isdecimal() or not 0 < int(length) <= MAX_BODY:
                    self.reply(413, {"error_code": "REQUEST_TOO_LARGE"})
                    return
                body = self.rfile.read(int(length))
                if len(body) != int(length):
                    raise ContractError("incomplete request body")
                data = parse_json(body)
                if not isinstance(data, dict) or data.get("run_id") != match[1]:
                    raise ContractError("run ID must match request path")
                if self.headers.get("X-Office-Operation-ID") != data.get("operation_id"):
                    raise ContractError("X-Office-Operation-ID must match operation_id")
                self.reply(200, supervisor.execute(data))
            except ContractError as exc:
                self.reply(400, {"error_code": "INVALID_EXECUTION_REQUEST", "error": str(exc)})
            except ExecutionError as exc:
                self.reply(exc.http_status, {"error_code": exc.code, "error": str(exc)})
            except (OSError, TimeoutError):
                self.reply(503, {"error_code": "EXECUTOR_IO_FAILURE"})
            except Exception:
                self.reply(500, {"error_code": "EXECUTOR_INTERNAL_ERROR"})
            finally:
                requests.release()

        def do_DELETE(self):
            if not self.authorized():
                self.reply(401, {"error_code": "UNAUTHORIZED"})
                return
            match = re.fullmatch(r"/v1/runs/([A-Za-z0-9_-]+)/operations/([A-Za-z0-9_-]+)", self.path)
            if not match or not all(ID.fullmatch(value) for value in match.groups()):
                self.reply(404, {"error_code": "NOT_FOUND"})
                return
            if self.headers.get("X-Office-Operation-ID") != match[2]:
                self.reply(400, {"error_code": "INVALID_EXECUTION_REQUEST"})
                return
            requested = supervisor.cancel(match[1], match[2])
            self.reply(202 if requested else 404, {"cancellation_requested": requested})
    return Handler


def environment_settings() -> Settings:
    return Settings(image=os.environ.get("OFFICE_EXECUTOR_IMAGE", ""),
                    state_dir=Path(os.environ.get("OFFICE_EXECUTOR_STATE_DIR", "/var/lib/milo-office-executor")).resolve(),
                    docker=os.environ.get("OFFICE_EXECUTOR_DOCKER", "docker"),
                    concurrency=int(os.environ.get("OFFICE_EXECUTOR_CONCURRENCY", "2")))


def main():
    parser = argparse.ArgumentParser(description="MiloCode office execution supervisor (Linux/gVisor only)")
    parser.add_argument("command", choices=("serve", "doctor", "execute"))
    args = parser.parse_args()
    try:
        settings = environment_settings()
        # One supervisor per state directory: startup must not invalidate a live peer's journal.
        settings.state_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
        lock = (settings.state_dir / ".supervisor.lock").open("a")
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        supervisor = Supervisor(settings)
        supervisor.doctor()
        if args.command == "doctor":
            print(json.dumps({"status": "ready", "runtime": "runsc", "network": "none"}))
        elif args.command == "execute":
            supervisor.recover()
            raw = sys.stdin.buffer.read(MAX_BODY + 1)
            if len(raw) > MAX_BODY:
                raise ContractError("request body exceeds byte limit")
            print(json.dumps(supervisor.execute(parse_json(raw))))
        else:
            supervisor.recover()
            token_file = os.environ.get("OFFICE_EXECUTOR_TOKEN_FILE")
            token = Path(token_file).read_text().strip() if token_file else os.environ.get("OFFICE_EXECUTOR_TOKEN", "")
            if len(token) < 32 or any(ord(char) < 33 or ord(char) > 126 for char in token):
                raise ValueError("executor service requires a separate random token of at least 32 printable characters")
            host = os.environ.get("OFFICE_EXECUTOR_HOST", "127.0.0.1")
            port = int(os.environ.get("OFFICE_EXECUTOR_PORT", "8093"))
            server = ThreadingHTTPServer((host, port), make_handler(supervisor, token))
            server.daemon_threads = False
            stopped = threading.Event()
            def janitor():
                while not stopped.wait(60):
                    supervisor.cleanup()
            threading.Thread(target=janitor, daemon=True).start()
            def stop(_signum, _frame):
                # An interrupted active operation is not automatically replayable.
                with supervisor._lock:
                    for event in supervisor._active.values():
                        event.set()
                threading.Thread(target=server.shutdown, daemon=True).start()
            signal.signal(signal.SIGTERM, stop)
            signal.signal(signal.SIGINT, stop)
            try:
                server.serve_forever()
            finally:
                stopped.set()
                server.server_close()
    except (ValueError, OSError, ExecutionError) as exc:
        print(json.dumps({"error_code": getattr(exc, "code", "EXECUTOR_CONFIGURATION_ERROR"), "error": str(exc)}), file=sys.stderr)
        raise SystemExit(1) from exc


if __name__ == "__main__":
    main()
