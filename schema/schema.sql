-- Anchor schema — mirrors anchor-product-spec.md §7. Keep the two in sync.
--
-- Vector indexes are gated behind a cluster setting; run this once per
-- cluster before applying this file:
--   SET CLUSTER SETTING feature.vector_index.enabled = true;
--
-- Every tenant-owned table carries org_id directly (not just derivable
-- via a join) so every query that touches tenant data can filter on it
-- without a join, and so a validation bug in one path can't leak another
-- org's rows through a forgotten join condition.

CREATE TABLE IF NOT EXISTS organizations (
    org_id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          STRING NOT NULL,
    created_at    TIMESTAMPTZ DEFAULT now()
);

-- Keys are a separate table, not a column on organizations, so an org can
-- hold more than one at a time: rotation mints a new key without
-- invalidating the old one, and revocation kills one specific key rather
-- than the org's only credential.
CREATE TABLE IF NOT EXISTS api_keys (
    key_id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      UUID NOT NULL REFERENCES organizations(org_id),
    key_hash    BYTES NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT now(),
    revoked_at  TIMESTAMPTZ,
    UNIQUE INDEX idx_key_hash (key_hash),
    INDEX idx_org (org_id)
);

CREATE TABLE IF NOT EXISTS agents (
    agent_id     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       UUID NOT NULL REFERENCES organizations(org_id),
    name         STRING NOT NULL,
    pool         STRING NOT NULL,
    created_at   TIMESTAMPTZ DEFAULT now(),
    INDEX idx_org_pool (org_id, pool)
);

CREATE TABLE IF NOT EXISTS memories (
    memory_id    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       UUID NOT NULL REFERENCES organizations(org_id),
    agent_id     UUID NOT NULL REFERENCES agents(agent_id),
    subject_id   STRING NOT NULL,
    content      STRING NOT NULL,
    embedding    VECTOR(1536),
    linked_table STRING,
    linked_id    STRING,
    created_at   TIMESTAMPTZ DEFAULT now(),
    INDEX idx_subject (org_id, agent_id, subject_id),
    VECTOR INDEX idx_embedding (embedding)
);

CREATE TABLE IF NOT EXISTS tasks (
    task_id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id            UUID NOT NULL REFERENCES organizations(org_id),
    agent_pool        STRING NOT NULL,
    payload           JSONB NOT NULL,
    status            STRING NOT NULL DEFAULT 'pending',  -- pending | claimed | flagged | done
    claimed_by        UUID REFERENCES agents(agent_id),
    claimed_at        TIMESTAMPTZ,
    claim_expires_at  TIMESTAMPTZ,
    result            JSONB,
    dedup_key         STRING,
    created_at        TIMESTAMPTZ DEFAULT now(),
    INDEX idx_queue (org_id, agent_pool, status, created_at),
    UNIQUE INDEX idx_dedup (org_id, dedup_key) WHERE dedup_key IS NOT NULL
);

CREATE TABLE IF NOT EXISTS executed_effects (
    idempotency_key  STRING PRIMARY KEY,
    org_id           UUID NOT NULL REFERENCES organizations(org_id),
    task_id          UUID NOT NULL REFERENCES tasks(task_id),
    tool_name        STRING NOT NULL,
    status           STRING NOT NULL DEFAULT 'pending',   -- pending | completed
    result           JSONB,
    created_at       TIMESTAMPTZ DEFAULT now(),
    completed_at     TIMESTAMPTZ
);
