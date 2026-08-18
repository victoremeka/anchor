Anchor

Transactional memory for AI agents, built on CockroachDB.

Anchor is a reliability layer for agent memory and task state. It handles things that become difficult once agents can take real actions: preventing duplicate side effects, coordinating multiple workers, recovering abandoned tasks, and making sure recalled memories are still valid.

Live deployment: https://d27whhcg0ttrqt.cloudfront.net

What is in this repository
	•	cmd/engine, internal/ - The Go Engine. This is the only service that connects to CockroachDB. It provides the HTTP API, bearer token authentication, task claiming, idempotent effect execution, and freshness-checked memory recall.
	•	schema/schema.sql - The CockroachDB schema for organizations, API keys, agents, memories, tasks, and executed effects.
	•	clients/python - A small Python client for the Engine API.
	•	demo/anchor_demo - OrderDesk, a three-agent LangGraph demo using the Python client. It includes four chaos scenarios and an MCP-based observability agent.
	•	Dockerfile - Builds the Engine into a small distroless container.

CockroachDB features used

Anchor uses CockroachDB for both task state and agent memory.
	•	Vector indexing - The memories table has a VECTOR column and vector index used for similarity search during memory recall.
	•	Managed MCP Server - The observability demo uses the official MCP SDK to connect to a SQL-capable MCP tool and query the database for things like flagged tasks, pending effects, and queue depth by pool.

There is also a local MCP server at demo/anchor_demo/mcp_server.py for offline development using the same client code.

AWS deployment

The live Engine runs on AWS:
	•	ECS Fargate runs the Engine container. Anchor has a background reclaim sweep, so the Engine needs to remain running rather than being paused between requests.
	•	ECR stores the container image.
	•	Secrets Manager stores the CockroachDB connection string. The ECS task only has access to that secret.
	•	CloudFront + ALB handle public traffic. CloudFront terminates HTTPS and forwards requests to the internal ALB. The Engine itself only speaks HTTP.

Running the Engine locally

You need a CockroachDB instance reachable over the Postgres wire protocol. A local single-node Docker instance works for development.

Initialize the schema:

cockroach sql --insecure < schema/schema.sql

Build the Engine:

go build -o anchor-engine ./cmd/engine

Start it:

ANCHOR_DATABASE_URL="postgresql://root@localhost:26257/anchor?sslmode=disable" \
ANCHOR_LISTEN_ADDR="127.0.0.1:8080" \
./anchor-engine

Environment variables

All variables are optional except ANCHOR_DATABASE_URL.

Variable	Default	Purpose
ANCHOR_DATABASE_URL	required	CockroachDB connection string
ANCHOR_LISTEN_ADDR	127.0.0.1:8080	Address the Engine listens on
ANCHOR_LEASE_TTL_SECONDS	60	Task claim lease duration
ANCHOR_RECLAIM_SWEEP_INTERVAL_SECONDS	15	How often expired leases are reclaimed
ANCHOR_DB_MAX_CONNS	10	Database connection pool size
ANCHOR_LINKED_TABLES	none	Comma-separated table:id_column pairs used for freshness checks, e.g. orders:order_id

When running in a container, ANCHOR_LISTEN_ADDR should be set to 0.0.0.0:<port> so the Engine can receive traffic from outside the container.

The Engine intentionally speaks plain HTTP and should run behind a TLS-terminating proxy or load balancer in production.

Run the tests:

go test ./...

Python client and demo

Install the Python client and demo:

cd clients/python && pip install -e .
cd demo && python -m venv .venv && .venv/bin/pip install -e ../clients/python -e . -e ".[test]"

With the Engine running locally:

ANCHOR_BASE_URL=http://127.0.0.1:8080 .venv/bin/python run_demo.py

This runs the OrderDesk demo using the Python client.

Chaos scenarios

The demo includes four scenarios covering the main failure cases Anchor is designed to handle:

.venv/bin/python -m anchor_demo.scenarios.scenario_a_reclaim
.venv/bin/python -m anchor_demo.scenarios.scenario_b_ambiguous
.venv/bin/python -m anchor_demo.scenarios.scenario_c_race
.venv/bin/python -m anchor_demo.scenarios.scenario_d_freshness

Each scenario manages its own Engine subprocess and expects a local CockroachDB instance at localhost:26257.
