"""A human support agent refunds an order in the orders table directly,
bypassing Anchor entirely, seconds after the triage agent wrote a memory
saying the order was still processing.

Expected outcome: the draft agent's recall shows the refund immediately,
even though the memory content it wrote earlier still says "processing."
"""

import json

from anchor_client import AnchorClient, create_organization

from ..agents import build_draft_graph, build_triage_graph, draft_initial_state, triage_initial_state
from ..pipeline import submit_request
from ..worker import drain
from . import _db
from ._engine import ManagedEngine

ADDR = "127.0.0.1:8094"


def main() -> None:
    engine = ManagedEngine(ADDR)
    engine.start()
    try:
        _db.query("UPDATE orders SET status = 'processing' WHERE order_id = 'order-1'")

        org_id, api_key = create_organization(engine.base_url, "scenario-d")
        client = AnchorClient(engine.base_url, api_key)
        memory_agent_id = client.create_agent("orderdesk-brain", "memory")
        triage_worker_id = client.create_agent("triage-worker-1", "triage")
        draft_worker_id = client.create_agent("draft-worker-1", "draft")

        submit_request(
            client, order_id="order-1", customer_email="jane@example.com", message="Where is my order?"
        )
        triage_graph = build_triage_graph(client, memory_agent_id)
        drain(client, "triage", triage_worker_id, triage_graph, triage_initial_state)
        print("triage wrote a memory: order-1 is being processed")

        print("a human support agent refunds order-1 directly in the orders table, Anchor never sees this write")
        _db.query("UPDATE orders SET status = 'refunded' WHERE order_id = 'order-1'")

        draft_graph = build_draft_graph(client, memory_agent_id)
        drain(client, "draft", draft_worker_id, draft_graph, draft_initial_state)

        memory_rows = _db.query(
            f"SELECT content FROM memories WHERE agent_id = '{memory_agent_id}' ORDER BY created_at DESC LIMIT 1"
        )
        stale_content = memory_rows[0]["content"]
        print(f"the stored memory content still says: {stale_content!r}")

        send_task_rows = _db.query(
            f"SELECT payload FROM tasks WHERE org_id = '{org_id}' AND agent_pool = 'send' "
            "ORDER BY created_at DESC LIMIT 1"
        )
        drafted_body = json.loads(send_task_rows[0]["payload"])["body"]
        print(f"but the drafted reply says: {drafted_body!r}")

        if "refunded" in drafted_body:
            print("PASS: the drafted reply reflects the live status, not the stale memory content")
        else:
            print("FAIL: expected the drafted reply to mention the refund")
    finally:
        engine.stop()


if __name__ == "__main__":
    main()
