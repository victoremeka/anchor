"""Two workers race to claim the same task at the same time.

Expected outcome: exactly one of them wins, enforced by the database, not
by application logic.
"""

from concurrent.futures import ThreadPoolExecutor

from anchor_client import AnchorClient, create_organization

from ._engine import ManagedEngine

ADDR = "127.0.0.1:8093"


def main() -> None:
    engine = ManagedEngine(ADDR)
    engine.start()
    try:
        org_id, api_key = create_organization(engine.base_url, "scenario-c")
        client = AnchorClient(engine.base_url, api_key)
        worker_a = client.create_agent("racer-a", "send")
        worker_b = client.create_agent("racer-b", "send")
        client.create_task("send", {"to": "jane@example.com"})

        with ThreadPoolExecutor(max_workers=2) as pool:
            future_a = pool.submit(client.claim_task, "send", worker_a)
            future_b = pool.submit(client.claim_task, "send", worker_b)
            result_a = future_a.result()
            result_b = future_b.result()

        winners = [r for r in (result_a, result_b) if r is not None]
        print(f"worker A got: {result_a}")
        print(f"worker B got: {result_b}")

        if len(winners) == 1:
            print("PASS: exactly one worker claimed the task")
        else:
            print(f"FAIL: expected exactly one winner, got {len(winners)}")
    finally:
        engine.stop()


if __name__ == "__main__":
    main()
