package engine

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (e *Engine) agentOrg(ctx context.Context, agentID uuid.UUID) (uuid.UUID, error) {
	var org pgtype.UUID
	err := e.store.Pool.QueryRow(ctx, `SELECT org_id FROM agents WHERE agent_id = $1`, toPGUUID(agentID)).Scan(&org)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.UUID{}, ErrAgentNotFound
	}
	if err != nil {
		return uuid.UUID{}, err
	}
	return fromPGUUID(org), nil
}

func (e *Engine) taskOrg(ctx context.Context, taskID uuid.UUID) (uuid.UUID, error) {
	var org pgtype.UUID
	err := e.store.Pool.QueryRow(ctx, `SELECT org_id FROM tasks WHERE task_id = $1`, toPGUUID(taskID)).Scan(&org)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.UUID{}, ErrTaskNotFound
	}
	if err != nil {
		return uuid.UUID{}, err
	}
	return fromPGUUID(org), nil
}

func (e *Engine) requireAgentOrg(ctx context.Context, agentID, orgID uuid.UUID) error {
	owner, err := e.agentOrg(ctx, agentID)
	if err != nil {
		return err
	}
	if owner != orgID {
		return ErrAgentNotFound
	}
	return nil
}

func (e *Engine) requireTaskOrg(ctx context.Context, taskID, orgID uuid.UUID) error {
	owner, err := e.taskOrg(ctx, taskID)
	if err != nil {
		return err
	}
	if owner != orgID {
		return ErrTaskNotFound
	}
	return nil
}

func (e *Engine) apiKeyOrg(ctx context.Context, keyID uuid.UUID) (uuid.UUID, error) {
	var org pgtype.UUID
	err := e.store.Pool.QueryRow(ctx, `SELECT org_id FROM api_keys WHERE key_id = $1`, toPGUUID(keyID)).Scan(&org)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.UUID{}, ErrAPIKeyNotFound
	}
	if err != nil {
		return uuid.UUID{}, err
	}
	return fromPGUUID(org), nil
}
