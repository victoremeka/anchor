# Anchor

Transactional memory for AI agents, built on CockroachDB.

Anchor is a reliability layer for agent memory and task state. A crashed task cannot cause a duplicate side effect. Two workers cannot claim the same task. A stalled claim cannot orphan a task forever. A recalled memory is checked against live state before it is returned.

Full design rationale, guarantees, and the correctness test plan live in [docs/anchor-product-spec.md](docs/anchor-product-spec.md). Read that for the why behind every choice below.

Live deployment: https://d27whhcg0ttrqt.cloudfront.net

## What is in this repository

- `cmd/engine`, `internal/` — the Go Engine. The only service that holds a CockroachDB connection. Plain HTTP API, bearer token auth, race free task claiming, idempotent effect execution, freshness checked memory recall.
- `schema/schema.sql` — the CockroachDB schema: organizations, api_keys, agents, memories, tasks, executed_effects.
- `clients/python` — a thin Python HTTP client for the Engine's API.
- `demo/anchor_demo` — OrderDesk, a three agent LangGraph demo (triage, draft, send) built on the Python client, plus the four chaos scenarios described in the spec, and an MCP based observability agent.
- `Dockerfile` — builds the Engine as a small distroless container image.

## CockroachDB features used

- **Distributed Vector Indexing.** The `memories` table has a `VECTOR` column with a `VECTOR INDEX`, backing `recall`'s similarity search (`internal/engine/memory.go`).
- **Managed MCP Server.** `demo/anchor_demo/observability.py` is a real MCP client (official SDK) that discovers a SQL capable tool on any MCP server it is pointed at and asks it real questions: flagged task count, pending effect count, queue depth per pool. It has been proven working end to end against a local reference MCP server backed by this same database (`demo/anchor_demo/mcp_server.py`); pointing it at CockroachDB Cloud's real Managed MCP Server endpoint is a two environment variable change (`ANCHOR_MCP_URL`, `ANCHOR_MCP_API_KEY`), no code change.

## AWS services used

- **Amazon ECS (Fargate).** The Engine runs as a container on Fargate. Chosen over Lambda because the Engine's reclaim sweep is a background goroutine ticker; Lambda freezes execution environments between invocations, which is documented by AWS to produce unreliable behavior for code still running in the background. Fargate keeps the process alive so the sweep runs correctly.
- **Amazon ECR** hosts the container image.
- **AWS Secrets Manager** holds the database connection string; the ECS task's execution role can read only that one secret.
- **Amazon CloudFront** terminates public HTTPS in front of an internal ALB, since the Engine deliberately never terminates TLS itself (`cmd/engine/main.go`).

## Running the Engine locally

Requires a CockroachDB instance reachable over the Postgres wire protocol (a local single node Docker container works fine for development).

```
cockroach sql --insecure < schema/schema.sql

go build -o anchor-engine ./cmd/engine
ANCHOR_DATABASE_URL="postgresql://root@localhost:26257/anchor?sslmode=disable" \
ANCHOR_LISTEN_ADDR="127.0.0.1:8080" \
./anchor-engine
```

Environment variables, all optional except `ANCHOR_DATABASE_URL`:

| Variable | Default | Purpose |
|---|---|---|
| `ANCHOR_DATABASE_URL` | required | Postgres wire protocol connection string |
| `ANCHOR_LISTEN_ADDR` | `127.0.0.1:8080` | bind address. In a container, this must be set to `0.0.0.0:<port>`, the loopback default is unreachable from outside the container |
| `ANCHOR_LEASE_TTL_SECONDS` | `60` | task claim lease duration |
| `ANCHOR_RECLAIM_SWEEP_INTERVAL_SECONDS` | `15` | how often expired leases are reclaimed |
| `ANCHOR_DB_MAX_CONNS` | `10` | database connection pool size |
| `ANCHOR_LINKED_TABLES` | none | comma separated `table:id_column` pairs to register for freshness checked recall, for example `orders:order_id` |

The Engine speaks plain HTTP only, on purpose, and must run behind a TLS terminating proxy or load balancer.

Run the tests:

```
go test ./...
```

## Running the Python client and demo agents

```
cd clients/python && pip install -e .
cd demo && python -m venv .venv && .venv/bin/pip install -e ../clients/python -e . -e ".[test]"
```

Try the demo pipeline against a running Engine:

```
ANCHOR_BASE_URL=http://127.0.0.1:8080 .venv/bin/python run_demo.py
```

Run the chaos scenarios (each one manages its own Engine subprocess and needs a local CockroachDB reachable at `localhost:26257`):

```
.venv/bin/python -m anchor_demo.scenarios.scenario_a_reclaim
.venv/bin/python -m anchor_demo.scenarios.scenario_b_ambiguous
.venv/bin/python -m anchor_demo.scenarios.scenario_c_race
.venv/bin/python -m anchor_demo.scenarios.scenario_d_freshness
```

Run all tests:

```
.venv/bin/python -m pytest clients/python/tests demo/tests
```

## Deploying

See [DEPLOYMENT_STATUS.txt](DEPLOYMENT_STATUS.txt) for the exact live AWS resource inventory and the reasoning behind each infrastructure decision.
