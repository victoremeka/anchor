package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"anchor/internal/engine"
	"anchor/internal/httpapi"
	"anchor/internal/registry"
	"anchor/internal/store"
)

func dsn(t *testing.T) string {
	t.Helper()
	v := os.Getenv("ANCHOR_TEST_DATABASE_URL")
	if v == "" {
		t.Skip("ANCHOR_TEST_DATABASE_URL not set; skipping http integration test")
	}
	return v
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	s, err := store.Open(context.Background(), dsn(t), 1)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(s.Close)
	e := engine.New(s, registry.New(), time.Minute)
	srv := httptest.NewServer(httpapi.New(e, nil).Mux())
	t.Cleanup(srv.Close)
	return srv
}

type testOrg struct {
	baseURL string
	apiKey  string
}

func newTestOrg(t *testing.T) testOrg {
	t.Helper()
	srv := newTestServer(t)
	resp := doJSON(t, "", http.MethodPost, srv.URL+"/organizations", map[string]any{"name": t.Name()})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create_organization status = %d, want 201", resp.StatusCode)
	}
	var created struct {
		OrgID  uuid.UUID `json:"org_id"`
		APIKey string    `json:"api_key"`
	}
	decodeBody(t, resp, &created)
	return testOrg{baseURL: srv.URL, apiKey: created.APIKey}
}

func (o testOrg) do(t *testing.T, method, path string, body any) *http.Response {
	t.Helper()
	return doJSON(t, o.apiKey, method, o.baseURL+path, body)
}

func (o testOrg) mustAgentID(t *testing.T, pool string) uuid.UUID {
	t.Helper()
	resp := o.do(t, http.MethodPost, "/agents", map[string]any{"name": t.Name(), "pool": pool})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create_agent status = %d, want 201", resp.StatusCode)
	}
	var created struct {
		AgentID uuid.UUID `json:"agent_id"`
	}
	decodeBody(t, resp, &created)
	return created.AgentID
}

func doJSON(t *testing.T, bearer, method, url string, body any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}
	req, err := http.NewRequest(method, url, &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decodeBody(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
}

func TestHTTP_CreateOrganization_ReturnsUsableKey(t *testing.T) {
	org := newTestOrg(t)
	if org.apiKey == "" {
		t.Fatalf("create_organization returned an empty api_key")
	}

	resp := org.do(t, http.MethodPost, "/agents", map[string]any{"name": "a", "pool": "p"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("authenticated call with fresh key status = %d, want 201", resp.StatusCode)
	}
}

func TestHTTP_RequireAuth_RejectsMissingAndInvalidKeys(t *testing.T) {
	srv := newTestServer(t)

	resp := doJSON(t, "", http.MethodPost, srv.URL+"/tasks", map[string]any{"agent_pool": "p", "payload": map[string]any{}})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no bearer token: status = %d, want 401", resp.StatusCode)
	}

	resp = doJSON(t, "anchor_not-a-real-key", http.MethodPost, srv.URL+"/tasks", map[string]any{"agent_pool": "p", "payload": map[string]any{}})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bogus bearer token: status = %d, want 401", resp.StatusCode)
	}
}

