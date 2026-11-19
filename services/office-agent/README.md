# Sub2API chat and office worker

This internal Python worker uses the existing Go router, user system and billing.
It has no public HTTP server, provider API key, database DSN or second user system.
Ordinary chat is a separate lightweight preset; office mode runs a real
`deepagents.create_deep_agent` / LangGraph graph with selected attributed Bisheng
middleware and complete DOCX/XLSX/PPTX skill bundles.

## Runtime boundaries

- `OFFICE_WORKER_PRESET=chat` creates no filesystem or office executor. Search and
  webpage reading are exposed only when the server manifest enables them. Simple
  chat makes one normal model call, with no automatic planning or title call.
- `OFFICE_WORKER_PRESET=office` has a bounded virtual RAM workspace. Generated
  Python is sent only to the separate Linux/gVisor office executor. There is no
  local shell, local generated-code execution, package installation, arbitrary MCP,
  host filesystem backend, general-purpose subagent or host execution fallback.
- All paid model operations use the original Go bridge and operation inbox. Real
  SSE tool-call fragments and usage are consumed without SDK retries. Go alone
  persists public `assistant.delta` events. An incomplete or uncertain operation
  stops instead of being replayed.
- Tools run serially. Durable distinct-tool budgets survive worker replacement.
  Lease loss cancels the active graph and attempts executor cancellation. Epochs
  fence dispatch but do not rename a completed operation during recovery.
- Worker bootstrap and executor credentials are separate. User API keys and lease
  credentials never enter graph state or model tool arguments. External LangSmith
  tracing is disabled. Tools, attachments and webpage content cannot grant tools.

## Persistence and recovery

`GoCheckpointSaver` persists a bounded JSON document through Go's lease-fenced
PostgreSQL CAS endpoint. It uses an allowlisted codec, never pickle or arbitrary
class imports. It keeps four complete graph snapshots within 1 MiB. The graph
explicitly uses the standard `add_messages` reducer: DeepAgents' default delta
channel cannot safely be pruned to four ancestors.

Files are stored separately through private run workspace snapshots, at most 64
files and 24 MiB decoded bytes. Snapshot GET supports latest (no query), exact
immutable version (`?version=N`) and initial authorized inputs (`?version=0`).
`head_version` is the next-write CAS head; it can differ from the restored version
when a crash left an unreferenced newer snapshot. Files are path/hash/size checked
before atomic restoration. Office outputs become durable before a completed tool
journal record; replay restores the recorded snapshot and never reruns the script.

`ask_user` checkpoints then transitions to `waiting_user`. The server requeues the
same run with `{request_id,answers:[{question_id,values:string[]}]}`; a new worker
resumes its graph. Recovery without a new clarification continues pending steps
instead of reinserting the original user messages. Missing recovery state fails
before new paid operations.

Completed files under `output/` are published as immutable private resources.
Optional `output_parent={resource_id,version_id,path}` comes only from an explicit
user "continue modifying" action verified by Go. Only that exact output path
creates a new version of the original resource; extra files become new resources.
A failed or unknown publication never yields a fabricated success/download link.
Representative preview images use `scratch/previews/<full-output-filename>.png`
(or `.jpg`) and are published with the resource.

Uncertain outcomes are reported once to Go's `/suspend` endpoint. Go locks and
fences the matching current lease, writes `run.needs_confirmation` immediately,
and preserves original dispatch identities for late inbox deposits. Already
fenced or cancelled runs cannot be revived. A five-minute expired-credential
grace applies only to reporting a stop, never to model/tool dispatch or renewal.

## Task budgets and public model profiles

Workbench settings define per-user chat/office concurrency (defaults 3/1, caps
8/2), model/tool call counts, per-call output and active run duration. Counts,
output and duration cannot exceed deployment limits. Each run snapshots these
values; official model context/output capabilities remain unchanged.

An optional `max_run_credits` uses original `usage_logs.charge_credits` and exact
run/user/key/request correlation. Zero adds no task credit cap. With a cap,
model admission is serialized per run; one in-flight supplier response can
exceed the remaining cap because it settles afterwards. A successful previous
call must appear in the original asynchronous ledger before another call is
admitted. Go waits at most five seconds by polling unsent admission only; if
settlement is still absent, it writes a clear `BILLING_PENDING` suspension.
Completed inbox replay remains readable after the cap is reached. No supplier
request, pricing calculation or billing write is duplicated.

Custom public IDs may explicitly bind an official profile through the independent
`workbench_model_profiles_v1` setting. Only the public ID and that binding are
used; supplier model mappings and desktop-client settings are never consulted.
The official remote profile directory is shared read-only. Binding `override`
must remain false, so task settings cannot invent or replace official maxima.

## Context and visual checks

The manifest accepts at most 2,000 selected messages and 2 MiB decoded text. Long
contexts are explicitly compacted through the same billed model bridge, then
saved by conversation CAS before the final question is answered. The current
question stays intact. Previous turns are not silently dropped. A preflight
checks that at most four compaction calls can cover the selected history; if not,
it asks for fewer selected turns or a larger-context model before spending calls.

