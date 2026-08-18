import asyncio
import os

from anchor_demo.observability import ObservabilityAgent

MCP_URL = os.environ["ANCHOR_MCP_URL"]
API_KEY = os.environ.get("ANCHOR_MCP_API_KEY")
CLUSTER_ID = os.environ.get("ANCHOR_MCP_CLUSTER_ID")
DATABASE = os.environ.get("ANCHOR_MCP_DATABASE", "defaultdb")

QUESTIONS = {
    "flagged tasks": "SELECT count(*) AS n FROM tasks WHERE status = 'flagged'",
    "pending effects": "SELECT count(*) AS n FROM executed_effects WHERE status = 'pending'",
    "queue depth by pool": "SELECT agent_pool, count(*) AS n FROM tasks WHERE status = 'pending' GROUP BY agent_pool",
}


async def main() -> None:
    agent = ObservabilityAgent(MCP_URL, API_KEY, CLUSTER_ID)
    print(f"connected tools: {[t.name for t in await agent.list_tools()]}")
    for label, sql in QUESTIONS.items():
        answer = await agent.run_sql(sql, database=DATABASE)
        print(f"{label}: {answer}")


if __name__ == "__main__":
    asyncio.run(main())
