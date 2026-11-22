"""Polling may recover before claim, but must never replay a claimed task."""
import json

import httpx
import pytest

import office_agent.worker as worker
from office_agent.contracts import RunResult, RuntimeFailure
from test_runtime import manifest


class PollingStopped(Exception):
    """Stop the real polling loop at a controlled idle boundary."""


def configure(monkeypatch, respond):
    monkeypatch.setenv("OFFICE_CONTROL_ORIGIN", "https://private-control.test")
    monkeypatch.setenv("OFFICE_WORKER_TOKEN", "private-bootstrap-token")
    monkeypatch.setenv("OFFICE_WORKER_ID", "test-poll-worker")
    monkeypatch.setenv("OFFICE_WORKER_PRESET", "chat")
    monkeypatch.setenv("OFFICE_EXECUTOR_PILOT_ENABLED", "false")
    real_client = httpx.AsyncClient
    transport = httpx.MockTransport(respond)
    monkeypatch.setattr(worker.httpx, "AsyncClient", lambda **kwargs: real_client(transport=transport, **kwargs))
    handled, sleeps = [], []

    async def process(origin, item, client, **kwargs):
        handled.append(str(item.run_id))
        return RunResult(status="completed", content="test completed")

    async def controlled_sleep(seconds):
        sleeps.append(seconds)
        if handled:
            raise PollingStopped

    monkeypatch.setattr(worker, "process_manifest", process)
    monkeypatch.setattr(worker.asyncio, "sleep", controlled_sleep)
    return handled, sleeps


def claim_response(item):
    data = item.model_dump(mode="json")
    # The secret is deliberately excluded from model_dump by the contract.
    data["lease_token"] = "private-lease-token"
    return httpx.Response(200, json={"code": 0, "data": data})


@pytest.mark.asyncio
async def test_disconnected_claim_recovers_without_worker_restart(monkeypatch, capsys):
    item, claims = manifest(mode="chat"), []

    async def respond(request):
        claims.append(request)
        if len(claims) == 1:
            raise httpx.ConnectError("private-control.test private-bootstrap-token", request=request)
        return claim_response(item) if len(claims) == 2 else httpx.Response(204)

    handled, sleeps = configure(monkeypatch, respond)
    with pytest.raises(PollingStopped):
        await worker.run_worker()
    assert handled == [str(item.run_id)]
    assert len(claims) == 3 and sleeps == [5, 5]
    assert all(request.url.path.endswith("/claim") for request in claims)
    assert json.loads(capsys.readouterr().err) == {"error_code": "CLAIM_TRANSPORT_FAILED", "retry_seconds": 5}


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [429, 500, 502, 503, 504])
async def test_temporary_claim_status_recovers(monkeypatch, capsys, status):
    item, claims = manifest(mode="chat"), []

    async def respond(request):
        claims.append(request)
        if len(claims) == 1:
            return httpx.Response(status, text="private server details", headers={"Retry-After": "100000"})
        return claim_response(item) if len(claims) == 2 else httpx.Response(204)

    handled, sleeps = configure(monkeypatch, respond)
    with pytest.raises(PollingStopped):
        await worker.run_worker()
    assert handled == [str(item.run_id)] and len(claims) == 3
    assert sleeps == [5, 5]
    assert json.loads(capsys.readouterr().err) == {"error_code": "CLAIM_TEMPORARILY_UNAVAILABLE", "retry_seconds": 5}


@pytest.mark.asyncio
@pytest.mark.parametrize("status", [301, 400, 401, 403, 404, 409, 422, 501])
async def test_permanent_claim_rejection_fails_closed(monkeypatch, capsys, status):
    claims = []

    async def respond(request):
        claims.append(request)
        return httpx.Response(status, text="private auth/contract failure")

    handled, sleeps = configure(monkeypatch, respond)
    with pytest.raises(RuntimeFailure, match="服务端拒绝领取任务") as failure:
        await worker.run_worker()
    assert failure.value.code == "CLAIM_REJECTED"
    assert len(claims) == 1 and not handled and not sleeps
    assert not capsys.readouterr().err


@pytest.mark.asyncio
@pytest.mark.parametrize("transport_failure", [True, False])
async def test_once_claim_failure_raises_immediately(monkeypatch, capsys, transport_failure):
    claims = []

    async def respond(request):
        claims.append(request)
        if transport_failure:
            raise httpx.ReadTimeout("private failure", request=request)
        return httpx.Response(503)

    handled, sleeps = configure(monkeypatch, respond)
    with pytest.raises(RuntimeFailure) as failure:
        await worker.run_worker(once=True)
    assert failure.value.code == ("CLAIM_TRANSPORT_FAILED" if transport_failure else "CLAIM_TEMPORARILY_UNAVAILABLE")
    assert len(claims) == 1 and not handled and not sleeps
    assert not capsys.readouterr().err


