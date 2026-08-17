import os

import uvicorn

from anchor_demo.mcp_server import build_server
from anchor_demo.scenarios._db import query

HOST = os.environ.get("ANCHOR_MCP_HOST", "127.0.0.1")
PORT = int(os.environ.get("ANCHOR_MCP_PORT", "8095"))


def run_query(sql: str) -> list:
    return query(sql)


def main() -> None:
    server = build_server(run_query)
    print(f"local reference MCP server listening on http://{HOST}:{PORT}/mcp")
    print("this stands in for CockroachDB Cloud's Managed MCP Server during local development")
    uvicorn.run(server.streamable_http_app(), host=HOST, port=PORT, log_level="info")


if __name__ == "__main__":
    main()
