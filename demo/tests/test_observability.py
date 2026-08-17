import asyncio
import threading
import time

import httpx
import pytest
import uvicorn

from anchor_demo.mcp_server import build_server
from anchor_demo.observability import ObservabilityAgent, ToolCallFailed

PORT = 8765


def _fake_query(sql: str) -> list:
    if "flagged" in sql.lower():
        return [{"n": 2}]
    return [{"n": 0}]


@pytest.fixture(scope="module")
def server_url():
    server = build_server(_fake_query)
    app = server.streamable_http_app()
    config = uvicorn.Config(app, host="127.0.0.1", port=PORT, log_level="warning")
    uv_server = uvicorn.Server(config)

    thread = threading.Thread(target=uv_server.run, daemon=True)
    thread.start()

    deadline = time.time() + 5
    while time.time() < deadline:
        try:
            httpx.get(f"http://127.0.0.1:{PORT}/mcp", timeout=0.2)
            break
        except httpx.TransportError:
            time.sleep(0.1)

    yield f"http://127.0.0.1:{PORT}/mcp"

    uv_server.should_exit = True
    thread.join(timeout=5)


def test_list_tools_returns_the_sql_tool(server_url):
    agent = ObservabilityAgent(server_url)
    tools = asyncio.run(agent.list_tools())
    assert any(t.name == "run_sql_query" for t in tools)


def test_run_sql_executes_through_the_discovered_tool(server_url):
    agent = ObservabilityAgent(server_url)
    result = asyncio.run(agent.run_sql("SELECT count(*) AS n FROM tasks WHERE status = 'flagged'"))
    assert "2" in result


def test_run_sql_rejects_non_read_statements(server_url):
    agent = ObservabilityAgent(server_url)
    with pytest.raises(ToolCallFailed, match="only select"):
        asyncio.run(agent.run_sql("DELETE FROM tasks"))