@pytest.mark.asyncio
async def test_claim_backoff_is_capped_and_resets_after_successful_poll(monkeypatch, capsys):
    claims = []
    # A successful idle poll resets backoff just as a received manifest does.
    responses = [503] * 6 + [204, 502, 204]

    async def respond(request):
        claims.append(request)
        if len(claims) > len(responses):
            raise PollingStopped
        return httpx.Response(responses[len(claims) - 1])

    handled, sleeps = configure(monkeypatch, respond)
    with pytest.raises(PollingStopped):
        await worker.run_worker()
    assert not handled
    assert sleeps == [5, 10, 20, 30, 30, 30, 5, 5, 5]
    logs = [json.loads(line) for line in capsys.readouterr().err.splitlines()]
    assert [line["retry_seconds"] for line in logs] == [5, 10, 20, 30, 30, 30, 5]
    assert all(set(line) == {"error_code", "retry_seconds"} for line in logs)


@pytest.mark.asyncio
async def test_claim_backoff_resets_after_a_manifest_is_processed(monkeypatch, capsys):
    item, claims = manifest(mode="chat"), []

    async def respond(request):
        claims.append(request)
        return claim_response(item) if len(claims) == 3 else httpx.Response(503)

    handled, sleeps = configure(monkeypatch, respond)
    with pytest.raises(PollingStopped):
        await worker.run_worker()
    assert handled == [str(item.run_id)] and len(claims) == 4
    assert sleeps == [5, 10, 5]
    logs = [json.loads(line) for line in capsys.readouterr().err.splitlines()]
    assert [line["retry_seconds"] for line in logs] == [5, 10, 5]


@pytest.mark.asyncio
async def test_invalid_manifest_is_not_retried(monkeypatch, capsys):
    claims = []

    async def respond(request):
        claims.append(request)
        return httpx.Response(200, json={"code": 0, "data": {"model": "invalid"}})

    handled, sleeps = configure(monkeypatch, respond)
    with pytest.raises(RuntimeFailure) as failure:
        await worker.run_worker()
    assert failure.value.code == "INVALID_MANIFEST"
    assert len(claims) == 1 and not handled and not sleeps
    assert not capsys.readouterr().err


@pytest.mark.asyncio
async def test_lost_claim_response_body_is_safe_to_repoll(monkeypatch, capsys):
    item, claims = manifest(mode="chat"), []

    class BrokenClaimBody(httpx.AsyncByteStream):
        async def __aiter__(self):
            yield b'{"code":0,"data":'
            raise httpx.ReadError("private interrupted response")

    async def respond(request):
        claims.append(request)
        if len(claims) == 1:
            return httpx.Response(200, stream=BrokenClaimBody())
        return claim_response(item) if len(claims) == 2 else httpx.Response(204)

    handled, sleeps = configure(monkeypatch, respond)
    with pytest.raises(PollingStopped):
        await worker.run_worker()
    assert handled == [str(item.run_id)] and sleeps == [5, 5]
    assert json.loads(capsys.readouterr().err) == {"error_code": "CLAIM_TRANSPORT_FAILED", "retry_seconds": 5}


@pytest.mark.asyncio
async def test_claimed_task_failure_is_never_caught_by_polling_retry(monkeypatch, capsys):
    item, claims, executions = manifest(mode="chat"), [], []

    async def respond(request):
        claims.append(request)
        return claim_response(item)

    _, sleeps = configure(monkeypatch, respond)

    async def fail_after_claim(*args, **kwargs):
        executions.append(True)
        # Even a matching error code from execution must not reach claim retry.
        raise RuntimeFailure("CLAIM_TRANSPORT_FAILED", "task failed after acquisition")

    monkeypatch.setattr(worker, "process_manifest", fail_after_claim)
    with pytest.raises(RuntimeFailure, match="task failed after acquisition"):
        await worker.run_worker()
    assert len(claims) == len(executions) == 1 and not sleeps
    assert not capsys.readouterr().err


@pytest.mark.asyncio
async def test_needs_confirmation_result_is_not_replayed_by_polling(monkeypatch):
    item, claims, executions = manifest(mode="chat"), [], []

    async def respond(request):
        claims.append(request)
        return claim_response(item) if len(claims) == 1 else httpx.Response(204)

    _, sleeps = configure(monkeypatch, respond)

    async def uncertain_after_claim(*args, **kwargs):
        executions.append(True)
        return RunResult(status="needs_confirmation", error_code="TRANSPORT_UNKNOWN")

    async def controlled_sleep(seconds):
        sleeps.append(seconds)
        raise PollingStopped

    monkeypatch.setattr(worker, "process_manifest", uncertain_after_claim)
    monkeypatch.setattr(worker.asyncio, "sleep", controlled_sleep)
    with pytest.raises(PollingStopped):
        await worker.run_worker()
    assert len(claims) == 2 and len(executions) == 1 and sleeps == [5]
