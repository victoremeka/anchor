"""A local, read only MCP server that stands in for CockroachDB Cloud's
Managed MCP Server during development.

ObservabilityAgent is written against the standard MCP protocol, so it
works against either one unchanged. Point it at this server's URL while
building locally, then swap the URL and API key for the real Cloud
endpoint once it exists, no code changes required.

This is not a security equivalent of Cockroach's managed offering, which
enforces read only access and audit logging on their own infrastructure.
The guard here is a plain keyword check, good enough for local
development, not a substitute for a real access control boundary.
"""

from typing import Callable

from mcp.server.mcpserver import MCPServer

QueryFn = Callable[[str], list]

_ALLOWED_PREFIXES = ("select", "show", "explain")


def build_server(run_query: QueryFn, name: str = "anchor-local-mcp") -> MCPServer:
    server = MCPServer(name)

    @server.tool()
    def run_sql_query(query: str) -> str:
        """Run a read only SQL query against the Anchor database and return the matching rows."""
        if not query.strip().lower().startswith(_ALLOWED_PREFIXES):
            raise ValueError("only select, show, and explain statements are allowed")
        return str(run_query(query))

    return server
