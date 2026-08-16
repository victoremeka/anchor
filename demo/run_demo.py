import os

from anchor_demo.agents import (
    build_draft_graph,
    build_send_graph,
    build_triage_graph,
    draft_initial_state,
    send_initial_state,
    triage_initial_state,
)
from anchor_demo.pipeline import bootstrap, submit_request
from anchor_demo.worker import drain

BASE_URL = os.environ.get("ANCHOR_BASE_URL", "http://127.0.0.1:8090")


def main() -> None:
    client, agents = bootstrap(BASE_URL, "orderdesk-demo")
    print(f"org bootstrapped, agents={agents}")

    task_id = submit_request(
        client,
        order_id="order-1",
        customer_email="jane@example.com",
        message="Where is my order? It has been a week.",
    )
    print(f"submitted triage task {task_id}")

    triage_graph = build_triage_graph(client, agents["memory_agent_id"])
    draft_graph = build_draft_graph(client, agents["memory_agent_id"])
    send_graph = build_send_graph(client)

    triaged = drain(client, "triage", agents["triage_worker_id"], triage_graph, triage_initial_state)
    print(f"triage processed {triaged} task(s)")

    drafted = drain(client, "draft", agents["draft_worker_id"], draft_graph, draft_initial_state)
    print(f"draft processed {drafted} task(s)")

    sent = drain(client, "send", agents["send_worker_id"], send_graph, send_initial_state)
    print(f"send processed {sent} task(s)")


if __name__ == "__main__":
    main()
