package engine_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"anchor/internal/engine"
	"anchor/internal/registry"
	"anchor/internal/store"
)

func pgUUID(id uuid.UUID) pgtype.UUID     { return pgtype.UUID{Bytes: id, Valid: true} }
func fromPGUUID(id pgtype.UUID) uuid.UUID { return uuid.UUID(id.Bytes) }

func TestCorrectness_Concurrency_ZeroDoubleClaims(t *testing.T) {
	const (
		numTasks   = 10
		numWorkers = 25
		numRuns    = 3
	)

	for run := 0; run < numRuns; run++ {
		t.Run(fmt.Sprintf("run-%d", run), func(t *testing.T) {
			e := newTestEngine(t, time.Minute)
			ctx := context.Background()
			orgID := mustOrg(t, e)
			pool := "concurrency-pool-" + uuid.NewString()

			taskIDs := make(map[uuid.UUID]bool, numTasks)
			for i := 0; i < numTasks; i++ {
				id, err := e.CreateTask(ctx, orgID, pool, json.RawMessage(`{}`), nil)
				if err != nil {
					t.Fatalf("create_task: %v", err)
				}
				taskIDs[id] = true
			}

			var mu sync.Mutex
			claimedBy := make(map[uuid.UUID]uuid.UUID, numTasks)

			var wg sync.WaitGroup
			for i := 0; i < numWorkers; i++ {
				workerID := mustAgent(t, e, orgID)
				wg.Add(1)
				go func() {
					defer wg.Done()
					we := newTestEngineNoCleanup(t, time.Minute)
					defer we.close()
					for {
						task, err := we.engine.ClaimTask(ctx, orgID, pool, workerID)
						if err != nil {
							if !errors.Is(err, engine.ErrNoTaskAvailable) {
								t.Errorf("unexpected claim error: %v", err)
							}
							return
						}
						mu.Lock()
						if prev, dup := claimedBy[task.ID]; dup {
							mu.Unlock()
							t.Errorf("task %s double-claimed by %s and %s", task.ID, prev, workerID)
							return
						}
						claimedBy[task.ID] = workerID
						mu.Unlock()
					}
				}()
			}
			wg.Wait()

			if len(claimedBy) != numTasks {
				t.Fatalf("expected all %d tasks claimed exactly once, got %d", numTasks, len(claimedBy))
			}
			for id := range taskIDs {
				if _, ok := claimedBy[id]; !ok {
					t.Errorf("task %s was never claimed", id)
				}
			}
		})
	}
}

