from typing import Optional

from anchor_client import AnchorClient, create_organization


def bootstrap(base_url: str, org_name: str) -> tuple:
    org_id, api_key = create_organization(base_url, org_name)
    client = AnchorClient(base_url, api_key)
    agent_ids = {
        "memory_agent_id": client.create_agent("orderdesk-brain", "memory"),
        "triage_worker_id": client.create_agent("triage-worker-1", "triage"),
        "draft_worker_id": client.create_agent("draft-worker-1", "draft"),
        "send_worker_id": client.create_agent("send-worker-1", "send"),
    }
    return client, agent_ids


def submit_request(
    client: AnchorClient, order_id: str, customer_email: str, message: str, *, dedup_key: Optional[str] = None
) -> str:
    return client.create_task(
        "triage",
        {"order_id": order_id, "customer_email": customer_email, "message": message},
        dedup_key=dedup_key,
    )
