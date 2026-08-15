package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

type ctxKey int

const orgIDCtxKey ctxKey = iota

func orgIDFromContext(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(orgIDCtxKey).(uuid.UUID)
	return id
}

var (
	errMissingAuth = errors.New("missing bearer token")
	errInvalidAuth = errors.New("invalid api key")
)

// Resolves the caller's API key to an org_id and injects it into the request context.
func (h *Handler) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := bearerToken(r)
		if key == "" {
			writeError(w, http.StatusUnauthorized, errMissingAuth)
			return
		}
		orgID, err := h.engine.AuthenticateAPIKey(r.Context(), key)
		if err != nil {
			writeError(w, http.StatusUnauthorized, errInvalidAuth)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), orgIDCtxKey, orgID)))
	}
}

func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, prefix); ok {
		return after
	}
	return ""
}
