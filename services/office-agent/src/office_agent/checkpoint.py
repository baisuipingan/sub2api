"""Lease-fenced LangGraph saver persisted through Go into PostgreSQL.

No DB password, pickle, import-by-name deserialization, file bytes, or platform
credential is stored. Only a small allowlisted JSON representation crosses Go.
"""
from __future__ import annotations

import asyncio
from dataclasses import fields
from datetime import datetime
import json
from uuid import UUID

from langchain_core.messages import BaseMessage, messages_from_dict, messages_to_dict
from langgraph.checkpoint.base import BaseCheckpointSaver, CheckpointTuple, WRITES_IDX_MAP
from langgraph.types import Command, Interrupt, Send

from .contracts import RuntimeFailure
from .control import operation_id

MAX_CHECKPOINT_BYTES = 1024 * 1024
_TAG = "__office_type__"


def encode(value):
    if value is None or type(value) in (bool, int, float, str):
        return value
    if isinstance(value, BaseMessage):
        return {_TAG: "message", "value": encode(messages_to_dict([value])[0])}
    if isinstance(value, Send):
        return {_TAG: "send", "node": value.node, "arg": encode(value.arg)}
    if isinstance(value, Interrupt):
        return {_TAG: "interrupt", "value": encode(value.value), "id": value.id}
    if isinstance(value, Command):
        return {_TAG: "command", "value": {field.name: encode(getattr(value, field.name)) for field in fields(value)}}
    if isinstance(value, datetime):
        return {_TAG: "datetime", "value": value.isoformat()}
    if isinstance(value, UUID):
        return {_TAG: "uuid", "value": str(value)}
    if isinstance(value, (tuple, set, frozenset)):
        return {_TAG: type(value).__name__, "value": [encode(item) for item in value]}
    if isinstance(value, list):
        return [encode(item) for item in value]
    if isinstance(value, dict) and all(isinstance(key, str) for key in value):
        # Escape maps so user content cannot masquerade as an encoded class.
        return {_TAG: "map", "value": {key: encode(item) for key, item in value.items()}}
    raise RuntimeFailure("UNSAFE_CHECKPOINT_TYPE", "运行状态包含不能安全持久化的数据；已停止恢复写入")


def decode(value):
    if value is None or type(value) in (bool, int, float, str):
        return value
    if isinstance(value, list):
        return [decode(item) for item in value]
    if not isinstance(value, dict):
        raise ValueError("invalid checkpoint value")
    kind = value.get(_TAG)
    if kind == "map":
        return {key: decode(item) for key, item in value["value"].items()}
    if kind == "message":
        return messages_from_dict([decode(value["value"])])[0]
    if kind == "send":
        return Send(value["node"], decode(value["arg"]))
    if kind == "interrupt":
        return Interrupt(value=decode(value["value"]), id=value["id"])
    if kind == "command":
        allowed = {field.name for field in fields(Command)}
        if set(value["value"]) - allowed:
            raise ValueError("unknown Command field")
        return Command(**{key: decode(item) for key, item in value["value"].items()})
    if kind in {"tuple", "set", "frozenset"}:
        return {"tuple": tuple, "set": set, "frozenset": frozenset}[kind](decode(item) for item in value["value"])
    if kind == "datetime":
        return datetime.fromisoformat(value["value"])
    if kind == "uuid":
        return UUID(value["value"])
    raise ValueError("unknown checkpoint type")


