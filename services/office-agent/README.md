# Internal office Agent pilot

This is **phase 0**, not an enabled public workbench. It runs a real
`deepagents.create_deep_agent` / LangGraph graph, with selected Bisheng skills
and middleware. Go remains the only user, routing, scheduling and billing
authority. There is no Bisheng server, second user system or second task queue.

## What works, and what is deliberately gated

- Poll one Go lease at a time; call the original model gateway through a scoped
  bridge; renew the lease independently every 20 seconds; publish safe progress
  and a final answer. The worker never receives a user's long-lived API key.
- Stable model operation IDs derived from run, graph step and canonical request;
  **no SDK/HTTP automatic retry**. Unknown paid outcomes stop for confirmation.
- Actual Bisheng Chinese base-prompt and repeated-tool-loop middleware, complete
  DOCX/XLSX/PPTX skill bundles, templates, inspect and render helpers. See
  `vendor/bisheng/SOURCE_MANIFEST.json` for commit, original paths/hashes and
  modifications. Apache-2.0 license is retained next to these sources.
- Optional **internal executor pilot**: generated Python goes only to
  `services/office-executor`. Files exist in a bounded virtual RAM workspace;
  write areas are `scratch/` and `output/`, uploads and skills are read-only.
  The executor is responsible for the gVisor/network/mount/CPU/memory/disk boundary.
  There is no local generated-code execution, local shell, package installation,
  dynamic MCP, general-purpose subagent, host filesystem backend or old executor
  fallback. Tools execute serially, with one per-run hard tool budget.
- Office execution uses the Go prepare/result operation journal. The full file
  snapshot is integrity checked and restored only in bounded RAM; the Go journal
  and model conversation contain metadata, not base64 output files or credentials.

**Not implemented / not promised:** durable checkpoint fencing, restarting a
partial graph, public artifact upload/download, visual image inspection, resource
restoration, browser search, real-time token delivery, or resumable `ask_user`.
`InMemorySaver` is only for this isolated pilot. A manifest requesting durable
resume or resource inputs fails before a model call. Without a persistent saver,
`ask_user` is not exposed as an interrupting tool: the model can ask a normal
question in its final reply. A repeated completed office operation is **not**
regenerated to recreate missing files. Those gates must remain until the design's
resource publishing and lease-fenced persistent-checkpoint acceptance tests pass.

The Go model bridge buffers each bounded result before returning it. This pilot
therefore does not claim low-latency, user-visible streaming. It also does not
claim that all available gateway models support tools: test the public model ID
and protocol before enabling office tools for it. Token output parameters are
sent through the existing OpenAI-compatible bridge, without default temperature
or a separate, potentially stale provider model catalog.

## Run locally

Python 3.12 is used for validation; dependencies are pinned by `uv.lock`.
No Office packages or browser binaries are installed into the trusted Agent.

```sh
cd services/office-agent
uv sync --python 3.12 --frozen
uv run pytest -q
export OFFICE_CONTROL_ORIGIN=https://your-internal-control-origin
export OFFICE_WORKER_TOKEN=your-separate-worker-bootstrap-token
uv run office-agent --once
```

Set the real token in a secret manager or protected process environment. Do not
commit it or put it in a run manifest file. `--once` claims at most one run; omit
it for the bounded polling loop. An empty queue sleeps five seconds. A process
manager can restart a failed worker; it must not invent a replay of a paid run.

`OFFICE_EXECUTOR_PILOT_ENABLED=true` additionally requires
`OFFICE_EXECUTOR_ORIGIN` and `OFFICE_EXECUTOR_TOKEN`. This is for isolated Linux
acceptance only, after the executor's doctor confirms `runsc`; it is **not** a
public feature switch. The normal chat pilot does not need an executor or Office
image. The executor receives no model/Go lease credential, only its separate
internal service credential and a bounded task payload. Credentials are never
added to graph state, tool arguments, operation IDs or logs. External LangSmith
tracing is explicitly disabled even if enabled in the calling environment.

## Control-plane protocol

The configured origin is trusted deployment configuration. The manifest can only
provide the exact relative model path for its own run; it cannot redirect Bearer
credentials to an arbitrary upstream.

