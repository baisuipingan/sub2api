# Independent office executor acceptance

Verified 2026-11-19 on the separate Linux test host, never on production.
The app/database/Redis/private object store use the dedicated
`milo-office-test` project and synthetic data. No production credentials or
user data were copied.

## Genuine runtime and Office results

`accept_executor.sh` invokes the fixed executor with Docker runtime `runsc`,
network `none`, UID 65532, a read-only root, no capabilities, no task secrets or
Docker socket, and CPU/memory/PID/tmpfs/output limits. The trusted acceptance
driver alone receives the Docker socket and has its own recovery journal.

| Probe | Observed outcome |
| --- | --- |
| Read-only inputs/image, nonroot, no secrets/socket/network | `completed` |
| Real vendored DOCX/XLSX/PPTX inspection, recalculation and rendering | `completed`, 28.6 seconds, 9 workspace files |
| Symlink output | `invalid_output`, no files |
| Execution timeout | `timed_out`, no files |
| Output byte ceiling | `failed`; cannot publish as a successful tool result |
| tmpfs disk ceiling | ENOSPC caught by fixture, `completed` |
| PID pressure with a 64-PID host cgroup ceiling | Sentry/task abort, `unknown`, no verified workspace |
| 512-MiB allocation under a 256-MiB memory ceiling | Task abort, `unknown`, no verified workspace |

PID/memory aborts deliberately remain uncertain, with no automatic replay.
The PID ceiling also covers gVisor's supporting processes; 32 PIDs could not
start the runtime, so the pressure fixture uses 64. Production's fixed default
is 128. The test did not weaken or remove that kernel ceiling to obtain a
successful script result.

After all probes, no containers with label `milo.office-executor=1` remained.

The returned DOCX/PPTX PNG previews were opened and visually inspected: Chinese
titles and body text rendered without missing glyphs or clipping. The XLSX
preview showed 10, 20 and the recalculated sum 30. The original XLSX cached
formula remained 30 after preview generation. Previews use temporary copies;
the renderer does not rewrite the original workbook.

Evidence stays outside Git at `logs/office-evidence/` on the test host. Its
`manifest.json` records file hashes and sizes. The corresponding operator copy
was reviewed at `/tmp/milo-office-acceptance-evidence/`.

## Defects found by real acceptance

- A single unbuffered text write truncated a large result JSON to 64 KiB.
  The fixed runner now loops over bounded raw writes, handling short and
  interrupted writes. A semantic unit test reconstructs a large Unicode result
  under repeated short writes and verifies exact JSON equivalence.
- The clean task environment omitted the fixed helper's Python module path.
  It now sets only the image-owned `/opt/office-executor`; it does not inherit
  any application or host environment.

Executor unit/HTTP tests: 30 passed. Go resource integration and full application
tests are tracked separately by the main implementation task.

This is executor acceptance, not production deployment or final application
release approval. A private MinIO image cached on this isolated host is used
only as a test fixture; production continues to use its configured S3/R2 store.

## Final same-name preview regression

After the Office skill and publisher preview mapping fix, the final task and
supervisor images repeated the real `office_skills` probe under gVisor. The
DOCX, XLSX and PPTX deliberately shared the filename stem `acceptance`; their
fixed renderers produced three distinct verified PDF paths:

- `scratch/preview/acceptance.docx/acceptance.pdf`
- `scratch/preview/acceptance.xlsx/acceptance.pdf`
- `scratch/preview/acceptance.pptx/acceptance.pdf`

The probe completed in 28.754 seconds with 9 workspace files and 9 exported
evidence files. The XLSX cached SUM remained 30 after preview. No new pressure
probes were necessary because this change affects filename resolution, skill
guidance and preview association; the existing isolation and resource ceilings
remain covered by the previous genuine runtime acceptance. Evidence is in
`logs/office-evidence-final/` on the independent test host.
