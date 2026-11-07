import contextlib
import io
import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import check_pnpm_audit_exceptions as checker


class AuditReportValidationTest(unittest.TestCase):
    def run_report(self, payload):
        with tempfile.TemporaryDirectory() as directory:
            audit = Path(directory) / "audit.json"
            exceptions = Path(directory) / "exceptions.yml"
            audit.write_text(json.dumps(payload), encoding="utf-8")
            exceptions.write_text("version: 1\nexceptions:\n", encoding="utf-8")
            output, errors = io.StringIO(), io.StringIO()
            with patch.object(sys, "argv", [
                "check_pnpm_audit_exceptions.py",
                "--audit", str(audit),
                "--exceptions", str(exceptions),
            ]), contextlib.redirect_stdout(output), contextlib.redirect_stderr(errors):
                result = checker.main()
            return result, output.getvalue(), errors.getvalue()

    def test_accepts_empty_supported_reports(self):
        for payload in ({"advisories": {}}, {"vulnerabilities": {}}):
            with self.subTest(payload=payload):
                code, output, errors = self.run_report(payload)
                self.assertEqual(code, 0)
                self.assertIn("Audit exceptions validated.", output)
                self.assertEqual(errors, "")

    def test_rejects_registry_error_even_with_empty_advisories(self):
        code, output, errors = self.run_report({
            "error": {"code": "ERR_PNPM_AUDIT_BAD_RESPONSE", "message": "Registry unavailable"},
            "advisories": {},
        })
        self.assertEqual(code, 1)
        self.assertNotIn("validated", output)
        self.assertIn("Audit did not return", errors)

    def test_rejects_missing_or_malformed_reports(self):
        for payload in ({}, [], {"metadata": {}}, {"advisories": []}):
            with self.subTest(payload=payload):
                code, output, errors = self.run_report(payload)
                self.assertEqual(code, 1)
                self.assertNotIn("validated", output)
                self.assertIn("Audit did not return", errors)

    def test_still_rejects_an_unapproved_high_vulnerability(self):
        code, output, errors = self.run_report({
            "advisories": {
                "example": {
                    "module_name": "example",
                    "severity": "high",
                    "github_advisory_id": "GHSA-test-test-test",
                    "title": "Example vulnerability",
                }
            }
        })
        self.assertEqual(code, 1)
        self.assertIn("missing exceptions", errors)
        self.assertNotIn("validated", output)


if __name__ == "__main__":
    unittest.main()
