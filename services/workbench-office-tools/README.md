# Workbench Office tool service

The service processes fixed Word, Excel, PowerPoint and ZIP operations. The caller authorizes a file version and provides its name, SHA-256, size and short-lived storage link. The worker verifies the file after one download and keeps a short-lived local cache keyed by version and hash. Each operation runs in a separate bounded subprocess with its own temporary directory. Sub2API owns file version publication and conversation state.

## Runtime

Build the directory as the Docker context. Supply the same reviewed Python base digest as the rest of the deployment. Mount a persistent volume at `/var/lib/office-tools` for the SQLite journal, input cache and delivery files.

| Environment variable | Use |
|---|---|
| `OFFICE_TOOLS_TOKEN` | Internal Bearer credential, at least 24 characters |
| `OFFICE_TOOLS_STORAGE_ORIGINS` | Comma-separated allowlist of exact HTTP(S) object-storage origins |
| `OFFICE_TOOLS_CONCURRENCY` | Startup limit before Sub2API synchronizes the active setting, defaults to two |
| `OFFICE_TOOLS_RECALC_ENABLED` | Set to `true` to enable the optional LibreOffice Calc operation |
| `OFFICE_TOOLS_STATE_DIR` | Persistent state path, defaults to `/var/lib/office-tools` |
| `OFFICE_TOOLS_HOST`, `OFFICE_TOOLS_PORT` | Bind address and port, default to 0.0.0.0 and 8094 |

The application starts with a read-only container root and an internal port. Its only external requests are to approved storage origins. Calc uses a private profile, rejects external references, disables macros and external link updates, and runs with an internet socket filter on Linux. It copies only values cached by Calc into the original worksheets and keeps the original formulas and remaining workbook parts. Formula errors are returned by sheet and cell.

## HTTP protocol

`PUT /v1/concurrency` accepts the Sub2API setting as `{"concurrency": 3, "revision": 42}`. Send the Bearer credential and the current `X-Office-Node-ID`. The worker applies a newer revision in place and returns the current `GET /v1/status` shape, including `concurrency_revision`. Earlier revisions and retries of an applied revision return the current status. Lowering the limit allows running operations to finish. The revision starts at zero on restart so Sub2API can synchronize the configured limit.

`POST /v1/jobs/{id}` accepts an action, an arguments object and an input object when applicable. Input fields are `version_id`, `name`, `sha256`, `bytes`, and `url`. Requests with the same ID and same immutable inputs return the recorded receipt. A refreshed signed URL does not change the operation fingerprint.

`GET /v1/jobs/{id}` returns the receipt. `DELETE /v1/jobs/{id}` records cancellation, including when the original dispatch is still in transit. States are `running`, `completed`, `partial`, `failed`, and `cancelled`. Receipts may include a tool result, structured error and output metadata. A node restart preserves terminal receipts and records interrupted work as failed.

`POST /v1/jobs/{id}/upload` uploads a completed output to the caller's short-lived signed PUT URL. The signed `Content-MD5` must match the Base64 checksum in the output receipt. If the caller signs a conditional write, the worker reports a pre-existing object as HTTP 412 so the caller can verify the stored bytes before publishing. Legacy stores can use a checksum-bound upload without the condition. `GET /v1/jobs/{id}/output` returns the bounded result bytes to an authenticated internal caller. Output bytes are kept for up to one hour. The service cleans expired input cache and receipts as it receives requests.

Sub2API defaults to create-only uploads. `OFFICE_AGENT_TOOLS_LEGACY_PUT=true` is the compatibility setting for older S3-compatible stores that lack conditional PUT, such as the isolated MinIO fixture.

## OfficeCLI comparison

The sample compared OfficeCLI 1.0.163 with the selected structured Python handlers. The inputs contained Chinese title, header and footer text and styled Word runs, a 500-row Excel file with formulas and a chart, and a three-slide PowerPoint deck with a template layout and tables. Both implementations passed independent readback. Both were converted to PDF by LibreOffice to check renderability. This was a macOS ARM64 sample, with three process invocations for each operation and without HTTP or object-store time.

| Operation | OfficeCLI median wall time and process RSS | Structured handler median wall time and process RSS |
|---|---:|---:|
| DOCX replacement | 0.69 s / 68.7 MiB | 0.17 s / 62.7 MiB |
| XLSX edit | 0.81 s / 33.0 MiB | 0.22 s / 62.8 MiB |
| PPTX edit | 0.87 s / 32.3 MiB | 0.20 s / 63.3 MiB |

OfficeCLI was run with `OFFICECLI_RESIDENT_FLUSH=each`. Its default resident mode returned an acknowledgement for an Excel or PowerPoint edit before the modified file was visible to an independent reader in a separate process. The structured handlers verified re-opened result files in every measured path. OfficeCLI remains a candidate for a future native operation when its coverage or resource profile benefits that specific workload. [OfficeCLI 1.0.163](https://github.com/iOfficeAI/OfficeCLI/releases/tag/v1.0.163)

Run the tests from this directory with `python -m unittest discover -s tests -v`. The live Calc value test runs when LibreOffice is installed.
