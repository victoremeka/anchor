package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type Task struct {
	ID        uuid.UUID
	OrgID     uuid.UUID
	AgentPool string
	Payload   json.RawMessage
	ClaimedBy uuid.UUID
}

// CreateTask creates a task, deduping on dedupKey (scoped per-org) if one
// is given.
func (e *Engine) CreateTask(ctx context.Context, orgID uuid.UUID, agentPool string, payload json.RawMessage, dedupKey *string) (uuid.UUID, error) {
	var dk any
	if dedupKey != nil && *dedupKey != "" {
		dk = *dedupKey
	}

	var taskID pgtype.UUID
	err := e.store.ExecuteTx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO tasks (org_id, agent_pool, payload, dedup_key)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (org_id, dedup_key) WHERE dedup_key IS NOT NULL DO NOTHING
			 RETURNING task_id`, toPGUUID(orgID), agentPool, payload, dk).Scan(&taskID)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if dk == nil {
			return fmt.Errorf("create_task: insert returned no row without a dedup key: %w", err)
		}
		return tx.QueryRow(ctx, `SELECT task_id FROM tasks WHERE org_id = $1 AND dedup_key = $2`, toPGUUID(orgID), dk).Scan(&taskID)
	})
	if err != nil {
		return uuid.UUID{}, err
	}
	return fromPGUUID(taskID), nil
}

// reclaimSweep runs the two reclaim UPDATEs as independent, non-atomic
// statements. It runs across all orgs: reclaim is maintenance, not
// a caller-scoped operation.
func (e *Engine) reclaimSweep(ctx context.Context) error {
	if _, err := e.store.Pool.Exec(ctx, `
		UPDATE tasks SET status = 'pending', claimed_by = NULL, claimed_at = NULL, claim_expires_at = NULL
		 WHERE status = 'claimed' AND claim_expires_at < now()
		   AND task_id NOT IN (SELECT task_id FROM executed_effects WHERE status = 'pending')`); err != nil {
		return fmt.Errorf("reclaim sweep (pending): %w", err)
	}
	if _, err := e.store.Pool.Exec(ctx, `
		UPDATE tasks SET status = 'flagged'
		 WHERE status = 'claimed' AND claim_expires_at < now()
		   AND task_id IN (SELECT task_id FROM executed_effects WHERE status = 'pending')`); err != nil {
		return fmt.Errorf("reclaim sweep (flag): %w", err)
	}
	return nil
}

// RunReclaimSweep runs the reclaim sweep once. Call it periodically from a background
// loop (e.g. StartReclaimLoop) instead of inline on every claim, to avoid
// running two extra table scans on the hottest path in the system.
func (e *Engine) RunReclaimSweep(ctx context.Context) error {
	return e.reclaimSweep(ctx)
}

// StartReclaimLoop runs the reclaim sweep on a ticker until ctx is
// canceled. Sweep errors are sent to onErr (if non-nil) rather than
// stopping the loop, since a single failed sweep should not take reclaim
// out of service until the next tick.
func (e *Engine) StartReclaimLoop(ctx context.Context, interval time.Duration, onErr func(error)) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := e.reclaimSweep(ctx); err != nil && onErr != nil {
					onErr(err)
				}
			}
		}
	}()
}

// ClaimTask claims race-free via SKIP LOCKED, scoped to orgID.
func (e *Engine) ClaimTask(ctx context.Context, orgID uuid.UUID, agentPool string, workerID uuid.UUID) (*Task, error) {
	if err := e.requireAgentOrg(ctx, workerID, orgID); err != nil {
		return nil, err
	}

	var task Task
	err := e.store.ExecuteTx(ctx, func(tx pgx.Tx) error {
		var taskID pgtype.UUID
		var payload json.RawMessage
		err := tx.QueryRow(ctx, `
			SELECT task_id, payload FROM tasks
			 WHERE org_id = $1 AND agent_pool = $2 AND status = 'pending'
			 ORDER BY created_at
			 FOR UPDATE SKIP LOCKED
			 LIMIT 1`, toPGUUID(orgID), agentPool).Scan(&taskID, &payload)
		if err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			UPDATE tasks SET status = 'claimed', claimed_by = $1, claimed_at = now(),
			                 claim_expires_at = now() + INTERVAL '1 second' * $2::FLOAT8
			 WHERE task_id = $3`, toPGUUID(workerID), e.leaseTTL.Seconds(), taskID); err != nil {
			return err
		}

		task = Task{ID: fromPGUUID(taskID), OrgID: orgID, AgentPool: agentPool, Payload: payload, ClaimedBy: workerID}
		return nil
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoTaskAvailable
		}
		return nil, err
	}
	return &task, nil
}

func (e *Engine) CompleteTask(ctx context.Context, orgID, taskID, claimedBy uuid.UUID, result json.RawMessage) (bool, error) {
	var ok bool
	err := e.store.ExecuteTx(ctx, func(tx pgx.Tx) error {
		ct, err := tx.Exec(ctx, `
			UPDATE tasks SET status = 'done', result = $2
			 WHERE task_id = $1 AND org_id = $4 AND claimed_by = $3 AND status = 'claimed'`,
			toPGUUID(taskID), result, toPGUUID(claimedBy), toPGUUID(orgID))
		if err != nil {
			return err
		}
		ok = ct.RowsAffected() > 0
		return nil
	})
	return ok, err
}
