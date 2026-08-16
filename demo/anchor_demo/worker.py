from typing import Callable

from anchor_client import AnchorClient, ClaimedTask


def run_one(client: AnchorClient, pool: str, worker_id: str, graph, build_state: Callable[[ClaimedTask], dict]) -> bool:
    task = client.claim_task(pool, worker_id)
    if task is None:
        return False
    graph.invoke(build_state(task))
    return True


def drain(client: AnchorClient, pool: str, worker_id: str, graph, build_state: Callable[[ClaimedTask], dict]) -> int:
    count = 0
    while run_one(client, pool, worker_id, graph, build_state):
        count += 1
    return count
