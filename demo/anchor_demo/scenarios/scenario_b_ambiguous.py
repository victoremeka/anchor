"""The Engine crashes after the tool call returns but before it records
the result.

Expected outcome: the effect stays pending forever, it is never retried
automatically, and once the task's lease expires the reclaim sweep flags
the task instead of silently reclaiming or completing it.
"""

import time

from anchor_client import AnchorClient, create_organization

from . import _db
from ._engine import ManagedEngine

ADDR = "127.0.0.1:8092"


def main() -> None:
    engine = ManagedEngine(ADDR, lease_ttl_seconds=3)
    engine.start()

    org_id, api_key = create_organization(engine.base_url, "scenario-b")
    client = AnchorClient(engine.base_url, api_key)
    worker = client.create_agent("send-worker-1", "send")
    task_id = client.create_task("send", {"to": "jane@example.com"})
    claimed = client.claim_task("send", worker)
    print(f"claimed task {task_id}")

    reservation = client.reserve_effect(task_id, "send_email", call_key="jane@example.com")
    print(f"reserved effect {reservation.idempotency_key}, decision={reservation.decision}")

    print("the tool call happens here, outside any transaction, and it succeeds")
    fake_result = {"message_id": "would-have-sent-this"}

    print("now the Engine crashes before the result gets recorded")
    engine.kill()

    print("waiting past the lease so the reclaim sweep has a chance to run")
    time.sleep(6)

    engine.start()

    rows = _db.query(f"SELECT status FROM tasks WHERE task_id = '{task_id}'")
    task_status = rows[0]["status"] if rows else "missing"
    effect_rows = _db.query(
        f"SELECT status FROM executed_effects WHERE idempotency_key = '{reservation.idempotency_key}'"
    )
    effect_status = effect_rows[0]["status"] if effect_rows else "missing"

    print(f"task status after restart: {task_status}")
    print(f"effect status after restart: {effect_status}")

    if task_status == "flagged" and effect_status == "pending":
        print("PASS: the task was flagged for an operator instead of being silently reclaimed or completed")
    else:
        print("FAIL: expected task=flagged and effect=pending")

    engine.stop()


if __name__ == "__main__":
    main()
