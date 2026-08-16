from anchor_client import EFFECT_ALREADY_DONE, EFFECT_AMBIGUOUS, EFFECT_RUN
from anchor_client.models import ClaimedTask, EffectReservation, RecalledMemory

from anchor_demo.agents import (
    build_draft_graph,
    build_send_graph,
    build_triage_graph,
    draft_initial_state,
    send_initial_state,
    triage_initial_state,
)

from .fake_client import FakeAnchorClient


def test_triage_graph_writes_memory_and_enqueues_draft():
    client = FakeAnchorClient()
    graph = build_triage_graph(client, memory_agent_id="brain-1")
    task = ClaimedTask(
        task_id="task-1",
        agent_pool="triage",
        payload={"order_id": "order-1", "customer_email": "jane@example.com", "message": "Where is my order?"},
        claimed_by="worker-1",
    )

    graph.invoke(triage_initial_state(task))

    assert client.remembered[0]["agent_id"] == "brain-1"
    assert client.remembered[0]["linked_table"] == "orders"
    assert client.remembered[0]["linked_id"] == "order-1"

    assert client.created_tasks[0]["agent_pool"] == "draft"
    assert client.created_tasks[0]["dedup_key"] == "draft-for-task-1"
    assert client.created_tasks[0]["payload"]["intent"] == "status_inquiry"

    assert client.completed_tasks[0] == {"task_id": "task-1", "claimed_by": "worker-1", "result": {"intent": "status_inquiry"}}


def test_draft_graph_uses_live_value_over_stale_content():
    client = FakeAnchorClient()
    client.recall_results = [
        RecalledMemory(
            memory_id="memory-1",
            content="Order order-1 is being processed.",
            freshness_verified=True,
            live_value={"order_id": "order-1", "status": "refunded"},
        )
    ]
    graph = build_draft_graph(client, memory_agent_id="brain-1")
    task = ClaimedTask(
        task_id="task-2",
        agent_pool="draft",
        payload={
            "order_id": "order-1",
            "customer_email": "jane@example.com",
            "message": "Where is my order?",
            "intent": "status_inquiry",
        },
        claimed_by="worker-1",
    )

    graph.invoke(draft_initial_state(task))

    body = client.created_tasks[0]["payload"]["body"]
    assert "refunded" in body
    assert client.created_tasks[0]["dedup_key"] == "send-for-task-2"


def test_send_graph_calls_tool_on_run_decision():
    client = FakeAnchorClient()
    client.reservation = EffectReservation(idempotency_key="idem-1", decision=EFFECT_RUN, result=None)
    graph = build_send_graph(client)
    task = ClaimedTask(
        task_id="task-3",
        agent_pool="send",
        payload={"order_id": "order-1", "customer_email": "jane@example.com", "subject": "s", "body": "b"},
        claimed_by="worker-1",
    )

    graph.invoke(send_initial_state(task))

    assert client.completed_effects[0]["idempotency_key"] == "idem-1"
    assert client.completed_tasks[0]["task_id"] == "task-3"
    assert client.completed_tasks[0]["result"]["to"] == "jane@example.com"


def test_send_graph_skips_tool_on_already_done():
    client = FakeAnchorClient()
    client.reservation = EffectReservation(idempotency_key="idem-1", decision=EFFECT_ALREADY_DONE, result={"message_id": "old-1"})
    graph = build_send_graph(client)
    task = ClaimedTask(
        task_id="task-4",
        agent_pool="send",
        payload={"order_id": "order-1", "customer_email": "jane@example.com", "subject": "s", "body": "b"},
        claimed_by="worker-1",
    )

    graph.invoke(send_initial_state(task))

    assert client.completed_effects == []
    assert client.completed_tasks[0]["result"] == {"message_id": "old-1"}


def test_send_graph_halts_on_ambiguous():
    client = FakeAnchorClient()
    client.reservation = EffectReservation(idempotency_key="idem-1", decision=EFFECT_AMBIGUOUS, result=None)
    graph = build_send_graph(client)
    task = ClaimedTask(
        task_id="task-5",
        agent_pool="send",
        payload={"order_id": "order-1", "customer_email": "jane@example.com", "subject": "s", "body": "b"},
        claimed_by="worker-1",
    )

    graph.invoke(send_initial_state(task))

    assert client.completed_effects == []
    assert client.completed_tasks == []
