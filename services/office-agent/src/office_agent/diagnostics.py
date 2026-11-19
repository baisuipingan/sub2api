"""Bounded internal exception classifications, never exception text or frames."""
from __future__ import annotations

import json
import re
import sys

_LABEL = re.compile(r"[A-Za-z_][A-Za-z0-9_]{0,63}\Z")
_COMPONENTS = {"chat", "office", "worker"}
_STAGES = {"context", "model", "search", "restore", "graph_setup", "graph", "clarification", "archive", "poll"}


def report_runtime_exception(component, manifest, error, *, stage):
    """Classify a swallowed error without serializing its message or payload."""
    kind = type(error)
    name = kind.__name__
    module = kind.__module__.split(".", 1)[0]
    diagnostic = {
        "event": "office.runtime_exception",
        "component": component if component in _COMPONENTS else "worker",
        "stage": stage if stage in _STAGES else "poll",
        "error_type": name if _LABEL.fullmatch(name) else "Exception",
        "error_module": module if _LABEL.fullmatch(module) else "unknown",
    }
    if manifest is not None:
        # RunManifest validates this as a UUID; no prompts, model IDs, filenames,
        # credentials, exception repr or traceback data enter the log record.
        diagnostic["run_id"] = str(manifest.run_id)
    try:
        print(json.dumps(diagnostic, separators=(",", ":")), file=sys.stderr, flush=True)
    except (OSError, ValueError):
        pass  # Diagnostics cannot turn a safe public failure into a worker crash.
