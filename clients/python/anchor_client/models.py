from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Optional

EFFECT_RUN = "run"
EFFECT_ALREADY_DONE = "already_done"
EFFECT_AMBIGUOUS = "ambiguous"


@dataclass(frozen=True)
class ClaimedTask:
    task_id: str
    agent_pool: str
    payload: Any
    claimed_by: str


@dataclass(frozen=True)
class APIKeyInfo:
    key_id: str
    created_at: str
    revoked_at: Optional[str]


@dataclass(frozen=True)
class EffectReservation:
    idempotency_key: str
    decision: str
    result: Any = None


@dataclass(frozen=True)
class RecalledMemory:
    memory_id: str
    content: str
    freshness_verified: bool
    live_value: Any = None
