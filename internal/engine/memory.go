package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pgvector/pgvector-go"

	"anchor/internal/registry"
)


func (e *Engine) Remember(ctx context.Context, orgID, agentID uuid.UUID, subjectID, content string, embedding []float32, linkedTable, linkedID *string) (uuid.UUID, error) {
	if err := e.requireAgentOrg(ctx, agentID, orgID); err != nil {
		return uuid.UUID{}, err
	}
	if linkedTable != nil && *linkedTable != "" && !e.registry.IsRegistered(*linkedTable) {
		return uuid.UUID{}, fmt.Errorf("remember: %w: %s", registry.ErrTableNotRegistered, *linkedTable)
	}

	var memoryID uuid.UUID
	err := e.store.ExecuteTx(ctx, func(tx pgx.Tx) error {
		var pgID pgtype.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO memories (org_id, agent_id, subject_id, content, embedding, linked_table, linked_id)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)
			 RETURNING memory_id`,
			toPGUUID(orgID), toPGUUID(agentID), subjectID, content, pgvector.NewVector(embedding), linkedTable, linkedID).Scan(&pgID); err != nil {
			return err
		}
		memoryID = fromPGUUID(pgID)
		return nil
	})
	return memoryID, err
}

type RecalledMemory struct {
	MemoryID          uuid.UUID
	Content           string
	FreshnessVerified bool
	LiveValue         json.RawMessage

	linkedTable, linkedID *string
}


func (e *Engine) Recall(ctx context.Context, orgID, agentID uuid.UUID, subjectID string, queryEmbedding []float32, topK int) ([]RecalledMemory, error) {
	if err := e.requireAgentOrg(ctx, agentID, orgID); err != nil {
		return nil, err
	}

	out, err := e.searchMemories(ctx, orgID, agentID, subjectID, queryEmbedding, topK)
	if err != nil {
		return nil, err
	}

	for i := range out {
		m := &out[i]
		if m.linkedTable != nil && m.linkedID != nil {
			if liveValue, found, lookupErr := e.registry.Lookup(ctx, *m.linkedTable, *m.linkedID); lookupErr == nil && found {
				m.FreshnessVerified = true
				m.LiveValue = liveValue
			}
		}
	}
	return out, nil
}

func (e *Engine) searchMemories(ctx context.Context, orgID, agentID uuid.UUID, subjectID string, queryEmbedding []float32, topK int) ([]RecalledMemory, error) {
	rows, err := e.store.Pool.Query(ctx, `
		SELECT memory_id, content, linked_table, linked_id
		 FROM memories
		 WHERE org_id = $1 AND agent_id = $2 AND subject_id = $3
		 ORDER BY embedding <-> $4
		 LIMIT $5`, toPGUUID(orgID), toPGUUID(agentID), subjectID, pgvector.NewVector(queryEmbedding), topK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RecalledMemory
	for rows.Next() {
		var m RecalledMemory
		var pgID pgtype.UUID
		if err := rows.Scan(&pgID, &m.Content, &m.linkedTable, &m.linkedID); err != nil {
			return nil, err
		}
		m.MemoryID = fromPGUUID(pgID)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (e *Engine) Forget(ctx context.Context, orgID, memoryID uuid.UUID) error {
	ct, err := e.store.Pool.Exec(ctx, `DELETE FROM memories WHERE memory_id = $1 AND org_id = $2`, toPGUUID(memoryID), toPGUUID(orgID))
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrMemoryNotFound
	}
	return nil
}

var pgIdentRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func quoteIdent(ident string) (string, error) {
	if !pgIdentRe.MatchString(ident) {
		return "", fmt.Errorf("invalid identifier: %q", ident)
	}
	return `"` + ident + `"`, nil
}

// Registers a generic freshness lookup for table.
func (e *Engine) RegisterTable(table, idColumn string) error {
	qTable, err := quoteIdent(table)
	if err != nil {
		return err
	}
	qCol, err := quoteIdent(idColumn)
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`SELECT to_jsonb(t) FROM %s AS t WHERE t.%s = $1`, qTable, qCol)

	e.registry.Register(table, func(ctx context.Context, id string) (json.RawMessage, bool, error) {
		var raw json.RawMessage
		err := e.store.Pool.QueryRow(ctx, query, id).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		return raw, true, nil
	})
	return nil
}
