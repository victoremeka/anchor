from __future__ import annotations

import json
from typing import Any, Optional
from unittest.mock import MagicMock

import pytest

from anchor_client import (
    AgentNotFoundError,
    AnchorClient,
    AuthenticationError,
    EffectReservation,
    LastActiveKeyError,
    TaskNotAvailableError,
    create_organization,
)
from anchor_client.client import EMBEDDING_DIM

BASE_URL = "http://localhost:8080"


class FakeResponse:
    def __init__(self, status_code: int, payload: Any):
        self.status_code = status_code
        self.ok = 200 <= status_code < 300
        self._payload = payload
        self.text = json.dumps(payload) if payload is not None else ""
        self.content = self.text.encode()

    def json(self):
        return self._payload


def make_client(responder) -> AnchorClient:
    session = MagicMock()
    session.post.side_effect = lambda url, json=None, headers=None, timeout=None: responder(
        "POST", url, json
    )
    session.get.side_effect = lambda url, headers=None, timeout=None: responder("GET", url, None)
    session.delete.side_effect = lambda url, headers=None, timeout=None: responder("DELETE", url, None)
    return AnchorClient(BASE_URL, "test-key", session=session)


def embedding() -> list:
    return [0.1] * EMBEDDING_DIM


def test_create_organization_needs_no_api_key():
    def responder(method, url, body):
        assert method == "POST"
        assert url == f"{BASE_URL}/organizations"
        assert body == {"name": "acme"}
        return FakeResponse(201, {"org_id": "org-1", "api_key": "sk-abc"})

    import requests

    real_post = requests.post
    requests.post = lambda url, json=None, timeout=None: responder("POST", url, json)
    try:
        org_id, api_key = create_organization(BASE_URL, "acme")
    finally:
        requests.post = real_post

    assert org_id == "org-1"
    assert api_key == "sk-abc"


def test_create_agent_posts_expected_body_and_auth_header():
    seen = {}

    def responder(method, url, body):
        seen["method"] = method
        seen["url"] = url
        seen["body"] = body
        return FakeResponse(201, {"agent_id": "agent-1"})

    client = make_client(responder)
    agent_id = client.create_agent("worker-1", "email-sender")

    assert agent_id == "agent-1"
    assert seen == {
        "method": "POST",
        "url": f"{BASE_URL}/agents",
        "body": {"name": "worker-1", "pool": "email-sender"},
    }


def test_claim_task_returns_none_on_empty_queue():
    def responder(method, url, body):
        return FakeResponse(404, {"error": "no pending task available"})

    client = make_client(responder)
    assert client.claim_task("email-sender", "worker-1") is None


def test_claim_task_raises_on_unknown_worker_not_empty_queue():
    def responder(method, url, body):
        return FakeResponse(404, {"error": "agent not found"})

    client = make_client(responder)
    with pytest.raises(AgentNotFoundError):
        client.claim_task("email-sender", "bad-worker")


def test_claim_task_returns_claimed_task_on_success():
    def responder(method, url, body):
        assert body == {"agent_pool": "email-sender", "worker_id": "worker-1"}
        return FakeResponse(
            200,
            {"task_id": "task-1", "agent_pool": "email-sender", "payload": {"to": "a@b.com"}, "claimed_by": "worker-1"},
        )

    client = make_client(responder)
    task = client.claim_task("email-sender", "worker-1")
    assert task.task_id == "task-1"
    assert task.payload == {"to": "a@b.com"}
    assert task.claimed_by == "worker-1"


def test_complete_task_returns_false_when_fenced_out():
    def responder(method, url, body):
        return FakeResponse(200, {"ok": False})

    client = make_client(responder)
    assert client.complete_task("task-1", "zombie-worker", {"sent": True}) is False


def test_reserve_effect_parses_decision_and_result():
    def responder(method, url, body):
        assert url == f"{BASE_URL}/tasks/task-1/effects/reserve"
        assert body == {"tool_name": "send_email", "call_key": "recipient-1"}
        return FakeResponse(200, {"idempotency_key": "idem-1", "decision": "already_done", "result": {"sent": True}})

    client = make_client(responder)
    reservation = client.reserve_effect("task-1", "send_email", call_key="recipient-1")
    assert reservation == EffectReservation(idempotency_key="idem-1", decision="already_done", result={"sent": True})


def test_remember_rejects_wrong_embedding_dimension():
    client = make_client(lambda *a: FakeResponse(201, {"memory_id": "m-1"}))
    with pytest.raises(ValueError):
        client.remember("agent-1", "subject-1", "note", [0.1, 0.2])


def test_recall_prefers_live_value_field_present():
    def responder(method, url, body):
        return FakeResponse(
            200,
            {
                "results": [
                    {"memory_id": "m-1", "content": "old", "freshness_verified": True, "live_value": {"status": "shipped"}},
                    {"memory_id": "m-2", "content": "still true", "freshness_verified": False},
                ]
            },
        )

    client = make_client(responder)
    results = client.recall("agent-1", "subject-1", embedding())
    assert results[0].freshness_verified is True
    assert results[0].live_value == {"status": "shipped"}
    assert results[1].live_value is None


def test_revoke_last_active_key_raises_conflict():
    def responder(method, url, body):
        return FakeResponse(409, {"error": "cannot revoke an organization's last active api key"})

    client = make_client(responder)
    with pytest.raises(LastActiveKeyError):
        client.revoke_api_key("key-1")


def test_unauthenticated_raises_authentication_error():
    def responder(method, url, body):
        return FakeResponse(401, {"error": "invalid api key"})

    client = make_client(responder)
    with pytest.raises(AuthenticationError):
        client.create_agent("x", "y")


def test_bearer_header_sent_on_every_authenticated_call():
    def responder(method, url, body):
        return FakeResponse(201, {"agent_id": "agent-1"})

    session = MagicMock()
    captured = {}

    def post(url, json=None, headers=None, timeout=None):
        captured["headers"] = headers
        return FakeResponse(201, {"agent_id": "agent-1"})

    session.post.side_effect = post
    client = AnchorClient(BASE_URL, "sk-secret", session=session)
    client.create_agent("x", "y")
    assert captured["headers"] == {"Authorization": "Bearer sk-secret"}