class GoCheckpointSaver(BaseCheckpointSaver):
    """Latest checkpoints + pending writes; each write CAS/fenced in Go's DB.

    Supports resuming the current run, not arbitrary historical time travel.
    No model call may be repeated merely because a checkpoint write is unknown.
    """
    def __init__(self, control, *, before_save=None):
        super().__init__()
        self.control, self.version = control, control.manifest.checkpoint_version
        self.records, self.loaded, self.lock = [], False, asyncio.Lock()
        self.before_save, self.workspace_version = before_save, 0
        self.tool_operations = []

    async def _load(self):
        if self.loaded:
            return
        response = await self.control.get(self.control.run_path("checkpoint"), maximum=MAX_CHECKPOINT_BYTES + 4096)
        if not isinstance(response, dict) or type(response.get("version")) is not int:
            raise RuntimeFailure("INVALID_CHECKPOINT", "持久运行状态无效，不能继续执行", uncertain=True)
        document = response.get("data")
        if document is not None:
            if not isinstance(document, dict) or document.get("format") != "office-langgraph-json-v1" or not isinstance(document.get("records"), list) or len(document["records"]) > 4:
                raise RuntimeFailure("INVALID_CHECKPOINT", "持久运行状态版本不兼容，已停止", uncertain=True)
            self.records = document["records"]
            self.workspace_version = document.get("workspace_version", 0)
            self.tool_operations = document.get("tool_operations", [])
            if (type(self.workspace_version) is not int or self.workspace_version < 0
                    or not isinstance(self.tool_operations, list) or len(self.tool_operations) > 128
                    or any(not isinstance(item, str) or len(item) > 128 for item in self.tool_operations)):
                raise RuntimeFailure("INVALID_CHECKPOINT", "持久运行状态预算无效，已停止", uncertain=True)
        self.version = response["version"]
        self.loaded = True

    async def _save(self):
        if self.before_save:
            self.workspace_version = await self.before_save()
        document = {"format": "office-langgraph-json-v1", "records": self.records,
                    "workspace_version": self.workspace_version, "tool_operations": self.tool_operations}
        if len(json.dumps(document, ensure_ascii=False, allow_nan=False, separators=(",", ":")).encode()) > MAX_CHECKPOINT_BYTES:
            raise RuntimeFailure("CHECKPOINT_LIMIT", "运行状态超过恢复预算，已停止")
        write_id = operation_id(str(self.control.manifest.run_id), "checkpoint", str(self.version), document)
        result = await self.control.post(self.control.run_path("checkpoint"),
                                         {"expected_version": self.version, "write_id": write_id, "data": document},
                                         uncertain_on_transport=True)
        if not isinstance(result, dict) or type(result.get("version")) is not int or result["version"] <= self.version:
            raise RuntimeFailure("CHECKPOINT_WRITE_UNKNOWN", "运行状态写入未确认，已停止自动恢复", uncertain=True)
        self.version = result["version"]

    async def reserve_tool(self, identity, limit):
        """Persist a bounded distinct-tool budget before dispatch, across resumes."""
        async with self.lock:
            await self._load()
            if identity in self.tool_operations:
                return
            if len(self.tool_operations) >= limit:
                raise RuntimeFailure("TOOL_BUDGET_EXCEEDED", "本次任务已达到工具调用次数上限")
            self.tool_operations.append(identity)
            await self._save()

    def _config(self, record):
        return {"configurable": {"thread_id": self.control.manifest.thread_id,
                                 "checkpoint_ns": record["namespace"], "checkpoint_id": record["id"]}}

    def _tuple(self, record):
        parent = {"configurable": {**self._config(record)["configurable"], "checkpoint_id": record["parent"]}} if record["parent"] else None
        return CheckpointTuple(config=self._config(record), checkpoint=decode(record["checkpoint"]),
                               metadata=decode(record["metadata"]), parent_config=parent,
                               pending_writes=[(row["task_id"], row["channel"], decode(row["value"])) for row in record["writes"]])

    async def aget_tuple(self, config):
        async with self.lock:
            await self._load()
            selection = config.get("configurable", {})
            if selection.get("thread_id") != self.control.manifest.thread_id:
                raise RuntimeFailure("CHECKPOINT_THREAD_MISMATCH", "恢复会话不匹配")
            matches = [record for record in self.records if record["namespace"] == selection.get("checkpoint_ns", "")
                       and (not selection.get("checkpoint_id") or record["id"] == selection["checkpoint_id"])]
            return self._tuple(max(matches, key=lambda record: record["id"])) if matches else None

    async def alist(self, config, *, filter=None, before=None, limit=None):
        async with self.lock:
            await self._load()
            rows = list(reversed(self.records))
        for record in rows:
            if config and record["namespace"] != config.get("configurable", {}).get("checkpoint_ns", ""):
                continue
            if before and record["id"] >= before["configurable"].get("checkpoint_id", ""):
                continue
            if filter and any(decode(record["metadata"]).get(key) != value for key, value in filter.items()):
                continue
            if limit is not None and limit <= 0:
                break
            yield self._tuple(record)
            if limit is not None:
                limit -= 1

    async def aput(self, config, checkpoint, metadata, new_versions):
        async with self.lock:
            await self._load()
            selection = config["configurable"]
            record = {"id": checkpoint["id"], "namespace": selection.get("checkpoint_ns", ""),
                      "parent": selection.get("checkpoint_id"), "checkpoint": encode(checkpoint),
                      "metadata": encode(metadata), "writes": []}
            self.records = [row for row in self.records if (row["namespace"], row["id"]) != (record["namespace"], record["id"])] + [record]
            self.records = sorted(self.records, key=lambda row: row["id"])[-4:]
            await self._save()
            return self._config(record)

    async def aput_writes(self, config, writes, task_id, task_path=""):
        async with self.lock:
            await self._load()
            selection = config["configurable"]
            record = next((row for row in self.records if row["namespace"] == selection.get("checkpoint_ns", "") and row["id"] == selection.get("checkpoint_id")), None)
            if record is None:
                raise RuntimeFailure("CHECKPOINT_NOT_FOUND", "运行步骤的恢复记录缺失，已停止", uncertain=True)
            for index, (channel, value) in enumerate(writes):
                write_index = WRITES_IDX_MAP.get(channel, index)
                existing = next((row for row in record["writes"] if row["task_id"] == task_id and row["index"] == write_index), None)
                row = {"task_id": task_id, "index": write_index, "channel": channel, "value": encode(value)}
                if existing is not None:
                    if write_index >= 0:
                        continue
                    record["writes"].remove(existing)
                record["writes"].append(row)
            if len(record["writes"]) > 256:
                raise RuntimeFailure("CHECKPOINT_LIMIT", "步骤恢复记录超过预算")
            await self._save()

    async def adelete_thread(self, thread_id):
        raise RuntimeFailure("CHECKPOINT_DELETE_FORBIDDEN", "恢复记录仅允许由用户会话管理接口删除")
