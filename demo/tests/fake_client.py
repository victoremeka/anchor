from anchor_client.models import EffectReservation, RecalledMemory


class FakeAnchorClient:
    def __init__(self):
        self.remembered = []
        self.created_tasks = []
        self.completed_tasks = []
        self.completed_effects = []
        self.recall_results = []
        self.reservation = None

    def remember(self, agent_id, subject_id, content, embedding, *, linked_table=None, linked_id=None):
        self.remembered.append(
            {
                "agent_id": agent_id,
                "subject_id": subject_id,
                "content": content,
                "linked_table": linked_table,
                "linked_id": linked_id,
            }
        )
        return "memory-1"

    def create_task(self, agent_pool, payload=None, *, dedup_key=None):
        self.created_tasks.append({"agent_pool": agent_pool, "payload": payload, "dedup_key": dedup_key})
        return "task-2"

    def complete_task(self, task_id, claimed_by, result=None):
        self.completed_tasks.append({"task_id": task_id, "claimed_by": claimed_by, "result": result})
        return True

    def recall(self, agent_id, subject_id, query_embedding, *, top_k=5):
        return self.recall_results

    def reserve_effect(self, task_id, tool_name, *, call_key=""):
        return self.reservation

    def complete_effect(self, idempotency_key, result=None):
        self.completed_effects.append({"idempotency_key": idempotency_key, "result": result})
