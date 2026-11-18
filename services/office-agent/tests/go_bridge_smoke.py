"""Cross-process Go integration probe: stdin manifest, argv control origin.

Use the project's pinned Python environment. This runs the exact same real
DeepAgents runtime and polling-worker completion logic; no fake agent loop.
Only a public RunResult is written to stdout. No tokens are accepted in argv.
"""
from __future__ import annotations

import asyncio
import json
import sys

import httpx

from office_agent.contracts import RunManifest, RuntimeFailure
from office_agent.worker import process_manifest


async def smoke(origin: str):
    raw = sys.stdin.buffer.read(1024 * 1024 + 1)
    if len(raw) > 1024 * 1024:
        raise RuntimeFailure("MANIFEST_TOO_LARGE", "试验清单超过大小限制")
    manifest = RunManifest.model_validate_json(raw)
    async with httpx.AsyncClient(timeout=httpx.Timeout(30, connect=3), trust_env=False) as client:
        result = await process_manifest(origin, manifest, client)
    print(result.model_dump_json())
    return 0 if result.status == "completed" else 1


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit("usage: python tests/go_bridge_smoke.py CONTROL_ORIGIN < manifest.json")
    try:
        raise SystemExit(asyncio.run(smoke(sys.argv[1])))
    except RuntimeFailure as exc:
        print(exc.result().model_dump_json())
        raise SystemExit(1) from None
    except (ValueError, TypeError):
        print(json.dumps({"status": "failed", "error_code": "INVALID_MANIFEST",
                          "error_message": "试验清单无效"}))
        raise SystemExit(1) from None
