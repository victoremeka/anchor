package engine

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func toPGUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func fromPGUUID(id pgtype.UUID) uuid.UUID {
	return uuid.UUID(id.Bytes)
}
