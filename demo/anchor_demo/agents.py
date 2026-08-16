from typing import Optional, TypedDict

from langgraph.graph import END, START, StateGraph

from anchor_client import AnchorClient, EFFECT_ALREADY_DONE, EFFECT_AMBIGUOUS, EFFECT_RUN
from anchor_client.models import ClaimedTask

from .embeddings import simple_embed
from .reasoning import classify_intent, draft_reply
from .tools import send_email


class TriageState(TypedDict):
    task_id: str
    claimed_by: str
    order_id: str
    customer_email: str
    message: str
    intent: str


def triage_initial_state(task: ClaimedTask) -> TriageState:
    payload = task.payload
    return {
        "task_id": task.task_id,
        "claimed_by": task.claimed_by,
        "order_id": payload["order_id"],
        "customer_email": payload["customer_email"],
        "message": payload["message"],
        "intent": "",
    }


def build_triage_graph(client: AnchorClient, memory_agent_id: str):
    def classify(state: TriageState) -> dict:
        return {"intent": classify_intent(state["message"])}

    def remember(state: TriageState) -> dict:
        content = f"Customer emailed about order {state['order_id']}: {state['message']}"
        client.remember(
            memory_agent_id,
            state["order_id"],
            content,
            simple_embed(content),
            linked_table="orders",
            linked_id=state["order_id"],
        )
        return {}

    def enqueue_draft(state: TriageState) -> dict:
        client.create_task(
            "draft",
            {
                "order_id": state["order_id"],
                "customer_email": state["customer_email"],
                "message": state["message"],
                "intent": state["intent"],
            },
            dedup_key=f"draft-for-{state['task_id']}",
        )
        return {}

    def complete(state: TriageState) -> dict:
        client.complete_task(state["task_id"], state["claimed_by"], {"intent": state["intent"]})
        return {}

    graph = StateGraph(TriageState)
    graph.add_node("classify", classify)
    graph.add_node("remember", remember)
    graph.add_node("enqueue_draft", enqueue_draft)
    graph.add_node("complete", complete)
    graph.add_edge(START, "classify")
    graph.add_edge("classify", "remember")
    graph.add_edge("remember", "enqueue_draft")
    graph.add_edge("enqueue_draft", "complete")
    graph.add_edge("complete", END)
    return graph.compile()


class DraftState(TypedDict):
    task_id: str
    claimed_by: str
    order_id: str
    customer_email: str
    message: str
    intent: str
    live_status: str
    subject: str
    body: str


def draft_initial_state(task: ClaimedTask) -> DraftState:
    payload = task.payload
    return {
        "task_id": task.task_id,
        "claimed_by": task.claimed_by,
        "order_id": payload["order_id"],
        "customer_email": payload["customer_email"],
        "message": payload["message"],
        "intent": payload["intent"],
        "live_status": "",
        "subject": "",
        "body": "",
    }


def build_draft_graph(client: AnchorClient, memory_agent_id: str):
    def recall_context(state: DraftState) -> dict:
        query = f"Customer emailed about order {state['order_id']}: {state['message']}"
        results = client.recall(memory_agent_id, state["order_id"], simple_embed(query))
        live_status = "unknown"
        for r in results:
            if r.freshness_verified and r.live_value:
                live_status = r.live_value["status"]
                break
        return {"live_status": live_status}

    def generate_reply(state: DraftState) -> dict:
        subject, body = draft_reply(state["order_id"], state["intent"], state["live_status"])
        return {"subject": subject, "body": body}

    def enqueue_send(state: DraftState) -> dict:
        client.create_task(
            "send",
            {
                "order_id": state["order_id"],
                "customer_email": state["customer_email"],
                "subject": state["subject"],
                "body": state["body"],
            },
            dedup_key=f"send-for-{state['task_id']}",
        )
        return {}

    def complete(state: DraftState) -> dict:
        client.complete_task(state["task_id"], state["claimed_by"], {"subject": state["subject"]})
        return {}

    graph = StateGraph(DraftState)
    graph.add_node("recall_context", recall_context)
    graph.add_node("generate_reply", generate_reply)
    graph.add_node("enqueue_send", enqueue_send)
    graph.add_node("complete", complete)
    graph.add_edge(START, "recall_context")
    graph.add_edge("recall_context", "generate_reply")
    graph.add_edge("generate_reply", "enqueue_send")
    graph.add_edge("enqueue_send", "complete")
    graph.add_edge("complete", END)
    return graph.compile()


class SendState(TypedDict):
    task_id: str
    claimed_by: str
    order_id: str
    customer_email: str
    subject: str
    body: str
    idempotency_key: str
    decision: str
    result: Optional[dict]


def send_initial_state(task: ClaimedTask) -> SendState:
    payload = task.payload
    return {
        "task_id": task.task_id,
        "claimed_by": task.claimed_by,
        "order_id": payload["order_id"],
        "customer_email": payload["customer_email"],
        "subject": payload["subject"],
        "body": payload["body"],
        "idempotency_key": "",
        "decision": "",
        "result": None,
    }


def build_send_graph(client: AnchorClient):
    def reserve(state: SendState) -> dict:
        reservation = client.reserve_effect(state["task_id"], "send_email", call_key=state["customer_email"])
        return {
            "idempotency_key": reservation.idempotency_key,
            "decision": reservation.decision,
            "result": reservation.result,
        }

    def call_tool(state: SendState) -> dict:
        result = send_email(state["customer_email"], state["subject"], state["body"])
        return {"result": result}

    def complete_effect(state: SendState) -> dict:
        client.complete_effect(state["idempotency_key"], state["result"])
        return {}

    def complete_task(state: SendState) -> dict:
        client.complete_task(state["task_id"], state["claimed_by"], state["result"])
        return {}

    def halt_ambiguous(state: SendState) -> dict:
        print(f"[send] task {state['task_id']} is ambiguous, leaving it flagged for an operator")
        return {}

    def route(state: SendState) -> str:
        return state["decision"]

    graph = StateGraph(SendState)
    graph.add_node("reserve", reserve)
    graph.add_node("call_tool", call_tool)
    graph.add_node("complete_effect", complete_effect)
    graph.add_node("complete_task", complete_task)
    graph.add_node("halt_ambiguous", halt_ambiguous)
    graph.add_edge(START, "reserve")
    graph.add_conditional_edges(
        "reserve",
        route,
        {EFFECT_RUN: "call_tool", EFFECT_ALREADY_DONE: "complete_task", EFFECT_AMBIGUOUS: "halt_ambiguous"},
    )
    graph.add_edge("call_tool", "complete_effect")
    graph.add_edge("complete_effect", "complete_task")
    graph.add_edge("complete_task", END)
    graph.add_edge("halt_ambiguous", END)
    return graph.compile()