1. `POST /internal/office-agent/v1/claim`, bootstrap Bearer, body `{"worker_id":...}`.
   `204` is no work. A successful response contains the `RunManifest` declared in
   `contracts.py` (raw JSON or the standard `{code,data}` envelope).
2. Run endpoints use the short-lived `lease_token` Bearer and decimal
   `X-Office-Lease: <lease_epoch>`. A successful heartbeat replaces the in-memory
   token; it never mutates the graph/checkpoint.
3. `POST /runs/<id>/model/chat/completions` under the internal prefix additionally
   uses `X-Office-Operation-ID: model-<sha256>`. The body is OpenAI-compatible,
   not a provider's SDK client. All actual model calls pass the original router.
4. `POST /runs/<id>/tools/<operation_id>/prepare`, body `{"input":...}`; execute
   only for an affirmative `execute:true`. Then POST `/result`, body
   `{"output":<safe result metadata>,"http_status":200}`. A transport uncertainty
   is never treated as permission to retry.
5. `POST /runs/<id>/events`, body `{"event_id":...,"type":"progress","payload":{"message":...}}`.
   `POST /runs/<id>/finish`, body `{status,content,error_code,error_message}`.
   The worker posts only `completed` or `failed`. For `needs_confirmation`, Go's
   operation inbox/recovery sweep owns the state; the worker stops and does not
   attempt a finish write with a lease Go may already have fenced.

All run paths above start with `/internal/office-agent/v1`. No raw LangChain
callbacks, model reasoning, script source, stack traces or internal paths are
forwarded as public progress. The worker runs one graph at a time; Go enforces
global/user budgets and operation identity independently.

## Office executor protocol

`POST <OFFICE_EXECUTOR_ORIGIN>/v1/runs/<run_id>` uses a separate Bearer and
`X-Office-Operation-ID` matching the body. Body:

```json
{
  "version": 1,
  "run_id": "UUID",
  "operation_id": "office-SHA256",
  "lease_epoch": 1,
  "script": "Python source",
  "inputs": [{"path":"scratch/input.txt","sha256":"...","content_base64":"..."}],
  "limits": {"timeout_seconds": 180}
}
```

Paths use Bisheng's `uploads/`, `scratch/`, `output/`, `skills/` conventions;
the isolated script's cwd is `/workspace`. Skills are mounted read-only by the
fixed image. Inputs and outputs are bounded, inline only for phase 0. The response
contains `run_id,operation_id,lease_epoch,version,status,exit_code,stdout,stderr,outputs,workspace_manifest`.
`workspace_manifest` is the **complete** output/scratch snapshot; every file has
`path,size,sha256,mime,content_base64`. `outputs` is only output-file metadata
(`path,size,sha256,mime`), so file bytes are never duplicated on the wire. The
client's wire cap matches the executor's 40 MiB ceiling. The client checks paths, counts, bytes and
integrity before atomically replacing its writable snapshot. Only metadata and
bounded logs go back into the model. An interrupted request attempts the narrow
executor cancellation endpoint; Go fences late task publication regardless.
`unknown`/`cancelled` stop the graph without normal result completion;
`timed_out`, `output_limit` and `invalid_output` record the known failure and stop.
A known script failure can be corrected within the remaining shared budget;
uncertain execution is never handed back to the model as permission to retry.

## Source changes and validation

Vendor commit: `dataelement/bisheng@a8e5be89c1b8d3a2e512c772e2a38de409336dc8`.
The selected source is recorded file by file. The original prompt/loop middleware
is loaded unmodified from fixed application paths. Skill instructions were adapted
to this tool name, offline environment and budgets; DOCX/PPTX rendering uses
Poppler rather than PyMuPDF. This avoids adding that library to the distribution;
font and template licensing still needs an asset review before public release.

Tests exercise actual DeepAgents construction and tool dispatch with mocked
network boundaries: model bridge auth/operation identity, no forbidden automatic
tools, one-call behavior, no retries on uncertainty, parallel-tool serialization,
executor journal/snapshot exchange, traversal and skill immutability, source
integrity and explicit unsupported-feature gates. They do not claim to prove
gVisor isolation or real LibreOffice rendering. The sibling executor owns real
Linux document probes and resource/isolation acceptance.