func TestCorrectness_CrashInjection_SurfacesAmbiguousOnRestart(t *testing.T) {
	dsnStr := dsn(t)
	ctx := context.Background()

	s, err := store.Open(ctx, dsnStr, 1)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer s.Close()
	if _, err := s.Pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS fake_tool_invocations (
			invocation_id    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			idempotency_key  STRING NOT NULL,
			called_at        TIMESTAMPTZ DEFAULT now()
		)`); err != nil {
		t.Fatalf("create fake_tool_invocations: %v", err)
	}

	e := engine.New(s, registry.New(), time.Minute)
	orgID := mustOrg(t, e)
	taskID, err := e.CreateTask(ctx, orgID, "crash-pool", json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatalf("create_task: %v", err)
	}
	const toolName = "fake_tool"

	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashInjectionHelperProcess$")
	cmd.Env = append(os.Environ(),
		"ANCHOR_CRASH_HELPER=1",
		"ANCHOR_TEST_DATABASE_URL="+dsnStr,
		"ANCHOR_CRASH_ORG_ID="+orgID.String(),
		"ANCHOR_CRASH_TASK_ID="+taskID.String(),
		"ANCHOR_CRASH_TOOL_NAME="+toolName,
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}

	watchdogDone := make(chan struct{})
	go func() {
		select {
		case <-watchdogDone:
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
		}
	}()

	var key string
	sawToolCalled := false
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if after, ok := strings.CutPrefix(line, "KEY:"); ok {
			key = after
		}
		if line == "TOOL_CALLED" {
			sawToolCalled = true
			break
		}
	}
	close(watchdogDone)

	killErr := cmd.Process.Kill()
	_ = cmd.Wait()
	if !sawToolCalled {
		t.Fatalf("helper process never signaled TOOL_CALLED (kill err: %v)", killErr)
	}
	if key == "" {
		t.Fatalf("helper process never reported its idempotency key")
	}

	var effectCount int
	var status string
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM executed_effects WHERE idempotency_key = $1`, key).Scan(&effectCount); err != nil {
		t.Fatalf("count executed_effects: %v", err)
	}
	if effectCount != 1 {
		t.Fatalf("expected exactly 1 executed_effects row, got %d", effectCount)
	}
	if err := s.Pool.QueryRow(ctx, `SELECT status FROM executed_effects WHERE idempotency_key = $1`, key).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "pending" {
		t.Fatalf("status after the crash = %q, want pending", status)
	}

	countInvocations := func() int {
		t.Helper()
		var n int
		if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM fake_tool_invocations WHERE idempotency_key = $1`, key).Scan(&n); err != nil {
			t.Fatalf("count fake_tool_invocations: %v", err)
		}
		return n
	}
	if n := countInvocations(); n != 1 {
		t.Fatalf("fake tool invocation count after the crash = %d, want 1", n)
	}

	s2, err := store.Open(ctx, dsnStr, 1)
	if err != nil {
		t.Fatalf("store.Open (restart): %v", err)
	}
	defer s2.Close()
	e2 := engine.New(s2, registry.New(), time.Minute)

	_, decision, _, err := e2.ReserveEffect(ctx, orgID, taskID, toolName, "")
	if err != nil {
		t.Fatalf("reserve after restart: %v", err)
	}
	if decision != engine.EffectAmbiguous {
		t.Fatalf("decision after restart = %v, want EffectAmbiguous (must not auto-retry)", decision)
	}
	if n := countInvocations(); n != 1 {
		t.Fatalf("fake tool invocation count after restart = %d, want still 1 (Engine auto-retried the call)", n)
	}
}

func TestCrashInjectionHelperProcess(t *testing.T) {
	if os.Getenv("ANCHOR_CRASH_HELPER") != "1" {
		t.Skip("subprocess helper for TestCorrectness_CrashInjection_SurfacesAmbiguousOnRestart; not a standalone test")
	}

	ctx := context.Background()
	orgID, err := uuid.Parse(os.Getenv("ANCHOR_CRASH_ORG_ID"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad ANCHOR_CRASH_ORG_ID:", err)
		os.Exit(1)
	}
	taskID, err := uuid.Parse(os.Getenv("ANCHOR_CRASH_TASK_ID"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "bad ANCHOR_CRASH_TASK_ID:", err)
		os.Exit(1)
	}
	toolName := os.Getenv("ANCHOR_CRASH_TOOL_NAME")

	s, err := store.Open(ctx, os.Getenv("ANCHOR_TEST_DATABASE_URL"), 1)
	if err != nil {
		fmt.Fprintln(os.Stderr, "store.Open:", err)
		os.Exit(1)
	}
	e := engine.New(s, registry.New(), time.Minute)

	key, decision, _, err := e.ReserveEffect(ctx, orgID, taskID, toolName, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "reserve:", err)
		os.Exit(1)
	}
	if decision != engine.EffectRun {
		fmt.Fprintln(os.Stderr, "unexpected decision:", decision)
		os.Exit(1)
	}

	if _, err := s.Pool.Exec(ctx, `INSERT INTO fake_tool_invocations (idempotency_key) VALUES ($1)`, key); err != nil {
		fmt.Fprintln(os.Stderr, "record invocation:", err)
		os.Exit(1)
	}

	fmt.Println("KEY:" + key)
	fmt.Println("TOOL_CALLED")

	select {}
}

func TestCorrectness_LeaseExpiry_ExactlyOneReclaim(t *testing.T) {
	const lease = 500 * time.Millisecond
	e := newTestEngine(t, lease)
	ctx := context.Background()
	orgID := mustOrg(t, e)
	pool := "lease-pool-" + uuid.NewString()

	taskID, err := e.CreateTask(ctx, orgID, pool, json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatalf("create_task: %v", err)
	}

	worker1 := mustAgent(t, e, orgID)
	task, err := e.ClaimTask(ctx, orgID, pool, worker1)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if task.ID != taskID {
		t.Fatalf("claimed wrong task")
	}

	time.Sleep(2 * lease)
	if err := e.RunReclaimSweep(ctx); err != nil {
		t.Fatalf("reclaim sweep: %v", err)
	}

	const racers = 5
	var wins int64
	winners := make(chan uuid.UUID, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		workerID := mustAgent(t, e, orgID)
		wg.Add(1)
		go func() {
			defer wg.Done()
			we := newTestEngineNoCleanup(t, lease)
			defer we.close()
			task, err := we.engine.ClaimTask(ctx, orgID, pool, workerID)
			if err != nil {
				return
			}
			if task.ID == taskID {
				atomic.AddInt64(&wins, 1)
				winners <- workerID
			}
		}()
	}
	wg.Wait()
	close(winners)

	if wins != 1 {
		t.Fatalf("expected exactly one reclaim winner, got %d — never two simultaneous owners", wins)
	}
	winner := <-winners

	var status string
	var claimedBy uuid.UUID
	if err := queryTaskClaim(ctx, t, taskID, &status, &claimedBy); err != nil {
		t.Fatalf("query task: %v", err)
	}
	if status != "claimed" {
		t.Fatalf("task status = %q, want claimed", status)
	}
	if claimedBy != winner {
		t.Fatalf("claimed_by = %s, want the sole winner %s", claimedBy, winner)
	}
}

func TestCorrectness_LeaseExpiry_FlagsWhenEffectPending(t *testing.T) {
	const lease = 500 * time.Millisecond
	e := newTestEngine(t, lease)
	ctx := context.Background()
	orgID := mustOrg(t, e)
	pool := "flag-pool-" + uuid.NewString()

	taskID, err := e.CreateTask(ctx, orgID, pool, json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatalf("create_task: %v", err)
	}
	worker := mustAgent(t, e, orgID)
	if _, err := e.ClaimTask(ctx, orgID, pool, worker); err != nil {
		t.Fatalf("claim: %v", err)
	}

	// Force its effect into pending: reserve, never complete.
	_, decision, _, err := e.ReserveEffect(ctx, orgID, taskID, "fake_tool", "")
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if decision != engine.EffectRun {
		t.Fatalf("reserve decision = %v, want EffectRun", decision)
	}

	time.Sleep(2 * lease)
	if err := e.RunReclaimSweep(ctx); err != nil {
		t.Fatalf("reclaim sweep: %v", err)
	}

	var status string
	var claimedBy uuid.UUID
	if err := queryTaskClaim(ctx, t, taskID, &status, &claimedBy); err != nil {
		t.Fatalf("query task: %v", err)
	}
	if status != "flagged" {
		t.Fatalf("status after sweep = %q, want flagged (not reclaimed)", status)
	}

	other := mustAgent(t, e, orgID)
	if _, err := e.ClaimTask(ctx, orgID, pool, other); !errors.Is(err, engine.ErrNoTaskAvailable) {
		t.Fatalf("flagged task was claimable: err=%v", err)
	}
}

func TestCorrectness_Fencing_ZombieCannotComplete(t *testing.T) {
	const lease = 500 * time.Millisecond
	e := newTestEngine(t, lease)
	ctx := context.Background()
	orgID := mustOrg(t, e)
	pool := "fencing-pool-" + uuid.NewString()

	taskID, err := e.CreateTask(ctx, orgID, pool, json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatalf("create_task: %v", err)
	}

	zombie := mustAgent(t, e, orgID)
	if _, err := e.ClaimTask(ctx, orgID, pool, zombie); err != nil {
		t.Fatalf("first claim: %v", err)
	}

	time.Sleep(2 * lease)
	if err := e.RunReclaimSweep(ctx); err != nil {
		t.Fatalf("reclaim sweep: %v", err)
	}

	legit := mustAgent(t, e, orgID)
	task2, err := e.ClaimTask(ctx, orgID, pool, legit)
	if err != nil {
		t.Fatalf("second claim after reclaim: %v", err)
	}
	if task2.ID != taskID {
		t.Fatalf("second claim got the wrong task")
	}

	ok, err := e.CompleteTask(ctx, orgID, taskID, zombie, json.RawMessage(`{"result":"zombie"}`))
	if err != nil {
		t.Fatalf("zombie complete_task: %v", err)
	}
	if ok {
		t.Fatalf("zombie caller was allowed to complete the task")
	}

	ok, err = e.CompleteTask(ctx, orgID, taskID, legit, json.RawMessage(`{"result":"legit"}`))
	if err != nil {
		t.Fatalf("legit complete_task: %v", err)
	}
	if !ok {
		t.Fatalf("legitimate second claimant could not complete the task")
	}

	var storedResult struct {
		Result string `json:"result"`
	}
	if err := queryTaskResult(ctx, t, taskID, &storedResult); err != nil {
		t.Fatalf("query result: %v", err)
	}
	if storedResult.Result != "legit" {
		t.Fatalf("stored result = %q, want legit — zombie must not overwrite it", storedResult.Result)
	}
}

func TestCorrectness_IdempotentCreation(t *testing.T) {
	for run := 0; run < 5; run++ {
		t.Run(fmt.Sprintf("run-%d", run), func(t *testing.T) {
			e := newTestEngine(t, time.Minute)
			ctx := context.Background()
			orgID := mustOrg(t, e)
			key := uuid.NewString()

			id1, err := e.CreateTask(ctx, orgID, "dedup-pool", json.RawMessage(`{"n":1}`), &key)
			if err != nil {
				t.Fatalf("first create_task: %v", err)
			}
			id2, err := e.CreateTask(ctx, orgID, "dedup-pool", json.RawMessage(`{"n":1}`), &key)
			if err != nil {
				t.Fatalf("second create_task: %v", err)
			}
			if id1 != id2 {
				t.Fatalf("dedup failed: got two different task ids %s / %s", id1, id2)
			}
			if n := countTasksByDedupKey(ctx, t, orgID, key); n != 1 {
				t.Fatalf("expected exactly 1 tasks row for dedup_key, got %d", n)
			}
		})
	}
}

func TestCorrectness_IdempotentCreation_ConcurrentRace(t *testing.T) {
	setup := newTestEngine(t, time.Minute)
	ctx := context.Background()
	orgID := mustOrg(t, setup)
	key := uuid.NewString()

	const racers = 10
	ids := make([]uuid.UUID, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			we := newTestEngineNoCleanup(t, time.Minute)
			defer we.close()
			id, err := we.engine.CreateTask(ctx, orgID, "dedup-race-pool", json.RawMessage(`{}`), &key)
			if err != nil {
				t.Errorf("create_task race: %v", err)
				return
			}
			ids[i] = id
		}()
	}
	wg.Wait()

	first := ids[0]
	for i, id := range ids {
		if id != first {
			t.Fatalf("racer %d got task id %s, want %s (all racers must agree)", i, id, first)
		}
	}
	if n := countTasksByDedupKey(ctx, t, orgID, key); n != 1 {
		t.Fatalf("expected exactly 1 tasks row for dedup_key, got %d", n)
	}
}

func TestCorrectness_EffectDisambiguation(t *testing.T) {
	e := newTestEngine(t, time.Minute)
	ctx := context.Background()
	orgID := mustOrg(t, e)

	taskID, err := e.CreateTask(ctx, orgID, "effect-pool", json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatalf("create_task: %v", err)
	}

	key1, decision, _, err := e.ReserveEffect(ctx, orgID, taskID, "send_email", "recipient-a")
	if err != nil {
		t.Fatalf("reserve (a): %v", err)
	}
	if decision != engine.EffectRun {
		t.Fatalf("reserve (a) decision = %v, want EffectRun", decision)
	}
	key2, decision, _, err := e.ReserveEffect(ctx, orgID, taskID, "send_email", "recipient-b")
	if err != nil {
		t.Fatalf("reserve (b): %v", err)
	}
	if decision != engine.EffectRun {
		t.Fatalf("reserve (b) decision = %v, want EffectRun", decision)
	}
	if key1 == key2 {
		t.Fatalf("different call_keys produced the same idempotency key")
	}

	if err := e.CompleteEffect(ctx, orgID, key1, json.RawMessage(`{"sent":"a"}`)); err != nil {
		t.Fatalf("complete (a): %v", err)
	}
	if err := e.CompleteEffect(ctx, orgID, key2, json.RawMessage(`{"sent":"b"}`)); err != nil {
		t.Fatalf("complete (b): %v", err)
	}
	if n := countEffectRows(ctx, t, taskID); n != 2 {
		t.Fatalf("expected 2 independent executed_effects rows, got %d", n)
	}

	// Repeat (a) with the same call_key: must not run again, must return
	// the stored result from the first run.
	_, decision, stored, err := e.ReserveEffect(ctx, orgID, taskID, "send_email", "recipient-a")
	if err != nil {
		t.Fatalf("re-reserve (a): %v", err)
	}
	if decision != engine.EffectAlreadyDone {
		t.Fatalf("re-reserve (a) decision = %v, want EffectAlreadyDone", decision)
	}
	var storedResult struct {
		Sent string `json:"sent"`
	}
	unmarshalJSON(t, stored, &storedResult)
	if storedResult.Sent != "a" {
		t.Fatalf("re-reserve (a) stored result = %s, want sent=a", stored)
	}
	if n := countEffectRows(ctx, t, taskID); n != 2 {
		t.Fatalf("repeating call_key=a created a new row: count=%d, want still 2", n)
	}
}

func queryTaskClaim(ctx context.Context, t *testing.T, taskID uuid.UUID, status *string, claimedBy *uuid.UUID) error {
	t.Helper()
	s, err := store.Open(ctx, dsn(t), 1)
	if err != nil {
		return err
	}
	defer s.Close()
	var cb pgtype.UUID
	if err := s.Pool.QueryRow(ctx, `SELECT status, claimed_by FROM tasks WHERE task_id = $1`, pgUUID(taskID)).Scan(status, &cb); err != nil {
		return err
	}
	if cb.Valid {
		*claimedBy = fromPGUUID(cb)
	}
	return nil
}

func queryTaskResult(ctx context.Context, t *testing.T, taskID uuid.UUID, v any) error {
	t.Helper()
	s, err := store.Open(ctx, dsn(t), 1)
	if err != nil {
		return err
	}
	defer s.Close()
	var raw json.RawMessage
	if err := s.Pool.QueryRow(ctx, `SELECT result FROM tasks WHERE task_id = $1`, pgUUID(taskID)).Scan(&raw); err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

func countTasksByDedupKey(ctx context.Context, t *testing.T, orgID uuid.UUID, dedupKey string) int {
	t.Helper()
	s, err := store.Open(ctx, dsn(t), 1)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer s.Close()
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE org_id = $1 AND dedup_key = $2`, pgUUID(orgID), dedupKey).Scan(&n); err != nil {
		t.Fatalf("count tasks by dedup_key: %v", err)
	}
	return n
}

func countEffectRows(ctx context.Context, t *testing.T, taskID uuid.UUID) int {
	t.Helper()
	s, err := store.Open(ctx, dsn(t), 1)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer s.Close()
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM executed_effects WHERE task_id = $1`, pgUUID(taskID)).Scan(&n); err != nil {
		t.Fatalf("count executed_effects: %v", err)
	}
	return n
}