Compaction uses server-provided official `context_window`, configured output and
transport bounds. UTF-8 bytes are a conservative token upper bound, not a claim
of exact model tokenization. Compaction requests have purpose header
`X-Office-Model-Purpose: context_compaction` and a 1 MiB request limit; normal model
requests retain the 256 KiB limit. Go suppresses public answer deltas for this
purpose while keeping the original usage/billing path. A saved summary has a
stable assistant-message boundary and SHA256 of its ordered source message IDs.

When Go declares `supports_vision=true`, office mode exposes
`inspect_rendered_image`. It validates a task-local scratch PNG/JPEG (96 KiB,
4096 maximum edge, four million maximum pixels), freezes its bytes under a hash
path, and stores only an image reference in the graph. On the next normal model
call, the adapter temporarily expands only the latest image as a user multimodal
message. Image bytes/data URLs never enter checkpoints. There is no extra hidden
vision call. Models without this capability can perform structural, recalculation
and render checks, and must not claim visual inspection.

## Running and building

Python 3.12 is used for validation. Dependencies are frozen in `uv.lock`;
`requirements.lock` is its production export with hashes. No Office libraries or
browser binaries are installed in the trusted worker.

```sh
cd services/office-agent
uv sync --python 3.12 --frozen
uv run pytest -q
export OFFICE_CONTROL_ORIGIN=https://your-internal-control-origin
export OFFICE_WORKER_TOKEN=separate-worker-bootstrap-token
export OFFICE_WORKER_PRESET=chat
uv run office-agent --once
```

Omit `--once` for continuous polling. Empty queues poll every five seconds;
heartbeats renew an active lease every twenty seconds. Use separate chat and
office processes. Office additionally requires `OFFICE_EXECUTOR_PILOT_ENABLED=true`,
`OFFICE_EXECUTOR_ORIGIN` and `OFFICE_EXECUTOR_TOKEN`. The office worker reports
cached executor health every thirty seconds from `/healthz`; chat reports null.

The container runs as UID/GID 11001 and requires a digest-pinned base image:

```sh
docker build --build-arg PYTHON_BASE_IMAGE='python@sha256:<reviewed-digest>' .
```

Run it with a read-only root filesystem, bounded tmpfs/memory/process limits and
private control/executor network access. There is no Docker socket in the worker.
Do not place tokens in manifests, logs, source files or image build arguments.
Public office enablement still requires actual Linux/runsc acceptance on its
isolated executor; unit tests do not substitute for that deployment check.

## Internal protocol

All run paths have prefix `/internal/office-agent/v1/runs/<run_id>/` and use the
short-lived lease Bearer plus `X-Office-Lease`. Mutating operations carry stable
operation IDs; no request follows redirects or automatically retries.

- `POST /internal/office-agent/v1/claim`: bootstrap Bearer,
  `{worker_id,preset,executor_ready}`. HTTP 204 means no task.
- `POST heartbeat`: replaces only the worker's in-memory lease token.
- `POST suspend`: `{code}` with a current signed lease (or the stop-only grace);
  server-owned safe text and immediate fencing, no automatic operation replay.
- `POST model/chat/completions`: original OpenAI-shaped model bridge, real SSE or
  durable completed response replay, with `X-Office-Operation-ID`.
- `GET/POST checkpoint`: `{version,data}` / `{expected_version,write_id,data}`.
- `GET/POST workspace`: immutable file snapshot and latest-head CAS.
- `POST tools/<id>/prepare` and `/result`: dispatch once, store safe metadata and
  workspace version, never base64 document bytes in tool/model messages.
- `POST wait`: `{request_id,payload}` after checkpoint persistence.
- `POST context-summary`: `{operation_id,expected_version,content,
  through_message_id,source_hash}` with Go-owned conversation/boundary validation.
- `POST artifacts`: snapshot-backed publication and optional explicit parent.
- `POST events`: safe progress and task/tool cards. Artifact publication events
  belong to Go's ready-resource transaction. No raw
  LangChain callbacks, script source, reasoning or stack traces are public.
- `POST finish`: only known completed/failed outcomes. `waiting_user` is owned by
  `wait`; unknown outcomes remain owned by Go recovery and are not force-finished.

## Sources and verification

Bisheng source commit:
`dataelement/bisheng@a8e5be89c1b8d3a2e512c772e2a38de409336dc8`.
`vendor/bisheng/SOURCE_MANIFEST.json` records every selected source file, original
path/hash and adaptation. Apache-2.0 attribution is preserved. The original prompt
and loop middleware are loaded from fixed application paths; DOCX/PPTX rendering
uses Poppler rather than PyMuPDF. Font/template asset review remains a deployment
responsibility.

Tests exercise actual DeepAgents construction, streaming/tool fragments,
clarification interruption and reconstruction, full-state checkpoint recovery,
completed tool replay across lease epochs, explicit resource versions, image
references without checkpoint bytes, context compaction ordering and uncertainty,
lease cancellation, bounded paths and vendored source integrity. Model/Go/executor
network boundaries are mocked. These tests do not claim real upstream model
quality, Linux isolation, or LibreOffice visual rendering acceptance.
