"""Run only inside the independent remote acceptance directory, never production."""
from pathlib import Path
import json
import os
import secrets
import subprocess


def digest(tag):
    record = json.loads(subprocess.check_output(["docker", "image", "inspect", tag]))[0]
    return (record.get("RepoDigests") or [record["Id"]])[0]


def main():
    root = Path(__file__).resolve().parent.parent
    if str(root) != "/opt/milo-workbench-test":
        raise SystemExit("this initializer is restricted to the separate acceptance environment")
    folder = root / "secrets"
    folder.mkdir(mode=0o700, exist_ok=True)
    folder.chmod(0o700)
    destination = folder / "compose.env"
    values = {}
    if destination.exists():
        for line in destination.read_text().splitlines():
            if line and not line.startswith("#"):
                key, value = line.split("=", 1)
                values[key] = value
    for name in ("POSTGRES_PASSWORD", "REDIS_PASSWORD", "MINIO_ROOT_PASSWORD", "ADMIN_PASSWORD", "JWT_SECRET", "TOTP_ENCRYPTION_KEY", "OFFICE_TOOLS_TOKEN"):
        values.setdefault(name, secrets.token_hex(32))
    for name, tag in {
        "POSTGRES_IMAGE": "postgres:16-alpine",
        "REDIS_IMAGE": "redis:7-alpine",
        # This cached, offline test-only image is deliberately not a production
        # recommendation. Official community image distribution was removed.
        "MINIO_IMAGE": "minio/minio:RELEASE.2023-03-20T20-16-18Z",
        "GO_IMAGE": "golang:1.27.4-alpine",
        "PYTHON_BASE_IMAGE": "python:3.12-slim-bookworm",
    }.items():
        values[name] = digest(tag)
    values.setdefault("APP_IMAGE", "milo-workbench-test:latest")
    values.setdefault("OFFICE_TOOLS_IMAGE", "milo-office-tools-test:latest")
    temporary = destination.with_suffix(".tmp")
    temporary.write_text("".join(key + "=" + values[key] + "\n" for key in sorted(values)))
    temporary.chmod(0o600)
    temporary.replace(destination)
    print("test-only credentials preserved/generated and image digests recorded; no secrets printed")


if __name__ == "__main__":
    main()
