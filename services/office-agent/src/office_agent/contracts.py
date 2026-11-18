"""Version-one control-plane contracts. Credentials never enter graph state."""
from __future__ import annotations

from datetime import datetime
from typing import Literal
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field, SecretStr, field_validator, model_validator


class Limits(BaseModel):
    model_config = ConfigDict(extra="forbid")
    max_model_calls: int = Field(ge=1, le=32)
    max_tool_calls: int = Field(ge=0, le=64)
    max_output_tokens: int = Field(ge=1, le=32768)
    max_run_seconds: int = Field(ge=1, le=1800)


class Message(BaseModel):
    model_config = ConfigDict(extra="forbid")
    role: Literal["user", "assistant"]
    # Go permits an assistant answer up to 96 KiB. The manifest-level UTF-8
    # budget remains authoritative (characters are not bytes).
    content: str = Field(min_length=1, max_length=98304)


class ResourceRef(BaseModel):
    model_config = ConfigDict(extra="forbid")
    resource_id: str
    version_id: str


class RunManifest(BaseModel):
    model_config = ConfigDict(extra="forbid")
    run_id: UUID
    conversation_id: UUID
    thread_id: str = Field(min_length=1, max_length=128)
    lease_epoch: int = Field(ge=1)
    lease_token: SecretStr = Field(exclude=True, repr=False)
    lease_until: datetime
    model: str = Field(min_length=1, max_length=128)
    bridge_path: str
    limits: Limits
    messages: list[Message] = Field(min_length=1, max_length=100)
    durable_resume: bool = False
    input_resources: list[ResourceRef] = Field(default_factory=list, max_length=16)

    @field_validator("input_resources", mode="before")
    @classmethod
    def nullable_resources(cls, value):
        return [] if value is None else value

    @model_validator(mode="after")
    def constrain_manifest(self):
        expected = f"/internal/office-agent/v1/runs/{self.run_id}/model/chat/completions"
        if self.bridge_path != expected:
            raise ValueError("bridge_path is not this run's fixed model endpoint")
        if sum(len(m.content.encode()) for m in self.messages) > 96 * 1024:
            raise ValueError("conversation exceeds the pilot input byte budget")
        return self

    @field_validator("model", "thread_id")
    @classmethod
    def no_controls(cls, value: str) -> str:
        if any(ord(c) < 32 or ord(c) == 127 for c in value):
            raise ValueError("control characters are not allowed")
        return value


class RunResult(BaseModel):
    status: Literal["completed", "failed", "needs_confirmation"]
    content: str = ""
    error_code: str = ""
    error_message: str = ""


class RuntimeFailure(Exception):
    """A safe error; never include HTTP bodies, credentials or script content."""
    def __init__(self, code: str, message: str, *, uncertain: bool = False):
        self.code = code
        self.message = message
        self.uncertain = uncertain
        super().__init__(message)

    def result(self) -> RunResult:
        return RunResult(status="needs_confirmation" if self.uncertain else "failed",
                         error_code=self.code, error_message=self.message)
