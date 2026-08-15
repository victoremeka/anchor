package engine_test

// Shared helpers for tests that need a real CockroachDB instance.

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"anchor/internal/engine"
	"anchor/internal/registry"
	"anchor/internal/store"
)

func dsn(t *testing.T) string {
	t.Helper()
	v := os.Getenv("ANCHOR_TEST_DATABASE_URL")
	if v == "" {
		t.Skip("ANCHOR_TEST_DATABASE_URL not set; skipping integration test")
	}
	return v
}

func newTestEngine(t *testing.T, leaseTTL time.Duration) *engine.Engine {
	t.Helper()
	s, err := store.Open(context.Background(), dsn(t), 1)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(s.Close)
	return engine.New(s, registry.New(), leaseTTL)
}


func mustOrg(t *testing.T, e *engine.Engine) uuid.UUID {
	t.Helper()
	orgID, _, err := e.CreateOrganization(context.Background(), t.Name())
	if err != nil {
		t.Fatalf("create_organization: %v", err)
	}
	return orgID
}

func mustAgent(t *testing.T, e *engine.Engine, orgID uuid.UUID) uuid.UUID {
	t.Helper()
	id, err := e.CreateAgent(context.Background(), orgID, t.Name(), "test-pool")
	if err != nil {
		t.Fatalf("create_agent: %v", err)
	}
	return id
}

type closableEngine struct {
	engine *engine.Engine
	s      *store.Store
}

func (c closableEngine) close() { c.s.Close() }

func newTestEngineNoCleanup(t *testing.T, leaseTTL time.Duration) closableEngine {
	t.Helper()
	s, err := store.Open(context.Background(), dsn(t), 1)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	return closableEngine{engine: engine.New(s, registry.New(), leaseTTL), s: s}
}

func TestIntegration_Remember_Recall_Freshness(t *testing.T) {
	e := newTestEngine(t, time.Minute)
	ctx := context.Background()
	orgID := mustOrg(t, e)
	agentID := mustAgent(t, e, orgID)

	s, err := store.Open(ctx, dsn(t), 1)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer s.Close()
	if _, err := s.Pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS orders (order_id STRING PRIMARY KEY, status STRING)`); err != nil {
		t.Fatalf("create orders table: %v", err)
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO orders (order_id, status) VALUES ('order-1', 'shipped')
		ON CONFLICT (order_id) DO UPDATE SET status = excluded.status`); err != nil {
		t.Fatalf("seed order: %v", err)
	}

	if err := e.RegisterTable("orders", "order_id"); err != nil {
		t.Fatalf("register_table: %v", err)
	}

	embedding := make([]float32, 1536)
	embedding[0] = 1
	linkedTable, linkedID := "orders", "order-1"
	memID, err := e.Remember(ctx, orgID, agentID, "order-status", "the order shipped", embedding, &linkedTable, &linkedID)
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	if memID == uuid.Nil {
		t.Fatalf("remember returned a nil memory id")
	}

	results, err := e.Recall(ctx, orgID, agentID, "order-status", embedding, 5)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("recall returned no results")
	}
	got := results[0]
	if !got.FreshnessVerified {
		t.Fatalf("expected freshness_verified=true, got false")
	}
	if string(got.LiveValue) == "" {
		t.Fatalf("expected a non-empty live_value")
	}

	if _, err := s.Pool.Exec(ctx, `UPDATE orders SET status = 'delivered' WHERE order_id = 'order-1'`); err != nil {
		t.Fatalf("update order: %v", err)
	}
	results, err = e.Recall(ctx, orgID, agentID, "order-status", embedding, 5)
	if err != nil {
		t.Fatalf("recall after update: %v", err)
	}
	if got := string(results[0].LiveValue); !strings.Contains(got, "delivered") {
		t.Fatalf("live_value did not reflect the update: %s", got)
	}
}

func unmarshalJSON(t *testing.T, raw json.RawMessage, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
}