func TestHTTP_CreateClaimComplete(t *testing.T) {
	org := newTestOrg(t)
	pool := "http-pool-" + uuid.NewString()

	resp := org.do(t, http.MethodPost, "/tasks", map[string]any{
		"agent_pool": pool,
		"payload":    map[string]any{"hello": "world"},
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create_task status = %d, want 201", resp.StatusCode)
	}
	var created struct {
		TaskID uuid.UUID `json:"task_id"`
	}
	decodeBody(t, resp, &created)

	workerID := org.mustAgentID(t, pool)
	resp = org.do(t, http.MethodPost, "/tasks/claim", map[string]any{
		"agent_pool": pool,
		"worker_id":  workerID,
	})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("claim_task status = %d, want 200, body = %s", resp.StatusCode, body)
	}
	var claimed struct {
		TaskID    uuid.UUID `json:"task_id"`
		ClaimedBy uuid.UUID `json:"claimed_by"`
	}
	decodeBody(t, resp, &claimed)
	if claimed.TaskID != created.TaskID {
		t.Fatalf("claimed the wrong task")
	}
	if claimed.ClaimedBy != workerID {
		t.Fatalf("claimed_by = %s, want %s", claimed.ClaimedBy, workerID)
	}

	resp = org.do(t, http.MethodPost, "/tasks/"+created.TaskID.String()+"/complete", map[string]any{
		"claimed_by": workerID,
		"result":     map[string]any{"ok": true},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("complete_task status = %d, want 200", resp.StatusCode)
	}
	var completed struct {
		OK bool `json:"ok"`
	}
	decodeBody(t, resp, &completed)
	if !completed.OK {
		t.Fatalf("complete_task ok = false")
	}
}

func TestHTTP_ClaimTask_NoneAvailable(t *testing.T) {
	org := newTestOrg(t)
	resp := org.do(t, http.MethodPost, "/tasks/claim", map[string]any{
		"agent_pool": "empty-pool-" + uuid.NewString(),
		"worker_id":  org.mustAgentID(t, "empty-pool"),
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHTTP_CreateTask_MissingField(t *testing.T) {
	org := newTestOrg(t)
	resp := org.do(t, http.MethodPost, "/tasks", map[string]any{"payload": map[string]any{}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHTTP_CrossOrg_CannotClaimAnotherOrgsTask(t *testing.T) {
	orgA := newTestOrg(t)
	orgB := newTestOrg(t)
	pool := "isolation-pool-" + uuid.NewString()

	resp := orgA.do(t, http.MethodPost, "/tasks", map[string]any{"agent_pool": pool, "payload": map[string]any{}})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create_task status = %d", resp.StatusCode)
	}

	// org B uses the SAME pool name — its agent must not see org A's task.
	workerB := orgB.mustAgentID(t, pool)
	resp = orgB.do(t, http.MethodPost, "/tasks/claim", map[string]any{
		"agent_pool": pool,
		"worker_id":  workerB,
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("org B claimed org A's task via a shared pool name: status = %d, want 404", resp.StatusCode)
	}
}

func TestHTTP_Effect_ReserveCompleteAndReReserve(t *testing.T) {
	org := newTestOrg(t)
	pool := "http-effect-pool-" + uuid.NewString()

	resp := org.do(t, http.MethodPost, "/tasks", map[string]any{
		"agent_pool": pool,
		"payload":    map[string]any{},
	})
	var created struct {
		TaskID uuid.UUID `json:"task_id"`
	}
	decodeBody(t, resp, &created)

	resp = org.do(t, http.MethodPost, "/tasks/"+created.TaskID.String()+"/effects/reserve", map[string]any{
		"tool_name": "send_email",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reserve status = %d, want 200", resp.StatusCode)
	}
	var reserved struct {
		IdempotencyKey string `json:"idempotency_key"`
		Decision       string `json:"decision"`
	}
	decodeBody(t, resp, &reserved)
	if reserved.Decision != string(engine.EffectRun) {
		t.Fatalf("decision = %s, want run", reserved.Decision)
	}

	resp = org.do(t, http.MethodPost, "/effects/"+reserved.IdempotencyKey+"/complete", map[string]any{
		"result": map[string]any{"sent": true},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("complete effect status = %d, want 200", resp.StatusCode)
	}

	resp = org.do(t, http.MethodPost, "/tasks/"+created.TaskID.String()+"/effects/reserve", map[string]any{
		"tool_name": "send_email",
	})
	var reReserved struct {
		Decision string          `json:"decision"`
		Result   json.RawMessage `json:"result"`
	}
	decodeBody(t, resp, &reReserved)
	if reReserved.Decision != string(engine.EffectAlreadyDone) {
		t.Fatalf("re-reserve decision = %s, want already_done", reReserved.Decision)
	}
	var result struct {
		Sent bool `json:"sent"`
	}
	if err := json.Unmarshal(reReserved.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !result.Sent {
		t.Fatalf("re-reserve did not return the stored result")
	}
}

func TestHTTP_Remember_Recall_Forget(t *testing.T) {
	org := newTestOrg(t)
	agentID := org.mustAgentID(t, "memory-pool")
	embedding := make([]float32, 1536)
	embedding[0] = 1

	resp := org.do(t, http.MethodPost, "/memories", map[string]any{
		"agent_id":   agentID,
		"subject_id": "http-subject",
		"content":    "a fact",
		"embedding":  embedding,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("remember status = %d, want 201", resp.StatusCode)
	}
	var created struct {
		MemoryID uuid.UUID `json:"memory_id"`
	}
	decodeBody(t, resp, &created)

	resp = org.do(t, http.MethodPost, "/memories/recall", map[string]any{
		"agent_id":        agentID,
		"subject_id":      "http-subject",
		"query_embedding": embedding,
		"top_k":           5,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("recall status = %d, want 200", resp.StatusCode)
	}
	var recalled struct {
		Results []struct {
			MemoryID uuid.UUID `json:"memory_id"`
		} `json:"results"`
	}
	decodeBody(t, resp, &recalled)
	if len(recalled.Results) == 0 || recalled.Results[0].MemoryID != created.MemoryID {
		t.Fatalf("recall did not return the remembered memory")
	}

	resp = org.do(t, http.MethodDelete, "/memories/"+created.MemoryID.String(), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("forget status = %d, want 200", resp.StatusCode)
	}

	resp = org.do(t, http.MethodDelete, "/memories/"+created.MemoryID.String(), nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("second forget status = %d, want 404", resp.StatusCode)
	}
}

func TestHTTP_Remember_WrongEmbeddingDimension(t *testing.T) {
	org := newTestOrg(t)
	resp := org.do(t, http.MethodPost, "/memories", map[string]any{
		"agent_id":   org.mustAgentID(t, "p"),
		"subject_id": "s",
		"content":    "c",
		"embedding":  []float32{1, 2, 3},
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHTTP_CrossOrg_CannotRememberUnderAnotherOrgsAgent(t *testing.T) {
	orgA := newTestOrg(t)
	orgB := newTestOrg(t)
	agentA := orgA.mustAgentID(t, "p")
	embedding := make([]float32, 1536)

	// org B tries to write a memory attributed to org A's agent.
	resp := orgB.do(t, http.MethodPost, "/memories", map[string]any{
		"agent_id":   agentA,
		"subject_id": "s",
		"content":    "c",
		"embedding":  embedding,
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-org remember status = %d, want 404 (agent not found for this org)", resp.StatusCode)
	}
}

func TestHTTP_Healthz(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("get /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
