package engine

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"anchor/internal/idempotency"
)

type EffectDecision string

const (
	EffectRun EffectDecision = "run"

	EffectAlreadyDone EffectDecision = "already_done"

	EffectAmbiguous EffectDecision = "ambiguous"
)

func (e *Engine) ReserveEffect(ctx context.Context, orgID, taskID uuid.UUID, toolName, callKey string) (idempotencyKey string, decision EffectDecision, storedResult json.RawMessage, err error) {
	if err := e.requireTaskOrg(ctx, taskID, orgID); err != nil {
		return "", "", nil, err
	}

	idempotencyKey = idempotency.Key(taskID.String(), toolName, callKey)

	err = e.store.ExecuteTx(ctx, func(tx pgx.Tx) error {
		var status string
		var result json.RawMessage
		selErr := tx.QueryRow(ctx,
			`SELECT status, result FROM executed_effects WHERE idempotency_key = $1 FOR UPDATE`,
			idempotencyKey).Scan(&status, &result)

		switch {
		case errors.Is(selErr, pgx.ErrNoRows):
			_, insErr := tx.Exec(ctx, `
				INSERT INTO executed_effects (idempotency_key, org_id, task_id, tool_name, status)
				 VALUES ($1, $2, $3, $4, 'pending')`, idempotencyKey, toPGUUID(orgID), toPGUUID(taskID), toolName)
			if insErr != nil {
				var pgErr *pgconn.PgError
				if errors.As(insErr, &pgErr) && pgErr.Code == "23505" {
					return ErrEffectReserveRace
				}
				return insErr
			}
			decision = EffectRun
			return nil
		case selErr != nil:
			return selErr
		case status == "completed":
			decision = EffectAlreadyDone
			storedResult = result
			return nil
		default: // "pending"
			decision = EffectAmbiguous
			return nil
		}
	})
	if err != nil {
		return "", "", nil, err
	}
	return idempotencyKey, decision, storedResult, nil
}

func (e *Engine) CompleteEffect(ctx context.Context, orgID uuid.UUID, idempotencyKey string, result json.RawMessage) error {
	return e.store.ExecuteTx(ctx, func(tx pgx.Tx) error {
		ct, err := tx.Exec(ctx, `
			UPDATE executed_effects SET status = 'completed', result = $2, completed_at = now()
			 WHERE idempotency_key = $1 AND org_id = $3`, idempotencyKey, result, toPGUUID(orgID))
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			return ErrEffectNotFound
		}
		return nil
	})
}
