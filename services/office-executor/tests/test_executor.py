from __future__ import annotations

import copy
import hashlib
import io
import json
from pathlib import Path
import tempfile
import threading
from types import SimpleNamespace
import unittest
from unittest.mock import patch
import zipfile

from office_executor.__main__ import parse_json
from office_executor.contracts import ContractError, DEFAULT_LIMITS, MAX_RESULT_WIRE_BYTES, MIB, file_record, logical_path, maximum_result_json_estimate, public_output_metadata, validate_request
from office_executor.runner import check_public_type, collect_workspace, read_regular
from office_executor.supervisor import ExecutionError, Settings, Supervisor, container_command


def request(**changes):
    return {"version": 1, "run_id": "run_123", "operation_id": "op_456", "lease_epoch": 1,
            "script": "print('office task')", "inputs": [], **changes}


class ContractsTest(unittest.TestCase):
    def test_rejects_paths_that_escape_or_normalize(self):
        for path in ("/output/a.txt", "output/../a.txt", "output//a.txt", "output/./a.txt", "output/a.txt/",
                     "output\\a.txt", "other/a.txt", "output", "output/\x00x", "skills/a.py"):
            with self.subTest(path=path), self.assertRaises(ContractError):
                logical_path(path)

    def test_no_custom_runtime_image_mount_or_environment(self):
        for key in ("image", "runtime", "env", "mounts", "command", "lang", "shell"):
            with self.subTest(key=key), self.assertRaises(ContractError):
                validate_request(request(**{key: "untrusted"}))

    def test_valid_inputs_with_hashes_and_zones(self):
        data = request(inputs=[file_record("uploads/中文.xlsx", b"input"), file_record("scratch/work.py", b"a=1")])
        self.assertEqual(validate_request(data)["limits"], DEFAULT_LIMITS)
        data["inputs"][0]["sha256"] = "0" * 64
        with self.assertRaisesRegex(ContractError, "sha256"):
            validate_request(data)

    def test_duplicate_and_file_directory_conflicts_are_rejected(self):
        for files in (("uploads/a.txt", "uploads/a.txt"), ("scratch/a", "scratch/a/x")):
            with self.subTest(files=files), self.assertRaises(ContractError):
                validate_request(request(inputs=[file_record(path, b"x") for path in files]))

    def test_limits_only_downward_and_not_bool_or_nan(self):
        for limits in ({"memory_mb": 999999}, {"timeout_seconds": True}, {"cpus": float("nan")}, {"exec": 1}):
            with self.subTest(limits=limits), self.assertRaises(ContractError):
                validate_request(request(limits=limits))
        self.assertEqual(validate_request(request(limits={"timeout_seconds": 10}))["limits"]["timeout_seconds"], 10)

    def test_duplicate_json_fields_rejected_recursively(self):
        for raw in (b'{"version":1,"version":2}', b'{"limits":{"cpus":1,"cpus":2}}'):
            with self.assertRaises(ContractError):
                parse_json(raw)

    def test_invalid_unicode_and_epoch_do_not_reach_execution(self):
        with self.assertRaises(ContractError):
            logical_path("output/\ud800.txt")
        for changes in ({"script": "\ud800"}, {"lease_epoch": 2**100}):
            with self.assertRaises(ContractError):
                validate_request(request(**changes))

    def test_maximum_snapshot_fits_shared_wire_cap_without_large_allocation(self):
        self.assertEqual(MAX_RESULT_WIRE_BYTES, 40 * MIB)
        self.assertLess(maximum_result_json_estimate(DEFAULT_LIMITS), MAX_RESULT_WIRE_BYTES)
        # Default 24 MiB bytes are encoded once (32 MiB), not twice (64 MiB).
        self.assertGreater(maximum_result_json_estimate(DEFAULT_LIMITS), 32 * MIB)
        snapshot = [file_record("output/a.txt", b"hello"), file_record("scratch/helper.py", b"x=1")]
        output = public_output_metadata(snapshot)
        self.assertEqual(len(output), 1)
        self.assertEqual(set(output[0]), {"path", "size", "sha256", "mime"})
        wire = json.dumps({"workspace_manifest": snapshot, "outputs": output})
        self.assertEqual(wire.count('"content_base64"'), len(snapshot))


class ResultSafetyTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.root = Path(self.directory.name)
        (self.root / "output").mkdir()
        (self.root / "scratch").mkdir()

    def tearDown(self):
        self.directory.cleanup()

    def test_real_output_snapshot_and_runtime_cache_exclusion(self):
        (self.root / "output/文档.txt").write_text("hello")
        (self.root / "scratch/.home").mkdir()
        (self.root / "scratch/.home/cache").write_text("not a conversation resource")
        result = collect_workspace(self.root, DEFAULT_LIMITS)
        self.assertEqual([entry["path"] for entry in result], ["output/文档.txt"])

    def test_symlink_file_and_directory_rejected(self):
        (self.root / "output/link.txt").symlink_to("/etc/passwd")
        with self.assertRaises((OSError, ContractError)):
            collect_workspace(self.root, DEFAULT_LIMITS)
        (self.root / "output/link.txt").unlink()
        (self.root / "output/linkdir").symlink_to("/tmp", target_is_directory=True)
        with self.assertRaises(ContractError):
            collect_workspace(self.root, DEFAULT_LIMITS)

    def test_hardlinks_and_parent_symlinks_rejected(self):
        (self.root / "scratch/a.txt").write_text("a")
        (self.root / "output/a.txt").hardlink_to(self.root / "scratch/a.txt")
        with self.assertRaises(ContractError):
            collect_workspace(self.root, DEFAULT_LIMITS)
        (self.root / "output/a.txt").unlink()
        (self.root / "output/parent").symlink_to(self.root / "scratch", target_is_directory=True)
        with self.assertRaises(OSError):
            read_regular(self.root, "output/parent/a.txt", 10)

    def test_total_bytes_and_file_limits(self):
        (self.root / "output/a.txt").write_bytes(b"123456")
        with self.assertRaises(ContractError):
            collect_workspace(self.root, {**DEFAULT_LIMITS, "max_output_bytes": 5})
        (self.root / "output/b.txt").write_bytes(b"ok")
        with self.assertRaises(ContractError):
            collect_workspace(self.root, {**DEFAULT_LIMITS, "max_output_files": 1})

    def test_unreadable_directory_is_not_silently_omitted(self):
        def unreadable(*args, **kwargs):
            kwargs["onerror"](PermissionError("cannot enumerate output directory"))
            yield  # Make this behave like os.walk's lazy iterator.
        with patch("office_executor.runner.os.walk", unreadable):
            with self.assertRaises(PermissionError):
                collect_workspace(self.root, DEFAULT_LIMITS)

    def test_public_format_signatures_and_active_formats(self):
        for path, content in (("output/a.html", b"<script>"), ("output/a.png", b"wrong"),
                              ("output/a.docx", b"not zip"), ("output/a.json", b"{")):
            with self.subTest(path=path), self.assertRaises(ContractError):
                check_public_type(path, content)
        # Scratch is private restoration state, not a downloadable public artifact.
        check_public_type("scratch/helper.py", b"import os")

    def test_office_container_requires_shape_and_rejects_macros_and_traversal(self):
        def office(extra=None):
            output = io.BytesIO()
            with zipfile.ZipFile(output, "w") as archive:
                archive.writestr("[Content_Types].xml", "types")
                archive.writestr("word/document.xml", "doc")
                if extra:
                    archive.writestr(extra, "x")
            return output.getvalue()
        check_public_type("output/a.docx", office())
        for extra in ("word/vbaProject.bin", "../bad", "/bad"):
            with self.subTest(extra=extra), self.assertRaises(ContractError):
                check_public_type("output/a.docx", office(extra))


class FakeSupervisor(Supervisor):
    calls = 0
    def _execute_container(self, data, cancel):
        self.calls += 1
        return {**self._error_result(data, "completed", ""), "exit_code": 0}


class SupervisorTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.settings = Settings(image="sha256:" + "a" * 64, state_dir=Path(self.directory.name))
        self.supervisor = FakeSupervisor(self.settings)

    def tearDown(self):
        self.directory.cleanup()

    def test_container_security_flags_and_no_runtime_secrets_or_host_socket(self):
        args = container_command(self.settings, validate_request(request()), Path("/private/task"), "container")
        for required in ("--pull=never", "--runtime=runsc", "--network=none", "--read-only", "--cap-drop=ALL",
                         "--user=65532:65532", "--pids-limit=128", "--memory=2048m", "--memory-swap=2048m"):
            self.assertIn(required, args)
        self.assertIn("/workspace:rw,noexec,nosuid,nodev,size=256m,uid=65532,gid=65532,mode=0700", args)
        self.assertNotIn("--privileged", args)
        self.assertFalse(any("docker.sock" in arg for arg in args if arg.startswith("type=bind")))
        self.assertFalse(any(arg.startswith("--env") for arg in args))
        self.assertIn(str(self.settings.state_dir / ".docker-config"), args)
        self.assertIn("unix:///var/run/docker.sock", args)  # CLI only; not a mount into the task.

    def test_mutable_image_tag_rejected(self):
        with self.assertRaises(ValueError):
            Settings(image="office:latest", state_dir=self.settings.state_dir)

    def test_idempotent_result_return_does_not_rerun(self):
        first = self.supervisor.execute(request())
        second = self.supervisor.execute(request())
        self.assertEqual(first, second)
        self.assertEqual(self.supervisor.calls, 1)
        with self.assertRaisesRegex(ExecutionError, "different input"):
            self.supervisor.execute(request(script="print('different')"))

    def test_stale_lease_rejected(self):
        self.supervisor.execute(request(lease_epoch=2))
        with self.assertRaisesRegex(ExecutionError, "stale"):
            self.supervisor.execute(request(operation_id="newop", lease_epoch=1))

    def test_crash_marker_never_replays_and_expired_result_keeps_tombstone(self):
        data = validate_request(request())
        self.supervisor._claim(data)
        restarted = FakeSupervisor(self.settings)
        with self.assertRaises(ExecutionError) as error:
            restarted.execute(request())
        self.assertEqual(error.exception.code, "OPERATION_UNKNOWN")
        self.assertEqual(restarted.calls, 0)
        restarted.execute(request(operation_id="done"))
        with restarted._db() as db:
            db.execute("UPDATE operations SET updated=0 WHERE operation_id='done'")
        restarted.cleanup()
        with self.assertRaises(ExecutionError) as error:
            restarted.execute(request(operation_id="done"))
        self.assertEqual(error.exception.code, "OPERATION_RESULT_EXPIRED")

    def test_unknown_result_is_journaled_on_infrastructure_error(self):
        with patch.object(self.supervisor, "_execute_container", side_effect=RuntimeError("gone")):
            with self.assertRaises(RuntimeError):
                self.supervisor.execute(request())
        result = self.supervisor.execute(request())
        self.assertEqual(result["status"], "unknown")
        self.assertEqual(self.supervisor.calls, 0)

    def test_doctor_fails_closed_on_mac_or_missing_gvisor(self):
        with patch("office_executor.supervisor.os.uname") as uname:
            uname.return_value.sysname = "Darwin"
            with self.assertRaises(ExecutionError) as error:
                self.supervisor.doctor()
            self.assertEqual(error.exception.code, "UNSUPPORTED_HOST")
            uname.return_value.sysname = "Linux"
            with patch("office_executor.supervisor.subprocess.run") as call:
                call.return_value.stdout = b'{"runc": {}}'
                with self.assertRaises(ExecutionError) as error:
                    self.supervisor.doctor()
                self.assertEqual(error.exception.code, "GVISOR_UNAVAILABLE")

    def test_runner_result_is_revalidated(self):
        data = validate_request(request())
        result = {**self.supervisor._error_result(data, "completed", ""), "exit_code": 0}
        result["workspace_manifest"] = [file_record("output/a.txt", b"hi")]
        result["outputs"] = [file_record("output/wrong.txt", b"do not trust")]
        checked = self.supervisor._validate_result(data, result)
        self.assertEqual(checked["outputs"][0]["path"], "output/a.txt")
        self.assertNotIn("content_base64", checked["outputs"][0])
        result["workspace_manifest"][0]["mime"] = "text/html"
        self.assertEqual(self.supervisor._validate_result(data, result)["workspace_manifest"][0]["mime"], "text/plain")
        result["workspace_manifest"][0]["path"] = "../escape.txt"
        with self.assertRaises(ContractError):
            self.supervisor._validate_result(data, result)

    def test_cleanup_failure_blocks_subsequent_execution(self):
        self.supervisor._unhealthy.set()
        with self.assertRaises(ExecutionError) as error:
            self.supervisor.execute(request())
        self.assertEqual(error.exception.code, "CLEANUP_REQUIRED")
        self.assertEqual(self.supervisor.calls, 0)

    def test_storage_rejection_does_not_add_journal_rows_or_results(self):
        with patch.object(self.supervisor, "_check_capacity", side_effect=ExecutionError("EXECUTOR_STORAGE_FULL", "full")):
            for index in range(5):
                with self.assertRaises(ExecutionError) as error:
                    self.supervisor.execute(request(operation_id="new_" + str(index)))
                self.assertEqual(error.exception.code, "EXECUTOR_STORAGE_FULL")
        with self.supervisor._db() as db:
            self.assertEqual(db.execute("SELECT COUNT(*) FROM operations").fetchone()[0], 0)
            self.assertEqual(db.execute("SELECT COUNT(*) FROM runs").fetchone()[0], 0)
        self.assertEqual(list(self.settings.state_dir.glob("*.json")), [])
        self.assertEqual(self.supervisor.calls, 0)

    def test_container_crash_never_returns_a_replaceable_empty_snapshot(self):
        actual = Supervisor(self.settings)
        completed = SimpleNamespace(returncode=0, stdout=b"", stderr=b"")
        with patch.object(actual, "doctor"), patch("office_executor.supervisor.subprocess.run", return_value=completed), \
                patch("office_executor.supervisor._bounded_capture", return_value=(137, b"", b"", None)):
            result = actual.execute(request())
        self.assertEqual(result["status"], "unknown")
        self.assertEqual(result["error_code"], "TASK_CONTAINER_FAILED")


if __name__ == "__main__":
    unittest.main()
