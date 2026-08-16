"""HTTP client for the Anchor Engine API.

AnchorClient holds no database connection. Every method sends one HTTP
request to the Engine, authenticated with a bearer API key.
"""

from __future__ import annotations

from typing import Any, List, Optional, Tuple

import requests

from .exceptions import TaskNotAvailableError, from_response
from .models import APIKeyInfo, ClaimedTask, EffectReservation, RecalledMemory

EMBEDDING_DIM = 1536

DEFAULT_TIMEOUT = 10.0


def create_organization(
    base_url: str, name: str, *, timeout: float = DEFAULT_TIMEOUT
) -> Tuple[str, str]:
    """Create a new organization. Returns its id and first API key.

    This is the only call that needs no API key. Use the returned key
    to build an AnchorClient for every other call.
    """
    resp = requests.post(
        f"{base_url.rstrip('/')}/organizations", json={"name": name}, timeout=timeout
    )
    data = _unwrap(resp)
    return data["org_id"], data["api_key"]


class AnchorClient:
    def __init__(
        self,
        base_url: str,
        api_key: str,
        *,
        timeout: float = DEFAULT_TIMEOUT,
        session: Optional[requests.Session] = None,
    ):
        self._base_url = base_url.rstrip("/")
        self._api_key = api_key
        self._timeout = timeout
        self._session = session or requests.Session()

    def create_agent(self, name: str, pool: str) -> str:
        data = self._post("/agents", {"name": name, "pool": pool})
        return data["agent_id"]

    def create_api_key(self) -> Tuple[str, str]:
        """Create a new key for this client's organization.

        The current key keeps working. Call revoke_api_key on the old key
        once callers have moved to the new one.
        """
        data = self._post("/api-keys", {})
        return data["key_id"], data["api_key"]

    def list_api_keys(self) -> List[APIKeyInfo]:
        data = self._get("/api-keys")
        return [
            APIKeyInfo(key_id=k["key_id"], created_at=k["created_at"], revoked_at=k.get("revoked_at"))
            for k in data["keys"]
        ]

    def revoke_api_key(self, key_id: str) -> None:
        """Revoke a key. Revoking an already revoked key succeeds.

        Revoking the organization's last active key raises LastActiveKeyError.
        """
        self._delete(f"/api-keys/{key_id}")

    def create_task(
        self, agent_pool: str, payload: Optional[dict] = None, *, dedup_key: Optional[str] = None
    ) -> str:
        """Create a task. Pass dedup_key to make creation safe under retry.

        Two calls with the same dedup_key return the same task id.
        """
        body: dict = {"agent_pool": agent_pool, "payload": payload or {}}
        if dedup_key is not None:
            body["dedup_key"] = dedup_key
        data = self._post("/tasks", body)
        return data["task_id"]

    def claim_task(self, agent_pool: str, worker_id: str) -> Optional[ClaimedTask]:
        """Claim the oldest pending task in agent_pool.

        Returns None if no task is available right now.
        """
        try:
            data = self._post("/tasks/claim", {"agent_pool": agent_pool, "worker_id": worker_id})
        except TaskNotAvailableError:
            return None
        return ClaimedTask(
            task_id=data["task_id"],
            agent_pool=data["agent_pool"],
            payload=data["payload"],
            claimed_by=data["claimed_by"],
        )

    def complete_task(self, task_id: str, claimed_by: str, result: Optional[dict] = None) -> bool:
        """Mark a task done. This is the only call that does that.

        Returns False if claimed_by no longer owns the task. That happens
        when the lease expired and the task was reclaimed or flagged.
        """
        data = self._post(f"/tasks/{task_id}/complete", {"claimed_by": claimed_by, "result": result or {}})
        return data["ok"]

    def reserve_effect(self, task_id: str, tool_name: str, *, call_key: str = "") -> EffectReservation:
        """Reserve an idempotency slot before calling an external tool.

        Call the tool only when decision is EFFECT_RUN. On EFFECT_ALREADY_DONE,
        result holds the prior outcome and the tool must not be called again.
        On EFFECT_AMBIGUOUS, do not call the tool and do not assume anything
        about whether it already ran.
        """
        body: dict = {"tool_name": tool_name}
        if call_key:
            body["call_key"] = call_key
        data = self._post(f"/tasks/{task_id}/effects/reserve", body)
        return EffectReservation(
            idempotency_key=data["idempotency_key"], decision=data["decision"], result=data.get("result")
        )

    def complete_effect(self, idempotency_key: str, result: Optional[dict] = None) -> None:
        """Record the result of a tool call reserved with reserve_effect."""
        self._post(f"/effects/{idempotency_key}/complete", {"result": result or {}})

    def remember(
        self,
        agent_id: str,
        subject_id: str,
        content: str,
        embedding: List[float],
        *,
        linked_table: Optional[str] = None,
        linked_id: Optional[str] = None,
    ) -> str:
        _check_embedding(embedding)
        body: dict = {
            "agent_id": agent_id,
            "subject_id": subject_id,
            "content": content,
            "embedding": embedding,
        }
        if linked_table is not None:
            body["linked_table"] = linked_table
        if linked_id is not None:
            body["linked_id"] = linked_id
        data = self._post("/memories", body)
        return data["memory_id"]

    def recall(
        self, agent_id: str, subject_id: str, query_embedding: List[float], *, top_k: int = 5
    ) -> List[RecalledMemory]:
        """Search memories by embedding similarity.

        Prefer live_value over content when live_value is present. content
        is not rewritten when the linked row changes.
        """
        _check_embedding(query_embedding)
        body = {
            "agent_id": agent_id,
            "subject_id": subject_id,
            "query_embedding": query_embedding,
            "top_k": top_k,
        }
        data = self._post("/memories/recall", body)
        return [
            RecalledMemory(
                memory_id=r["memory_id"],
                content=r["content"],
                freshness_verified=r["freshness_verified"],
                live_value=r.get("live_value"),
            )
            for r in data["results"]
        ]

    def forget(self, memory_id: str) -> None:
        self._delete(f"/memories/{memory_id}")

    def _headers(self) -> dict:
        return {"Authorization": f"Bearer {self._api_key}"}

    def _post(self, path: str, body: dict) -> Any:
        resp = self._session.post(
            f"{self._base_url}{path}", json=body, headers=self._headers(), timeout=self._timeout
        )
        return _unwrap(resp)

    def _get(self, path: str) -> Any:
        resp = self._session.get(f"{self._base_url}{path}", headers=self._headers(), timeout=self._timeout)
        return _unwrap(resp)

    def _delete(self, path: str) -> Any:
        resp = self._session.delete(f"{self._base_url}{path}", headers=self._headers(), timeout=self._timeout)
        return _unwrap(resp)


def _check_embedding(embedding: List[float]) -> None:
    if len(embedding) != EMBEDDING_DIM:
        raise ValueError(f"embedding must have {EMBEDDING_DIM} dimensions, got {len(embedding)}")


def _unwrap(resp: requests.Response) -> Any:
    if not resp.ok:
        try:
            message = resp.json().get("error", resp.text)
        except ValueError:
            message = resp.text
        raise from_response(resp.status_code, message)
    if not resp.content:
        return {}
    return resp.json()
