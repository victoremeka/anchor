package engine

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"anchor/internal/auth"
)

// CreateOrganization creates a new tenant with its first API key.
func (e *Engine) CreateOrganization(ctx context.Context, name string) (orgID uuid.UUID, apiKey string, err error) {
	var pgOrgID pgtype.UUID
	err = e.store.ExecuteTx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ($1) RETURNING org_id`, name).Scan(&pgOrgID)
	})
	if err != nil {
		return uuid.UUID{}, "", err
	}
	orgID = fromPGUUID(pgOrgID)

	if _, apiKey, err = e.CreateAPIKey(ctx, orgID); err != nil {
		return uuid.UUID{}, "", err
	}
	return orgID, apiKey, nil
}

// CreateAPIKey mints a new key for orgID.
func (e *Engine) CreateAPIKey(ctx context.Context, orgID uuid.UUID) (keyID uuid.UUID, apiKey string, err error) {
	apiKey, keyHash, err := auth.GenerateAPIKey()
	if err != nil {
		return uuid.UUID{}, "", err
	}

	var pgID pgtype.UUID
	err = e.store.ExecuteTx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO api_keys (org_id, key_hash) VALUES ($1, $2)
			 RETURNING key_id`, toPGUUID(orgID), keyHash).Scan(&pgID)
	})
	if err != nil {
		return uuid.UUID{}, "", err
	}
	return fromPGUUID(pgID), apiKey, nil
}

// Resolves a plaintext API key to its owning
// organization. 
func (e *Engine) AuthenticateAPIKey(ctx context.Context, apiKey string) (uuid.UUID, error) {
	var pgID pgtype.UUID
	err := e.store.Pool.QueryRow(ctx,
		`SELECT org_id FROM api_keys WHERE key_hash = $1 AND revoked_at IS NULL`, auth.HashKey(apiKey)).Scan(&pgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.UUID{}, ErrInvalidAPIKey
	}
	if err != nil {
		return uuid.UUID{}, err
	}
	return fromPGUUID(pgID), nil
}

type APIKeyInfo struct {
	KeyID     uuid.UUID
	CreatedAt time.Time
	RevokedAt *time.Time
}

func (e *Engine) ListAPIKeys(ctx context.Context, orgID uuid.UUID) ([]APIKeyInfo, error) {
	rows, err := e.store.Pool.Query(ctx, `
		SELECT key_id, created_at, revoked_at FROM api_keys
		 WHERE org_id = $1
		 ORDER BY created_at`, toPGUUID(orgID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []APIKeyInfo
	for rows.Next() {
		var pgID pgtype.UUID
		var info APIKeyInfo
		if err := rows.Scan(&pgID, &info.CreatedAt, &info.RevokedAt); err != nil {
			return nil, err
		}
		info.KeyID = fromPGUUID(pgID)
		out = append(out, info)
	}
	return out, rows.Err()
}

func (e *Engine) RevokeAPIKey(ctx context.Context, orgID, keyID uuid.UUID) error {
	owner, err := e.apiKeyOrg(ctx, keyID)
	if err != nil {
		return err
	}
	if owner != orgID {
		return ErrAPIKeyNotFound
	}

	return e.store.ExecuteTx(ctx, func(tx pgx.Tx) error {
		var targetActive bool
		if err := tx.QueryRow(ctx, `SELECT revoked_at IS NULL FROM api_keys WHERE key_id = $1`, toPGUUID(keyID)).Scan(&targetActive); err != nil {
			return err
		}
		if !targetActive {
			return nil
		}

		var activeCount int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM api_keys WHERE org_id = $1 AND revoked_at IS NULL`, toPGUUID(orgID)).Scan(&activeCount); err != nil {
			return err
		}
		if activeCount <= 1 {
			return ErrLastActiveKey
		}

		_, err := tx.Exec(ctx, `UPDATE api_keys SET revoked_at = now() WHERE key_id = $1`, toPGUUID(keyID))
		return err
	})
}

// Registers a new agent identity dynamically, scoped to orgID.
func (e *Engine) CreateAgent(ctx context.Context, orgID uuid.UUID, name, pool string) (uuid.UUID, error) {
	var pgID pgtype.UUID
	err := e.store.ExecuteTx(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO agents (org_id, name, pool) VALUES ($1, $2, $3)
			 RETURNING agent_id`, toPGUUID(orgID), name, pool).Scan(&pgID)
	})
	if err != nil {
		return uuid.UUID{}, err
	}
	return fromPGUUID(pgID), nil
}
