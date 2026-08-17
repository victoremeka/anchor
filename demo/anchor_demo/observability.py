"""MCP client for an observability agent.

Points at any MCP server that exposes a SQL capable tool, including
CockroachDB Cloud's Managed MCP Server. That endpoint is read only and
audited on Cockroach's side, so this client never needs, and never gets,
the Engine's own database credentials.
"""

from contextlib import asynccontextmanager
from typing import Optional

import httpx
from mcp import ClientSession
from mcp.client.streamable_http import streamable_http_client


class NoSQLToolAvailable(Exception):
    pass


class ToolCallFailed(Exception):
    pass


class ObservabilityAgent:
    def __init__(self, mcp_url: str, api_key: Optional[str] = None):
        self._mcp_url = mcp_url
        self._api_key = api_key

    @asynccontextmanager
    async def _session(self):
        headers = {"Authorization": f"Bearer {self._api_key}"} if self._api_key else {}
        async with httpx.AsyncClient(headers=headers) as http_client:
            async with streamable_http_client(self._mcp_url, http_client=http_client) as (read, write):
                async with ClientSession(read, write) as session:
                    await session.initialize()
                    yield session

    async def list_tools(self) -> list:
        async with self._session() as session:
            result = await session.list_tools()
            return result.tools

    async def call_tool(self, name: str, arguments: dict) -> str:
        async with self._session() as session:
            result = await session.call_tool(name, arguments)
        return _extract_text(result)

    async def run_sql(self, sql: str, query_arg: str = "query") -> str:
        tools = await self.list_tools()
        tool_name = _find_sql_tool_name(tools)
        return await self.call_tool(tool_name, {query_arg: sql})


def _find_sql_tool_name(tools: list) -> str:
    for tool in tools:
        haystack = f"{tool.name} {tool.description or ''}".lower()
        if "sql" in haystack or "query" in haystack:
            return tool.name
    raise NoSQLToolAvailable(f"no SQL capable tool advertised, available tools: {[t.name for t in tools]}")


def _extract_text(result) -> str:
    parts = [block.text for block in result.content if hasattr(block, "text")]
    text = "\n".join(parts)
    if result.is_error:
        raise ToolCallFailed(text)
    return text
