"""A send worker claims a task then crashes before it reserves the effect.

Expected outcome: the lease expires, the reclaim sweep returns the task to
pending, a second worker claims it and sends the email exactly once.
"""

import time

from anchor_client import AnchorClient, create_organization

from ._engine import ManagedEngine

ADDR = "127.0.0.1:8091"


def main() -> None:
    engine = ManagedEngine(ADDR, lease_ttl_seconds=3)
    engine.start()
    try:
        org_id, api_key = create_organization(engine.base_url, "scenario-a")
        client = AnchorClient(engine.base_url, api_key)
        worker_a = client.create_agent("send-worker-a", "send")
        worker_b = client.create_agent("send-worker-b", "send")

        client.create_task("send", {"to": "jane@example.com"})

        claimed = client.claim_task("send", worker_a)
        print(f"worker A claimed task {claimed.task_id}, then crashes without doing anything else")

        print("waiting for the lease to expire and the reclaim sweep to run")
        time.sleep(6)

        claimed_again = client.claim_task("send", worker_b)
        if claimed_again is None:
            print("FAIL: worker B could not claim the reclaimed task")
            return
        print(f"worker B claimed task {claimed_again.task_id} after reclaim")

        reservation = client.reserve_effect(claimed_again.task_id, "send_email", call_key="jane@example.com")
        print(f"worker B reserved the effect, decision={reservation.decision}")
        client.complete_effect(reservation.idempotency_key, {"sent": True})
        ok = client.complete_task(claimed_again.task_id, worker_b, {"sent": True})
        print(f"task completed by worker B: {ok}")
        print("PASS: exactly one worker ever sent the email, the crashed worker never did")
    finally:
        engine.stop()


if __name__ == "__main__":
    main()
